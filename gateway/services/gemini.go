package services

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"gateway/pkg/logger"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// GeminiRequest represents the request to Gemini API
type GeminiRequest struct {
	Contents       []GeminiContent `json:"contents"`
	SafetySettings []struct {
		Category  string `json:"category"`
		Threshold string `json:"threshold"`
	} `json:"safetySettings,omitempty"`
	GenerationConfig struct {
		Temperature     float64 `json:"temperature,omitempty"`
		MaxOutputTokens int     `json:"maxOutputTokens,omitempty"`
		TopP            float64 `json:"topP,omitempty"`
		TopK            int     `json:"topK,omitempty"`
	} `json:"generationConfig,omitempty"`
}

// GeminiContent represents a content part in a Gemini request
type GeminiContent struct {
	Parts []struct {
		Text string `json:"text"`
	} `json:"parts"`
	Role string `json:"role,omitempty"`
}

// GeminiResponse represents a response from the Gemini API
type GeminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	PromptFeedback struct {
		BlockReason string `json:"blockReason,omitempty"`
	} `json:"promptFeedback,omitempty"`
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error,omitempty"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		TotalTokenCount      int `json:"totalTokenCount"`
	} `json:"usageMetadata,omitempty"`
}

// Global optimized HTTP client for Gemini requests
var (
	geminiClient *http.Client
	geminiOnce   sync.Once
)

