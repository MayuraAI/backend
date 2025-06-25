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
func StreamGroqResponse(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, prompt string, model string, displayName string, clientID int, previousMessages []models.ChatMessage, profileContext string, isThinkingModel bool) error {
	// Initialize optimized client
	initGroqClient()

	startTime := time.Now()

	// Get API key and base URL from environment
	apiKey, baseURL := getGroqConfig()

	// Get the system prompt
	systemPrompt := models.Config.GetSystemPrompt("groq")

	// Format messages for Groq
	messages := []GroqMessage{}

	// Add system prompt as a proper system message
	finalSystemPrompt := systemPrompt
	if profileContext != "" {
		finalSystemPrompt += "\n\nUser Profile Context and instructions:\n" + profileContext
	}

	if finalSystemPrompt != "" {
		messages = append(messages, GroqMessage{
			Role:    "system",
			Content: finalSystemPrompt,
		})
	}

	// Add previous messages (up to the last 4)
	// Filter out thinking blocks
	filteredMessages := []models.ChatMessage{}
	for _, msg := range previousMessages {
		if !strings.Contains(msg.Content, "◁think▷") && !strings.Contains(msg.Content, "◁/think▷") {
			filteredMessages = append(filteredMessages, msg)
		}
	}
	if len(filteredMessages) > 0 {
		startIdx := 0
		if len(filteredMessages) > 4 {
			startIdx = len(filteredMessages) - 4
		}
		for _, msg := range filteredMessages[startIdx:] {
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
	var inThinking bool = false
	var thinkingBuffer strings.Builder

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
				// If we're still in thinking mode when finishing, close it (only for thinking models)
				if isThinkingModel && inThinking {
					thinkEndResponse := models.Response{
						Message: "◁/think▷",
						Type:    "chunk",
					}
					msg, err := models.FormatSSEMessage(thinkEndResponse)
					if err == nil {
						fmt.Fprint(w, msg)
						flusher.Flush()
					}
				}
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
					// Process content for thinking blocks only for thinking models
					if isThinkingModel {
						processedContent := processThinkingContent(content, &inThinking, &thinkingBuffer, w, flusher)
						if processedContent != "" {
							fullResponse.WriteString(processedContent)

							// Send processed chunk
							chunkResponse := models.Response{
								Message: processedContent,
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
					} else {
						// For non-thinking models, send content as-is
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

// processThinkingContent processes content chunks and handles <think> tags for Groq responses
func processThinkingContent(content string, inThinking *bool, thinkingBuffer *strings.Builder, w http.ResponseWriter, flusher http.Flusher) string {
	// Decode Unicode escape sequences in the content
	decodedContent := decodeUnicodeEscapes(content)

	// If we're not in thinking mode, check if this chunk starts thinking
	if !*inThinking {
		// Check if the new content contains <think>
		if strings.Contains(decodedContent, "<think>") {
			parts := strings.Split(decodedContent, "<think>")
			beforeThink := parts[0]
			afterThink := ""
			if len(parts) > 1 {
				afterThink = strings.Join(parts[1:], "<think>") // In case there are multiple <think> tags
			}

			// Send content before <think> as regular output
			outputContent := beforeThink

			// Send thinking start marker
			thinkStartResponse := models.Response{
				Message: "◁think▷",
				Type:    "chunk",
			}
			msg, err := models.FormatSSEMessage(thinkStartResponse)
			if err == nil {
				fmt.Fprint(w, msg)
				flusher.Flush()
			}

			*inThinking = true

			// Process the content after <think>
			if afterThink != "" {
				return outputContent + processThinkingContentRecursive(afterThink, inThinking, w, flusher)
			}

			return outputContent
		} else {
			// No thinking tag, return as regular content
			return decodedContent
		}
	} else {
		// We're in thinking mode, check if this chunk ends thinking
		if strings.Contains(decodedContent, "</think>") {
			parts := strings.Split(decodedContent, "</think>")
			thinkingContent := parts[0]
			afterThink := ""
			if len(parts) > 1 {
				afterThink = strings.Join(parts[1:], "</think>") // In case there are multiple </think> tags
			}

			// Send thinking content
			if thinkingContent != "" {
				thinkingResponse := models.Response{
					Message: thinkingContent,
					Type:    "chunk",
				}
				msg, err := models.FormatSSEMessage(thinkingResponse)
				if err == nil {
					fmt.Fprint(w, msg)
					flusher.Flush()
				}
			}

			// Send thinking end marker
			thinkEndResponse := models.Response{
				Message: "◁/think▷",
				Type:    "chunk",
			}
			msg, err := models.FormatSSEMessage(thinkEndResponse)
			if err == nil {
				fmt.Fprint(w, msg)
				flusher.Flush()
			}

			*inThinking = false

			// Return content after </think> as regular output
			return afterThink
		} else {
			// Still in thinking mode, send as thinking content
			if decodedContent != "" {
				thinkingResponse := models.Response{
					Message: decodedContent,
					Type:    "chunk",
				}
				msg, err := models.FormatSSEMessage(thinkingResponse)
				if err == nil {
					fmt.Fprint(w, msg)
					flusher.Flush()
				}
			}
			return "" // No regular output when in thinking mode
		}
	}
}

// processThinkingContentRecursive handles recursive processing when there are multiple tags in one chunk
func processThinkingContentRecursive(content string, inThinking *bool, w http.ResponseWriter, flusher http.Flusher) string {
	if !*inThinking {
		return content // Should not happen, but safety check
	}

	if strings.Contains(content, "</think>") {
		parts := strings.Split(content, "</think>")
		thinkingContent := parts[0]
		afterThink := ""
		if len(parts) > 1 {
			afterThink = strings.Join(parts[1:], "</think>")
		}

		// Send thinking content
		if thinkingContent != "" {
			thinkingResponse := models.Response{
				Message: thinkingContent,
				Type:    "chunk",
			}
			msg, err := models.FormatSSEMessage(thinkingResponse)
			if err == nil {
				fmt.Fprint(w, msg)
				flusher.Flush()
			}
		}

		// Send thinking end marker
		thinkEndResponse := models.Response{
			Message: "◁/think▷",
			Type:    "chunk",
		}
		msg, err := models.FormatSSEMessage(thinkEndResponse)
		if err == nil {
			fmt.Fprint(w, msg)
			flusher.Flush()
		}

		*inThinking = false
		return afterThink
	} else {
		// Send as thinking content
		if content != "" {
			thinkingResponse := models.Response{
				Message: content,
				Type:    "chunk",
			}
			msg, err := models.FormatSSEMessage(thinkingResponse)
			if err == nil {
				fmt.Fprint(w, msg)
				flusher.Flush()
			}
		}
		return ""
	}
}

// decodeUnicodeEscapes decodes Unicode escape sequences in JSON strings
func decodeUnicodeEscapes(s string) string {
	// Replace common Unicode escapes that might appear in thinking tags
	s = strings.ReplaceAll(s, "\\u003c", "◁")  // \u003c -> <
	s = strings.ReplaceAll(s, "\\u003e", "▷")  // \u003e -> >
	s = strings.ReplaceAll(s, "\\u002f", "/")  // \u002f -> /
	s = strings.ReplaceAll(s, "\\u0022", "\"") // \u0022 -> "
	s = strings.ReplaceAll(s, "\\u0027", "'")  // \u0027 -> '
	s = strings.ReplaceAll(s, "\\u0026", "&")  // \u0026 -> &

	// Handle other common escape sequences
	s = strings.ReplaceAll(s, "\\n", "\n")  // \n -> newline
	s = strings.ReplaceAll(s, "\\t", "\t")  // \t -> tab
	s = strings.ReplaceAll(s, "\\r", "\r")  // \r -> carriage return
	s = strings.ReplaceAll(s, "\\\\", "\\") // \\ -> \

	return s
}
