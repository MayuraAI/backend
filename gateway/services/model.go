package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"gateway/middleware"
	"gateway/pkg/logger"
)

// ModelRequest represents the request to the model service
type ModelRequest struct {
	Prompt      string `json:"prompt"`
	RequestType string `json:"request_type"` // "max" or "free"
}

// ModelResponse represents the response from the model service
type ModelResponse struct {
	PrimaryModel              string                `json:"primary_model"`
	PrimaryModelDisplayName   string                `json:"primary_model_display_name"`
	SecondaryModel            string                `json:"secondary_model"`
	SecondaryModelDisplayName string                `json:"secondary_model_display_name"`
	DefaultModel              string                `json:"default_model"`
	DefaultModelDisplayName   string                `json:"default_model_display_name"`
	Metadata                  ModelResponseMetadata `json:"metadata"`
}

type ModelResponseMetadata struct {
	ProcessingTime        float64               `json:"processing_time"`
	PredictedCategory     string                `json:"predicted_category"`
	CategoryProbabilities map[string]float64    `json:"category_probabilities"`
	RequestType           string                `json:"request_type"`
	AvailableModels       int                   `json:"available_models"`
	ModelScores           map[string]ModelScore `json:"model_scores"`
	PrimaryModel          string                `json:"primary_model"`
	SecondaryModel        string                `json:"secondary_model"`
	DefaultModel          string                `json:"default_model"`
	Confidence            float64               `json:"confidence"`
}

type ModelScore struct {
	QualityScore      float64 `json:"quality_score"`
	NormalizedQuality float64 `json:"normalized_quality"`
	Cost              float64 `json:"cost"`
	NormalizedCost    float64 `json:"normalized_cost"`
	FinalScore        float64 `json:"final_score"`
	Tier              string  `json:"tier"`
	Provider          string  `json:"provider"`
	DisplayName       string  `json:"display_name"`
	ProviderModelName string  `json:"provider_model_name"`
	IsThinkingModel   bool    `json:"is_thinking_model"`
}

// Circuit breaker states
type CircuitState int

const (
	Closed CircuitState = iota
	Open
	HalfOpen
)

// Circuit breaker for classifier service
type CircuitBreaker struct {
	mu               sync.RWMutex
	state            CircuitState
	failureCount     int
	lastFailureTime  time.Time
	successCount     int
	failureThreshold int
	recoveryTimeout  time.Duration
	halfOpenMaxCalls int
}

// Global instances
var (
	// Circuit breaker for classifier service
	classifierCircuit = &CircuitBreaker{
		failureThreshold: 5,
		recoveryTimeout:  30 * time.Second,
		halfOpenMaxCalls: 3,
	}

	// Optimized HTTP client for classifier requests
	classifierClient = &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        50,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
			TLSHandshakeTimeout: 10 * time.Second,
			DisableKeepAlives:   false,
			DisableCompression:  false,
		},
	}
)

// getClassifierURL returns the classifier service URL from environment or default
func getClassifierURL() string {
	if url := os.Getenv("CLASSIFIER_URL"); url != "" {
		return url
	}
	return "http://classifier:8000" // Default for local development
}

// Circuit breaker methods
func (cb *CircuitBreaker) canExecute() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	switch cb.state {
	case Closed:
		return true
	case Open:
		return time.Since(cb.lastFailureTime) >= cb.recoveryTimeout
	case HalfOpen:
		return cb.successCount < cb.halfOpenMaxCalls
	}
	return false
}

func (cb *CircuitBreaker) onSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failureCount = 0
	if cb.state == HalfOpen {
		cb.successCount++
		if cb.successCount >= cb.halfOpenMaxCalls {
			cb.state = Closed
			cb.successCount = 0
		}
	}
}

func (cb *CircuitBreaker) onFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failureCount++
	cb.lastFailureTime = time.Now()

	if cb.state == Closed && cb.failureCount >= cb.failureThreshold {
		cb.state = Open
	} else if cb.state == HalfOpen {
		cb.state = Open
		cb.successCount = 0
	}
}

func (cb *CircuitBreaker) setState(state CircuitState) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.state = state
}

// CallModelService calls the local model service with optimizations and request type
func CallModelService(prompt string, requestType middleware.RequestType) (ModelResponse, error) {
	// Check circuit breaker
	if !classifierCircuit.canExecute() {
		return ModelResponse{}, fmt.Errorf("classifier service circuit breaker is open")
	}

	// If circuit breaker is in half-open state, transition it
	if classifierCircuit.state == Open && time.Since(classifierCircuit.lastFailureTime) >= classifierCircuit.recoveryTimeout {
		classifierCircuit.setState(HalfOpen)
	}

	// Convert RequestType to string
	requestTypeStr := "free"
	if requestType == middleware.MaxRequest {
		requestTypeStr = "max"
	}

	// Prepare the request
	reqBody := ModelRequest{
		Prompt:      prompt,
		RequestType: requestTypeStr,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return ModelResponse{}, fmt.Errorf("error marshaling request: %v", err)
	}

	// Create request with context and timeout
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	classifierURL := getClassifierURL()
	req, err := http.NewRequestWithContext(ctx, "POST", classifierURL+"/complete", bytes.NewBuffer(jsonData))
	if err != nil {
		classifierCircuit.onFailure()
		return ModelResponse{}, fmt.Errorf("error creating request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Connection", "keep-alive")

	// Make the request using optimized client
	resp, err := classifierClient.Do(req)
	if err != nil {
		classifierCircuit.onFailure()
		return ModelResponse{}, fmt.Errorf("error calling model service: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		classifierCircuit.onFailure()
		return ModelResponse{}, fmt.Errorf("classifier service returned status %d", resp.StatusCode)
	}

	// Parse the response
	var modelResp ModelResponse
	if err := json.NewDecoder(resp.Body).Decode(&modelResp); err != nil {
		classifierCircuit.onFailure()
		return ModelResponse{}, fmt.Errorf("error decoding response: %v", err)
	}

	// Success - update circuit breaker
	classifierCircuit.onSuccess()

	// Log the response for debugging
	logger.GetDailyLogger().Info("Model service response: %s (primary), %s (secondary)", modelResp.PrimaryModel, modelResp.SecondaryModel)

	return modelResp, nil
}

// CallModelServiceWithFallback calls model service with fallback to default for backward compatibility
func CallModelServiceWithFallback(prompt string) (ModelResponse, error) {
	// Default to free request type for backward compatibility
	return CallModelService(prompt, middleware.FreeRequest)
}

// GetCircuitBreakerStats returns circuit breaker statistics for monitoring
func GetCircuitBreakerStats() map[string]interface{} {
	classifierCircuit.mu.RLock()
	defer classifierCircuit.mu.RUnlock()

	return map[string]interface{}{
		"circuit_state": classifierCircuit.state,
		"failure_count": classifierCircuit.failureCount,
	}
}
