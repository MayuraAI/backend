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

// OpenRouterMessage represents a message in OpenRouter format
type OpenRouterMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// OpenRouterRequest represents the request to OpenRouter API
type OpenRouterRequest struct {
	Model    string                 `json:"model"`
	Messages []OpenRouterMessage    `json:"messages"`
	Stream   bool                   `json:"stream"`
	Options  map[string]interface{} `json:"options,omitempty"`
}

// OpenRouterResponse represents the streaming response from OpenRouter API
type OpenRouterResponse struct {
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
}

// Global optimized HTTP client for OpenRouter requests
var (
	openRouterClient *http.Client
	openRouterOnce   sync.Once
)

// initOpenRouterClient initializes the optimized HTTP client for OpenRouter
func initOpenRouterClient() {
	openRouterOnce.Do(func() {
		openRouterClient = &http.Client{
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

// getOpenRouterConfig returns OpenRouter configuration from environment variables
func getOpenRouterConfig() (apiKey, baseURL string) {
	apiKey = os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		apiKey = "dummy-api-key" // For development - replace with proper key handling
	}

	baseURL = os.Getenv("OPENROUTER_API_BASE_URL")
	if baseURL == "" {
		baseURL = "https://openrouter.ai/api/v1"
	}

	return apiKey, baseURL
}

// StreamOpenRouterResponse calls OpenRouter API and streams the response with optimizations
func StreamOpenRouterResponse(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, prompt string, model string, displayName string, clientID int, previousMessages []models.ChatMessage, profileContext string) error {
	// Initialize optimized client
	initOpenRouterClient()

	startTime := time.Now()

	// Get API key and base URL from environment
	apiKey, baseURL := getOpenRouterConfig()

	// Get the system prompt
	systemPrompt := models.Config.GetSystemPrompt("openrouter")

	// Format messages for OpenRouter
	messages := []OpenRouterMessage{}

	// Add system prompt if available
	finalSystemPrompt := systemPrompt
	if profileContext != "" {
		finalSystemPrompt += "\n\nUser Profile Context:\n" + profileContext
	}

	if finalSystemPrompt != "" {
		messages = append(messages, OpenRouterMessage{
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
			messages = append(messages, OpenRouterMessage{
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
		messages = append(messages, OpenRouterMessage{
			Role:    "user",
			Content: prompt,
		})
	}

	// Create the request body
	reqBody := OpenRouterRequest{
		Model:    model,
		Messages: messages,
		Stream:   true,
		Options: map[string]interface{}{
			"temperature": 0.8,
			"top_p":       0.95,
		},
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
	resp, err := openRouterClient.Do(req)
	if err != nil {
		return fmt.Errorf("error calling OpenRouter API: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("OpenRouter API returned status %d: %s", resp.StatusCode, string(respBody))
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

		// Skip OpenRouter processing messages
		if strings.HasPrefix(line, ": OPENROUTER") {
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
			var streamResp OpenRouterResponse
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

	log.Printf("OpenRouter streaming completed for client %d: %d chunks in %.2fs", clientID, chunkCount, time.Since(startTime).Seconds())

	return nil
}

// CallOpenRouterAPI calls OpenRouter API for non-streaming requests
func CallOpenRouterAPI(model, prompt string) (string, error) {
	// Initialize optimized client
	initOpenRouterClient()

	// Get API key and base URL from environment
	apiKey, baseURL := getOpenRouterConfig()

	// Create the request body
	reqBody := OpenRouterRequest{
		Model: model,
		Messages: []OpenRouterMessage{
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

	resp, err := openRouterClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("error calling OpenRouter API: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("OpenRouter API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var response OpenRouterResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return "", fmt.Errorf("error decoding response: %v", err)
	}

	if len(response.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}

	return response.Choices[0].Delta.Content, nil
}