// initGeminiClient initializes the optimized HTTP client for Gemini
func initGeminiClient() {
	geminiOnce.Do(func() {
		geminiClient = &http.Client{
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

// getGeminiConfig returns Gemini configuration from environment variables
func getGeminiConfig() (apiKey, modelName, baseURL string) {
	apiKey = os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		apiKey = "dummy-api-key" // For development - replace with proper key handling
	}

	modelName = os.Getenv("GEMINI_MODEL_NAME")
	if modelName == "" {
		modelName = "gemini-2.0-flash-exp" // Default model
	}

	baseURL = os.Getenv("GEMINI_API_BASE_URL")
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com/v1beta/models"
	}

	return apiKey, modelName, baseURL
}

// StreamGeminiResponse calls Gemini API and streams the response with optimizations
func StreamGeminiResponse(model, prompt string, onChunk func(string) error) error {
	// Initialize optimized client
	initGeminiClient()

	startTime := time.Now()
	apiKey, modelName, baseURL := getGeminiConfig()

	// Use provided model or fall back to default
	if model != "" {
		modelName = model
	}

	// Prepare optimized request
	reqBody := GeminiRequest{
		Contents: []GeminiContent{
			{
				Parts: []struct {
					Text string `json:"text"`
				}{
					{Text: prompt},
				},
			},
		},
		GenerationConfig: struct {
			Temperature     float64 `json:"temperature,omitempty"`
			MaxOutputTokens int     `json:"maxOutputTokens,omitempty"`
			TopP            float64 `json:"topP,omitempty"`
			TopK            int     `json:"topK,omitempty"`
		}{
			Temperature:     0.7,
			MaxOutputTokens: 2048,
			TopP:            0.95,
			TopK:            40,
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("error marshaling request: %v", err)
	}

	// Create streaming URL
	url := fmt.Sprintf("%s/%s:streamGenerateContent?alt=sse&key=%s", baseURL, modelName, apiKey)

	// Create request with context for cancellation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("error creating request: %v", err)
	}

	// Optimize headers for streaming
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Cache-Control", "no-cache")

	// Make the request
	resp, err := geminiClient.Do(req)
	if err != nil {
		return fmt.Errorf("error calling Gemini API: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Gemini API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	// Stream processing with optimized buffering
	scanner := bufio.NewScanner(resp.Body)

	// Increase buffer size for better performance
	buf := make([]byte, 64*1024) // 64KB buffer
	scanner.Buffer(buf, 64*1024)

	chunkCount := 0
	firstChunkTime := time.Time{}
	var fullResponse strings.Builder

	for scanner.Scan() {
		line := scanner.Text()
		if len(line) == 0 {
			continue
		}

		// Parse SSE format - strip "data: " prefix
		if strings.HasPrefix(line, "data: ") {
			line = strings.TrimPrefix(line, "data: ")
		} else {
			// Skip non-data lines
			continue
		}

		// Parse JSON response
		var streamResp map[string]interface{}
		if err := json.Unmarshal([]byte(line), &streamResp); err != nil {
			// Log error but continue processing
			continue
		}

		// Track first chunk timing
		if chunkCount == 0 {
			firstChunkTime = time.Now()
		}
		chunkCount++

		// Extract the response part
		var chunkText string
		isFinal := false

		// Navigate through the JSON structure to find the text
		if candidates, ok := streamResp["candidates"].([]interface{}); ok && len(candidates) > 0 {
			if candidate, ok := candidates[0].(map[string]interface{}); ok {
				if content, ok := candidate["content"].(map[string]interface{}); ok {
					if parts, ok := content["parts"].([]interface{}); ok && len(parts) > 0 {
						if part, ok := parts[0].(map[string]interface{}); ok {
							if text, ok := part["text"].(string); ok {
								chunkText = text
								fullResponse.WriteString(text)
							}
						}
					}
				}

				// Check if this is the final message with finishReason
				if finishReason, ok := candidate["finishReason"].(string); ok && finishReason != "" {
					isFinal = true
				}
			}
		}

		// Send chunk to handler if there's content
		if chunkText != "" {
			if err := onChunk(chunkText); err != nil {
				return fmt.Errorf("error processing chunk: %v", err)
			}
		}

		// Check if done
		if isFinal {
			totalTime := time.Since(startTime)
			timeToFirst := firstChunkTime.Sub(startTime)

			// Log performance metrics
			logGeminiStreamingMetrics(modelName, chunkCount, timeToFirst, totalTime, fullResponse.Len())
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading stream: %v", err)
	}

	return nil
}

// logGeminiStreamingMetrics logs performance metrics for monitoring
func logGeminiStreamingMetrics(model string, chunks int, timeToFirst, totalTime time.Duration, responseLength int) {
	avgChunkTime := float64(totalTime.Milliseconds()) / float64(chunks)

	log := logger.GetLogger("gemini.metrics")
	log.InfoWithFields("Gemini streaming metrics", map[string]interface{}{
		"model":                     model,
		"chunks":                    chunks,
		"response_length":           responseLength,
		"time_to_first_ms":          timeToFirst.Milliseconds(),
		"total_time_s":              totalTime.Seconds(),
		"avg_chunk_time_ms":         avgChunkTime,
		"throughput_chunks_per_sec": float64(chunks) / totalTime.Seconds(),
	})
}

// CallGeminiAPI calls Gemini API for non-streaming response
func CallGeminiAPI(model, prompt string) (string, error) {
	initGeminiClient()

	apiKey, modelName, baseURL := getGeminiConfig()

	// Use provided model or fall back to default
	if model != "" {
		modelName = model
	}

	// Prepare the request
	reqBody := GeminiRequest{
		Contents: []GeminiContent{
			{
				Parts: []struct {
					Text string `json:"text"`
				}{
					{Text: prompt},
				},
			},
		},
		GenerationConfig: struct {
			Temperature     float64 `json:"temperature,omitempty"`
			MaxOutputTokens int     `json:"maxOutputTokens,omitempty"`
			TopP            float64 `json:"topP,omitempty"`
			TopK            int     `json:"topK,omitempty"`
		}{
			Temperature:     0.7,
			MaxOutputTokens: 2048,
			TopP:            0.95,
			TopK:            40,
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("error marshaling request: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	url := fmt.Sprintf("%s/%s:generateContent?key=%s", baseURL, modelName, apiKey)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("error creating request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Make the request to Gemini API
	resp, err := geminiClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("error calling Gemini API: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("Gemini API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse the response
	var geminiResp GeminiResponse
	if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil {
		return "", fmt.Errorf("error decoding response: %v", err)
	}

	// Check for API-reported errors
	if geminiResp.Error.Message != "" {
		return "", fmt.Errorf("Gemini API error: %s", geminiResp.Error.Message)
	}

	// Extract response text
	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("no response text found in Gemini API response")
	}

	return geminiResp.Candidates[0].Content.Parts[0].Text, nil
}
