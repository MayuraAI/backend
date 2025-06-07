package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"gateway/pkg/logger"
	"net/http"
	"os"
	"sync"
	"time"
)

// ModelRequest represents the request to the model service
type ModelRequest struct {
	Prompt string `json:"prompt"`
}

// ModelResponse represents the response from the model service
type ModelResponse struct {
	Model    string                `json:"model"`
	Metadata ModelResponseMetadata `json:"metadata"`
}

type ModelResponseMetadata struct {
	ProcessingTime        float64               `json:"processing_time"`
	PredictedCategory     string                `json:"predicted_category"`
	CategoryProbabilities map[string]float64    `json:"category_probabilities"`
	ModelScores           map[string]ModelScore `json:"model_scores"`
	SelectedModel         string                `json:"selected_model"`
	Confidence            float64               `json:"confidence"`
}

type ModelScore struct {
	QualityScore      float64 `json:"quality_score"`
	NormalizedQuality float64 `json:"normalized_quality"`
	Cost              float64 `json:"cost"`
	NormalizedCost    float64 `json:"normalized_cost"`
	FinalScore        float64 `json:"final_score"`
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
	return "http://localhost:8000" // Default for local development
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

// CallModelService calls the local model service with optimizations
func CallModelService(prompt string) (ModelResponse, error) {
	startTime := time.Now()

	// // Check circuit breaker
	// if !classifierCircuit.canExecute() {
	// 	return ModelResponse{}, fmt.Errorf("classifier service circuit breaker is open")
	// }

	// If circuit breaker is in half-open state, transition it
	// if classifierCircuit.state == Open && time.Since(classifierCircuit.lastFailureTime) >= classifierCircuit.recoveryTimeout {
	// 	classifierCircuit.setState(HalfOpen)
	// }

	// Prepare the request
	reqBody := ModelRequest{
		Prompt: prompt,
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
		// classifierCircuit.onFailure()
		return ModelResponse{}, fmt.Errorf("error creating request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Connection", "keep-alive")

	// Make the request using optimized client
	resp, err := classifierClient.Do(req)
	if err != nil {
		// classifierCircuit.onFailure()
		return ModelResponse{}, fmt.Errorf("error calling model service: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// classifierCircuit.onFailure()
		return ModelResponse{}, fmt.Errorf("classifier service returned status %d", resp.StatusCode)
	}

	// Parse the response
	var modelResp ModelResponse
	if err := json.NewDecoder(resp.Body).Decode(&modelResp); err != nil {
		// classifierCircuit.onFailure()
		return ModelResponse{}, fmt.Errorf("error decoding response: %v", err)
	}

	// Success - update circuit breaker
	// classifierCircuit.onSuccess()

	// Log the response details
	logModelResponse(modelResp, time.Since(startTime))

	return modelResp, nil
}

// logModelResponse logs the model response details in a formatted way
func logModelResponse(resp ModelResponse, requestTime time.Duration) {
	log := logger.GetLogger("model.service")
	log.InfoWithFields("Model service response", map[string]interface{}{
		"selected_model":     resp.Metadata.SelectedModel,
		"confidence":         resp.Metadata.Confidence,
		"predicted_category": resp.Metadata.PredictedCategory,
		"processing_time_ms": resp.Metadata.ProcessingTime * 1000,
		"request_time_ms":    requestTime.Milliseconds(),
	})
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
