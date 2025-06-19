package services

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"gateway/models"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// GroqMessage represents a message in Groq format
type GroqMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// GroqRequest represents the request to Groq API
type GroqRequest struct {
	Model    string        `json:"model"`
	Messages []GroqMessage `json:"messages"`
	Stream   bool          `json:"stream"`
	// Messages map[string]interface{} `json:"options"`
}

// GroqResponse represents the streaming response from Groq API
type GroqResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage,omitempty"`
	XGroq struct {
		ID    string `json:"id"`
		Usage struct {
			QueueTime        float64 `json:"queue_time"`
			PromptTokens     int     `json:"prompt_tokens"`
			PromptTime       float64 `json:"prompt_time"`
			CompletionTokens int     `json:"completion_tokens"`
			CompletionTime   float64 `json:"completion_time"`
			TotalTokens      int     `json:"total_tokens"`
			TotalTime        float64 `json:"total_time"`
		} `json:"usage,omitempty"`
	} `json:"x_groq,omitempty"`
}

// Global optimized HTTP client for Groq requests
var (
	groqClient *http.Client
	groqOnce   sync.Once
)

// initGroqClient initializes the optimized HTTP client for Groq
func initGroqClient() {
	groqOnce.Do(func() {
		groqClient = &http.Client{
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

// getGroqConfig returns Groq configuration from environment variables
func getGroqConfig() (apiKey, baseURL string) {
	apiKey = os.Getenv("GROQ_API_KEY")
	if apiKey == "" {
		apiKey = "dummy-api-key" // For development - replace with proper key handling
	}

	baseURL = os.Getenv("GROQ_API_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.groq.com/openai/v1"
	}

	return apiKey, baseURL
}

// StreamGroqResponse calls Groq API and streams the response with optimizations
func StreamGroqResponse(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, prompt string, model string, displayName string, clientID int, previousMessages []models.ChatMessage, profileContext string) error {
	// Initialize optimized client
	initGroqClient()

	startTime := time.Now()

	// Get API key and base URL from environment
	apiKey, baseURL := getGroqConfig()

	// Get the system prompt
	systemPrompt := models.Config.GetSystemPrompt("groq")

	// Format messages for Groq
	messages := []GroqMessage{}

	// Add system prompt if available
	finalSystemPrompt := systemPrompt
	if profileContext != "" {
		finalSystemPrompt += "\n\nUser Profile Context:\n" + profileContext
	}

	if finalSystemPrompt != "" {
		messages = append(messages, GroqMessage{
			Role:    "system",
			Content: finalSystemPrompt,
		})
	}

	// Add previous messages (up to the last 4)
	if len(previousMessages) > 0 {
		startIdx := 0
		if len(previousMessages) > 4 {
			startIdx = len(previousMessages) - 4
		}

		for _, msg := range previousMessages[startIdx:] {
			messages = append(messages, GroqMessage{
				Role:    msg.Role,
				Content: msg.Content,
			})
		}
	}

	// Check if the current prompt is already included in the previous messages
	addCurrentPrompt := true
	if len(previousMessages) > 0 {
		lastMsg := previousMessages[len(previousMessages)-1]
		if lastMsg.Role == "user" && lastMsg.Content == prompt {
			addCurrentPrompt = false
		}
	}

	// Add the current prompt as a user message if needed
	if addCurrentPrompt {
		messages = append(messages, GroqMessage{
			Role:    "user",
			Content: prompt,
		})
	}

	// Create the request body
	reqBody := GroqRequest{
		Model:    model,
		Messages: messages,
		Stream:   true,
		// Messages: map[string]interface{}{
		// 	"temperature": 0.8,
		// 	"top_p":       0.95,
		// },
	}

	// Prepare optimized request
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("error marshaling request: %v", err)
	}

	// Create request with context for cancellation
	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("error creating request: %v", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Cache-Control", "no-cache")

	// Make the request
	resp, err := groqClient.Do(req)
	if err != nil {
		return fmt.Errorf("error calling Groq API: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Groq API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	// API request succeeded - now send start chunk with model display name
	startResponse := models.Response{
		Message: displayName,
		Type:    "start",
		Model:   displayName,
	}

	startMsg, err := models.FormatSSEMessage(startResponse)
	if err == nil {
		fmt.Fprint(w, startMsg)
		flusher.Flush()
	}

	// Stream processing with optimized buffering
	scanner := bufio.NewScanner(resp.Body)

	// Increase buffer size for better performance
	buf := make([]byte, 64*1024) // 64KB buffer
	scanner.Buffer(buf, 64*1024)

	chunkCount := 0
	var fullResponse strings.Builder

	for scanner.Scan() {
		line := scanner.Text()
		if len(line) == 0 {
			continue
		}

		// Handle data lines
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")

			// Check for end of stream
			if data == "[DONE]" {
				break
			}

			// Parse JSON response
			var streamResp GroqResponse
			if err := json.Unmarshal([]byte(data), &streamResp); err != nil {
				// Skip invalid JSON responses
				continue
			}

			chunkCount++

			// Extract the response part
			if len(streamResp.Choices) > 0 {
				content := streamResp.Choices[0].Delta.Content
				if content != "" {
					fullResponse.WriteString(content)

					// Send chunk using structured response format (matching Gemini)
					chunkResponse := models.Response{
						Message: content,
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
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading response: %v", err)
	}

	// Send completion signal using structured format (matching Gemini)
	finalResponse := models.Response{
		Type:      "end",
		Timestamp: time.Now().Format(time.RFC3339),
	}

	msg, _ := models.FormatSSEMessage(finalResponse)
	fmt.Fprint(w, msg)
	flusher.Flush()

	log.Printf("Groq streaming completed for client %d: %d chunks in %.2fs", clientID, chunkCount, time.Since(startTime).Seconds())

	return nil
}

// CallGroqAPI calls Groq API for non-streaming requests
func CallGroqAPI(model, prompt string) (string, error) {
	// Initialize optimized client
	initGroqClient()

	// Get API key and base URL from environment
	apiKey, baseURL := getGroqConfig()

	// Create the request body
	reqBody := GroqRequest{
		Model: model,
		Messages: []GroqMessage{
			{
				Role:    "user",
				Content: prompt,
			},
		},
		Stream: false,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("error marshaling request: %v", err)
	}

	// Create request with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("error creating request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := groqClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("error calling Groq API: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("Groq API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var response GroqResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return "", fmt.Errorf("error decoding response: %v", err)
	}

	if len(response.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}

	return response.Choices[0].Delta.Content, nil
}
