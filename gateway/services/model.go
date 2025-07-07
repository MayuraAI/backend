package services

import (
	"bytes"
	"context"
	"encoding/json"
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
	RequestType string `json:"request_type"` // "pro" or "free"
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
		failureThreshold: 3,                // Reduced threshold for faster fallback
		recoveryTimeout:  60 * time.Second, // Longer recovery time
		halfOpenMaxCalls: 2,
	}

	// Optimized HTTP client for classifier requests
	classifierClient = &http.Client{
		Timeout: 10 * time.Second, // Reduced timeout for faster fallback
		Transport: &http.Transport{
			MaxIdleConns:        50,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
			TLSHandshakeTimeout: 5 * time.Second, // Reduced timeout
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

// createFallbackResponse creates a default response when classifier is unavailable
func createFallbackResponse(requestType middleware.RequestType) ModelResponse {
	// Choose models based on request type
	var primaryModel, primaryDisplayName string
	var secondaryModel, secondaryDisplayName string

	if requestType == middleware.MaxRequest {
		// For max requests, use better models
		primaryModel = "claude-3-5-sonnet-20241022"
		primaryDisplayName = "Claude 3.5 Sonnet"
		secondaryModel = "gpt-4o"
		secondaryDisplayName = "GPT-4o"
	} else {
		// For free requests, use more affordable models
		primaryModel = "claude-3-haiku-20240307"
		primaryDisplayName = "Claude 3 Haiku"
		secondaryModel = "gpt-4o-mini"
		secondaryDisplayName = "GPT-4o Mini"
	}

	defaultModel := "gemini-1.5-flash"
	defaultDisplayName := "Gemini 1.5 Flash"

	// Create model scores for the fallback response
	modelScores := map[string]ModelScore{
		primaryModel: {
			QualityScore:      0.9,
			NormalizedQuality: 0.9,
			Cost:              0.7,
			NormalizedCost:    0.7,
			FinalScore:        0.8,
			Tier:              string(requestType),
			Provider:          "openrouter",
			DisplayName:       primaryDisplayName,
			ProviderModelName: primaryModel,
			IsThinkingModel:   false,
		},
		secondaryModel: {
			QualityScore:      0.85,
			NormalizedQuality: 0.85,
			Cost:              0.6,
			NormalizedCost:    0.6,
			FinalScore:        0.75,
			Tier:              string(requestType),
			Provider:          "openrouter",
			DisplayName:       secondaryDisplayName,
			ProviderModelName: secondaryModel,
			IsThinkingModel:   false,
		},
		defaultModel: {
			QualityScore:      0.8,
			NormalizedQuality: 0.8,
			Cost:              0.3,
			NormalizedCost:    0.3,
			FinalScore:        0.7,
			Tier:              string(requestType),
			Provider:          "gemini",
			DisplayName:       defaultDisplayName,
			ProviderModelName: defaultModel,
			IsThinkingModel:   false,
		},
	}

	return ModelResponse{
		PrimaryModel:              primaryModel,
		PrimaryModelDisplayName:   primaryDisplayName,
		SecondaryModel:            secondaryModel,
		SecondaryModelDisplayName: secondaryDisplayName,
		DefaultModel:              defaultModel,
		DefaultModelDisplayName:   defaultDisplayName,
		Metadata: ModelResponseMetadata{
			ProcessingTime:        0.001, // Minimal processing time for fallback
			PredictedCategory:     "general",
			CategoryProbabilities: map[string]float64{"general": 1.0},
			RequestType:           string(requestType),
			AvailableModels:       3,
			ModelScores:           modelScores,
			PrimaryModel:          primaryModel,
			SecondaryModel:        secondaryModel,
			DefaultModel:          defaultModel,
			Confidence:            0.5, // Lower confidence for fallback
		},
	}
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
	logger.GetDailyLogger().Info("🚨🚨🚨 FALLBACK DEBUG: CallModelService function started - checking for classifier service 🚨🚨🚨")
	logger.GetDailyLogger().Info("CallModelService called with requestType: %s", requestType)

	// Check circuit breaker - if open, immediately return fallback
	if !classifierCircuit.canExecute() {
		logger.GetDailyLogger().Warn("🔴 Classifier service circuit breaker is open, using fallback response")
		return createFallbackResponse(requestType), nil
	}

	logger.GetDailyLogger().Info("Circuit breaker allows execution, trying classifier service")

	// If circuit breaker is in half-open state, transition it
	if classifierCircuit.state == Open && time.Since(classifierCircuit.lastFailureTime) >= classifierCircuit.recoveryTimeout {
		classifierCircuit.setState(HalfOpen)
		logger.GetDailyLogger().Info("Circuit breaker transitioned to half-open state")
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
		logger.GetDailyLogger().Error("Error marshaling request, using fallback: %v", err)
		return createFallbackResponse(requestType), nil
	}

	// Create request with context and timeout
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second) // Reduced timeout
	defer cancel()

	classifierURL := getClassifierURL()
	logger.GetDailyLogger().Info("🔍 Attempting to call classifier at: %s", classifierURL)

	req, err := http.NewRequestWithContext(ctx, "POST", classifierURL+"/complete", bytes.NewBuffer(jsonData))
	if err != nil {
		classifierCircuit.onFailure()
		logger.GetDailyLogger().Error("❌ Error creating classifier request, using fallback: %v", err)
		return createFallbackResponse(requestType), nil
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Connection", "keep-alive")

	// Make the request using optimized client
	resp, err := classifierClient.Do(req)
	if err != nil {
		classifierCircuit.onFailure()
		logger.GetDailyLogger().Error("❌ Error calling classifier service, using fallback: %v", err)
		logger.GetDailyLogger().Info("Circuit breaker failure count now: %d", classifierCircuit.failureCount)
		return createFallbackResponse(requestType), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		classifierCircuit.onFailure()
		logger.GetDailyLogger().Error("❌ Classifier service returned status %d, using fallback", resp.StatusCode)
		return createFallbackResponse(requestType), nil
	}

	// Parse the response
	var modelResp ModelResponse
	if err := json.NewDecoder(resp.Body).Decode(&modelResp); err != nil {
		classifierCircuit.onFailure()
		logger.GetDailyLogger().Error("❌ Error decoding classifier response, using fallback: %v", err)
		return createFallbackResponse(requestType), nil
	}

	// Success - update circuit breaker
	classifierCircuit.onSuccess()

	// Log the response for debugging
	logger.GetDailyLogger().Info("✅ Model service response: %s (primary), %s (secondary)", modelResp.PrimaryModel, modelResp.SecondaryModel)

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

	var stateStr string
	switch classifierCircuit.state {
	case Closed:
		stateStr = "closed"
	case Open:
		stateStr = "open"
	case HalfOpen:
		stateStr = "half-open"
	}

	return map[string]interface{}{
		"circuit_state":        stateStr,
		"failure_count":        classifierCircuit.failureCount,
		"last_failure_time":    classifierCircuit.lastFailureTime,
		"failure_threshold":    classifierCircuit.failureThreshold,
		"recovery_timeout_sec": int(classifierCircuit.recoveryTimeout.Seconds()),
	}
}
