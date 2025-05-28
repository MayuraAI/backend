package services

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"
)

// OllamaRequest represents the request to Ollama API
type OllamaRequest struct {
	Model   string                 `json:"model"`
	Prompt  string                 `json:"prompt"`
	Stream  bool                   `json:"stream"`
	Options map[string]interface{} `json:"options,omitempty"`
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
func StreamOllamaResponse(model, prompt string, onChunk func(string) error) error {
	// Initialize optimized client
	initOllamaClient()

	startTime := time.Now()

	// Prepare optimized request
	reqBody := OllamaRequest{
		Model:  model,
		Prompt: prompt,
		Stream: true,
		Options: map[string]interface{}{
			"temperature": 0.7,
			"top_p":       0.9,
			"top_k":       40,
			"num_predict": 2048, // Limit response length
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("error marshaling request: %v", err)
	}

	// Create request with context for cancellation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	ollamaURL := getOllamaURL()
	req, err := http.NewRequestWithContext(ctx, "POST", ollamaURL+"/api/generate", bytes.NewBuffer(jsonData))
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
		return fmt.Errorf("Ollama API returned status %d", resp.StatusCode)
	}

	// Stream processing with optimized buffering
	scanner := bufio.NewScanner(resp.Body)

	// Increase buffer size for better performance
	buf := make([]byte, 64*1024) // 64KB buffer
	scanner.Buffer(buf, 64*1024)

	chunkCount := 0
	firstChunkTime := time.Time{}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		// Parse JSON response
		var ollamaResp OllamaResponse
		if err := json.Unmarshal([]byte(line), &ollamaResp); err != nil {
			// Log error but continue processing
			continue
		}

		// Track first chunk timing
		if chunkCount == 0 {
			firstChunkTime = time.Now()
		}
		chunkCount++

		// Send chunk to handler
		if ollamaResp.Response != "" {
			if err := onChunk(ollamaResp.Response); err != nil {
				return fmt.Errorf("error processing chunk: %v", err)
			}
		}

		// Check if done
		if ollamaResp.Done {
			totalTime := time.Since(startTime)
			timeToFirst := firstChunkTime.Sub(startTime)

			// Log performance metrics
			logStreamingMetrics(model, chunkCount, timeToFirst, totalTime)
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading stream: %v", err)
	}

	return nil
}

// logStreamingMetrics logs performance metrics for monitoring
func logStreamingMetrics(model string, chunks int, timeToFirst, totalTime time.Duration) {
	avgChunkTime := float64(totalTime.Milliseconds()) / float64(chunks)

	fmt.Printf("🦙 Ollama Streaming Metrics:\n")
	fmt.Printf("   Model: %s\n", model)
	fmt.Printf("   Chunks: %d\n", chunks)
	fmt.Printf("   Time to first chunk: %.2fms\n", timeToFirst.Seconds()*1000)
	fmt.Printf("   Total time: %.2fs\n", totalTime.Seconds())
	fmt.Printf("   Avg time per chunk: %.2fms\n", avgChunkTime)
	fmt.Printf("   Throughput: %.1f chunks/sec\n", float64(chunks)/totalTime.Seconds())
}

// GetOllamaHealth checks if Ollama service is healthy
func GetOllamaHealth() error {
	initOllamaClient()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ollamaURL := getOllamaURL()
	req, err := http.NewRequestWithContext(ctx, "GET", ollamaURL+"/api/tags", nil)
	if err != nil {
		return fmt.Errorf("error creating health check request: %v", err)
	}

	resp, err := ollamaClient.Do(req)
	if err != nil {
		return fmt.Errorf("Ollama health check failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Ollama health check returned status %d", resp.StatusCode)
	}

	return nil
}

// WarmupOllamaModel preloads a model to reduce first-request latency
func WarmupOllamaModel(model string) error {
	initOllamaClient()

	reqBody := OllamaRequest{
		Model:  model,
		Prompt: "Hello", // Simple warmup prompt
		Stream: false,
		Options: map[string]interface{}{
			"num_predict": 1, // Minimal response
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("error marshaling warmup request: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ollamaURL := getOllamaURL()
	req, err := http.NewRequestWithContext(ctx, "POST", ollamaURL+"/api/generate", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("error creating warmup request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := ollamaClient.Do(req)
	if err != nil {
		return fmt.Errorf("error warming up model: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		fmt.Printf("🔥 Model %s warmed up successfully\n", model)
	}

	return nil
}

// CallOllamaAPI calls Ollama API for non-streaming response
func CallOllamaAPI(model, prompt string) (string, error) {
	// Prepare the request
	reqBody := OllamaRequest{
		Model:  model,
		Prompt: prompt,
		Stream: false,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("error marshaling request: %v", err)
	}

	// Get Ollama URL from environment
	ollamaURL := getOllamaURL()

	// Make the request to Ollama API
	resp, err := http.Post(
		ollamaURL+"/api/generate",
		"application/json",
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		return "", fmt.Errorf("error calling Ollama API: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("Ollama API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse the response
	var ollamaResp OllamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		return "", fmt.Errorf("error decoding response: %v", err)
	}

	return ollamaResp.Response, nil
}
