package services

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"gateway/models"
	"gateway/pkg/logger"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// OllamaRequest represents the request to Ollama API
type OllamaRequest struct {
	Model    string                 `json:"model"`
	Prompt   string                 `json:"prompt"`
	Stream   bool                   `json:"stream"`
	Options  map[string]interface{} `json:"options,omitempty"`
	System   string                 `json:"system,omitempty"`
	Template string                 `json:"template,omitempty"`
}

// OllamaResponse represents the streaming response from Ollama API
type OllamaResponse struct {
	Model     string `json:"model"`
	Response  string `json:"response"`
	Done      bool   `json:"done"`
	Context   []int  `json:"context,omitempty"`
	CreatedAt string `json:"created_at"`
}

// Global optimized HTTP client for Ollama requests
var (
	ollamaClient *http.Client
	ollamaOnce   sync.Once
)

// initOllamaClient initializes the optimized HTTP client for Ollama
func initOllamaClient() {
	ollamaOnce.Do(func() {
		ollamaClient = &http.Client{
			Timeout: 0, // No timeout for streaming
			Transport: &http.Transport{
				MaxIdleConns:        20,                // Max idle connections
				MaxIdleConnsPerHost: 5,                 // Max idle per host
				MaxConnsPerHost:     10,                // Max total per host
				IdleConnTimeout:     120 * time.Second, // Keep connections alive longer
				TLSHandshakeTimeout: 10 * time.Second,

				// Streaming optimizations
				DisableKeepAlives:  false,
				DisableCompression: true,      // Disable compression for streaming
				WriteBufferSize:    32 * 1024, // 32KB write buffer
				ReadBufferSize:     32 * 1024, // 32KB read buffer

				// Connection timeouts
				ResponseHeaderTimeout: 30 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
			},
		}
	})
}

// getOllamaURL returns the Ollama service URL from environment or default
func getOllamaURL() string {
	if url := os.Getenv("OLLAMA_URL"); url != "" {
		return url
	}
	return "http://localhost:11434" // Default for local development
}

// StreamOllamaResponse calls Ollama API and streams the response with optimizations
func StreamOllamaResponse(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, prompt string, clientID int, previousMessages []models.ChatMessage, profileContext string, workspaceInstructions string) error {
	// Initialize optimized client
	initOllamaClient()

	startTime := time.Now()

	// Get the system prompt
	systemPrompt := models.Config.GetSystemPrompt("gemma3")

	// Format messages for context
	var contextBuilder strings.Builder

	// Add profile context if available
	if profileContext != "" {
		contextBuilder.WriteString("Profile Context:\n" + profileContext + "\n\n")
	}

	// Add workspace instructions if available
	if workspaceInstructions != "" {
		contextBuilder.WriteString("Workspace Instructions:\n" + workspaceInstructions + "\n\n")
	}

	if len(previousMessages) > 0 {
		contextBuilder.WriteString("These are the previous messages in the conversation:\n")
		// Limit to last 4 messages
		startIdx := 0
		if len(previousMessages) > 4 {
			startIdx = len(previousMessages) - 4
		}

		for _, msg := range previousMessages[startIdx:] {
			if msg.Role == "user" {
				contextBuilder.WriteString(fmt.Sprintf("User: %s\n", msg.Content))
			} else {
				contextBuilder.WriteString(fmt.Sprintf("Assistant(%s): %s\n", msg.ModelName, msg.Content))
			}
		}
		contextBuilder.WriteString("\nThe next user question is:\n")
	}
	contextBuilder.WriteString(prompt)

	contextBuilder.WriteString("\nNow answer the user's question.")

	// Create the request body
	reqBody := OllamaRequest{
		Model:  "gemma3:4b",
		Prompt: contextBuilder.String(),
		Stream: true,
		Options: map[string]interface{}{
			"temperature": 0.8,
			"top_k":       40,
			"top_p":       0.95,
		},
		System:   systemPrompt,
		Template: "system: {{ .System }}\n\nuser: {{ .Prompt }}\n\nassistant:",
	}

	// Prepare optimized request
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("error marshaling request: %v", err)
	}

	// Create request with context for cancellation
	req, err := http.NewRequestWithContext(ctx, "POST", getOllamaURL()+"/api/generate", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("error creating request: %v", err)
	}

	// Optimize headers for streaming
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Cache-Control", "no-cache")

	// Make the request
	resp, err := ollamaClient.Do(req)
	if err != nil {
		return fmt.Errorf("error calling Ollama API: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Ollama API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	// Stream processing with optimized buffering
	scanner := bufio.NewScanner(resp.Body)

	// Increase buffer size for better performance
	buf := make([]byte, 64*1024) // 64KB buffer
	scanner.Buffer(buf, 64*1024)

	chunkCount := 0
	firstChunkTime := time.Now()
	var fullResponse strings.Builder

	// Wait for the first chunk to arrive
	firstChunkReceived := false

	for scanner.Scan() {
		line := scanner.Text()
		if len(line) == 0 {
			continue
		}

		// Parse JSON response
		var streamResp OllamaResponse
		if err := json.Unmarshal([]byte(line), &streamResp); err != nil {
			// Log error for debugging
			log := logger.GetLogger("ollama.stream")
			log.ErrorWithFields("Failed to unmarshal Ollama response", map[string]interface{}{
				"error": err.Error(),
				"line":  line,
			}, err)
			continue
		}

		// Track first chunk timing
		if !firstChunkReceived {
			firstChunkTime = time.Now()
			firstChunkReceived = true
		}
		chunkCount++

		// Extract the response part
		chunkText := streamResp.Response
		if chunkText == "" {
			continue
		}
		fullResponse.WriteString(chunkText)

		// Send chunk to handler
		chunkResponse := models.Response{
			Message: chunkText,
			Type:    "chunk",
		}

		msg, err := models.FormatSSEMessage(chunkResponse)
		if err != nil {
			return fmt.Errorf("error formatting chunk: %v", err)
		}

		_, err = fmt.Fprint(w, msg)
		if err != nil {
			return fmt.Errorf("error sending chunk: %v", err)
		}
		flusher.Flush()

		// Check if done
		if streamResp.Done {
			totalTime := time.Since(startTime)
			timeToFirst := firstChunkTime.Sub(startTime)

			// Log performance metrics
			logStreamingMetrics("gemma3", chunkCount, timeToFirst, totalTime)
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading stream: %v", err)
	}

	streamTime := time.Since(startTime)
	streamLogger := logger.GetLogger("stream")
	streamLogger.InfoWithFieldsCtx(ctx, "Llama streaming completed", map[string]interface{}{
		"client_id":     clientID,
		"chunk_count":   chunkCount,
		"stream_time_s": streamTime.Seconds(),
	})

	return nil
}

// logStreamingMetrics logs performance metrics for monitoring
func logStreamingMetrics(model string, chunks int, timeToFirst, totalTime time.Duration) {
	avgChunkTime := float64(totalTime.Milliseconds()) / float64(chunks)

	log := logger.GetLogger("ollama.metrics")
	log.InfoWithFields("Ollama streaming metrics", map[string]interface{}{
		"model":                     model,
		"chunks":                    chunks,
		"time_to_first_ms":          timeToFirst.Milliseconds(),
		"total_time_s":              totalTime.Seconds(),
		"avg_chunk_time_ms":         avgChunkTime,
		"throughput_chunks_per_sec": float64(chunks) / totalTime.Seconds(),
	})
}
