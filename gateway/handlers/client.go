package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"gateway/middleware"
	"gateway/models"
	"gateway/pkg/logger"
	"gateway/services"
)

type Response struct {
	Prompt    string `json:"prompt"`
	Timestamp string `json:"timestamp"`
	UserID    string `json:"user_id,omitempty"`
	Model     string `json:"model,omitempty"`
	UserEmail string `json:"user_email,omitempty"`
}

type RequestBody struct {
	Prompt           string               `json:"prompt,omitempty"`
	PreviousMessages []models.ChatMessage `json:"previous_messages,omitempty"`
	ProfileContext   string               `json:"profile_context,omitempty"`
	// WorkspaceInstructions string               `json:"workspace_instructions,omitempty"`
}

// RateLimitStatus represents the current rate limiting status for a user
type RateLimitStatus struct {
	DailyLimit        int                    `json:"daily_limit"`
	RequestsUsed      int                    `json:"requests_used"`
	RequestsRemaining int                    `json:"requests_remaining"`
	CurrentMode       middleware.RequestType `json:"current_mode"` // "pro" or "free"
	ResetTime         time.Time              `json:"reset_time"`
	ResetTimeUnix     int64                  `json:"reset_time_unix"`
	UserID            string                 `json:"user_id,omitempty"`
	UserEmail         string                 `json:"user_email,omitempty"`
	Message           string                 `json:"message"`

	// Suspicious activity tracking
	IsBlocked        bool      `json:"is_blocked"`
	BlockedUntil     time.Time `json:"blocked_until,omitempty"`
	BlockedUntilUnix int64     `json:"blocked_until_unix,omitempty"`
	RecentRequests   int       `json:"recent_requests"` // Requests in last minute
	SuspiciousConfig struct {
		Threshold int    `json:"threshold"`
		Window    string `json:"window"`
		Duration  string `json:"block_duration"`
	} `json:"suspicious_config"`
}

// Global metrics for monitoring
var (
	activeConnections int64
	totalRequests     int64
	totalErrors       int64
)

// ClientHandler handles streaming chat completions with optimizations
func ClientHandler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	clientID := rand.Intn(1000000)

	logger.GetDailyLogger().Info("Client %d: New request started from %s", clientID, r.RemoteAddr)

	// Create request context with ID
	ctx := r.Context()

	// Get authenticated user from context
	user, userOk := middleware.GetSupabaseUserFromContext(ctx)
	if userOk {
		logger.GetDailyLogger().Info("Processing request for user: %s (%s)", user.Email, user.ID.String())
	}

	// Get request type from context (set by rate limiter)
	requestType, hasRequestType := middleware.GetRequestTypeFromContext(ctx)
	if hasRequestType {
		logger.GetDailyLogger().Info("Request type: %s", string(requestType))
	}

	// Increment metrics
	atomic.AddInt64(&totalRequests, 1)
	atomic.AddInt64(&activeConnections, 1)
	defer atomic.AddInt64(&activeConnections, -1)

	// Set optimized response headers for streaming
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")

	// Handle preflight requests
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Validate flusher capability
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		atomic.AddInt64(&totalErrors, 1)
		return
	}

	// Read and validate request body with size limit
	var reqBody models.RequestBody
	if r.Body != nil {
		// Limit request body size to 1MB
		limitedReader := io.LimitReader(r.Body, 1024*1024)
		body, err := io.ReadAll(limitedReader)
		if err != nil {
			sendErrorResponse(w, flusher, "Error reading request body", clientID)
			atomic.AddInt64(&totalErrors, 1)
			return
		}

		if len(body) > 0 {
			if err := json.Unmarshal(body, &reqBody); err != nil {
				sendErrorResponse(w, flusher, "Invalid request body", clientID)
				atomic.AddInt64(&totalErrors, 1)
				return
			}
		}
	}

	// Get the prompt from either prompt field
	prompt := reqBody.Prompt

	// Validate prompt content
	if strings.TrimSpace(prompt) == "" {
		sendErrorResponse(w, flusher, "Prompt cannot be empty", clientID)
		atomic.AddInt64(&totalErrors, 1)
		return
	}

	logger.GetDailyLogger().Info("Client %d: Processing prompt request (%d chars)", clientID, len(prompt))

	// Create context with timeout for the entire request
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	// Monitor context cancellation (client disconnect)
	go func() {
		<-ctx.Done()
		if ctx.Err() == context.Canceled {
			logger.GetDailyLogger().Info("Client %d disconnected", clientID)
		} else if ctx.Err() == context.DeadlineExceeded {
			logger.GetDailyLogger().Info("Client %d request timeout", clientID)
		}
	}()

	// Call the model service with timeout
	modelResponse, err := callModelServiceWithTimeout(ctx, prompt, requestType)
	if err != nil {
		logger.GetDailyLogger().Error("Model service error for client %d: %v", clientID, err)
		sendErrorResponse(w, flusher, fmt.Sprintf("Model service error: %v", err), clientID)
		atomic.AddInt64(&totalErrors, 1)
		return
	}

	logger.GetDailyLogger().Info("Selected model: %s (%s)", modelResponse.PrimaryModel, modelResponse.PrimaryModelDisplayName)

	// Use the new fallback streaming logic
	err = streamWithFallback(ctx, w, flusher, modelResponse, prompt, clientID, reqBody.PreviousMessages, reqBody.ProfileContext)
	if err != nil {
		logger.GetDailyLogger().Error("Streaming error for client %d: %v", clientID, err)
		sendErrorResponse(w, flusher, "Models not available currently. Please try again later.", clientID)
		atomic.AddInt64(&totalErrors, 1)
		return
	}

	logger.GetDailyLogger().Info("Request completed for client %d in %.2fs", clientID, time.Since(startTime).Seconds())
}

// callModelServiceWithTimeout calls the model service with context timeout
func callModelServiceWithTimeout(ctx context.Context, prompt string, requestType middleware.RequestType) (services.ModelResponse, error) {
	// Create a channel to receive the result
	resultChan := make(chan struct {
		response services.ModelResponse
		err      error
	}, 1)

	// Call model service in goroutine
	go func() {
		response, err := services.CallModelService(prompt, requestType)
		resultChan <- struct {
			response services.ModelResponse
			err      error
		}{response, err}
	}()

	// Wait for result or timeout
	select {
	case result := <-resultChan:
		return result.response, result.err
	case <-ctx.Done():
		return services.ModelResponse{}, ctx.Err()
	}
}

// sendErrorResponse sends an error response in SSE format
func sendErrorResponse(w http.ResponseWriter, flusher http.Flusher, errorMsg string, clientID int) {
	errorResponse := models.Response{
		Message:   fmt.Sprintf("Error: %s", errorMsg),
		Type:      "error",
		Timestamp: time.Now().Format(time.RFC3339),
		// UserID and Model are omitted for error messages in the new format
	}

	msg, err := models.FormatSSEMessage(errorResponse)
	if err != nil {
		logger.GetDailyLogger().Error("Error formatting error response for client %d: %v", clientID, err)
		// Fallback to plain HTTP error if SSE formatting fails
		http.Error(w, errorMsg, http.StatusInternalServerError)
		return
	}

	fmt.Fprint(w, msg)
	flusher.Flush()
}

// GetMetrics returns current performance metrics
func GetMetrics() map[string]interface{} {
	return map[string]interface{}{
		"active_connections":    atomic.LoadInt64(&activeConnections),
		"total_requests":        atomic.LoadInt64(&totalRequests),
		"total_errors":          atomic.LoadInt64(&totalErrors),
		"error_rate":            float64(atomic.LoadInt64(&totalErrors)) / float64(atomic.LoadInt64(&totalRequests)),
		"circuit_breaker_stats": services.GetCircuitBreakerStats(),
	}
}

// RateLimitStatusHandler returns the current rate limiting status for the authenticated user
func RateLimitStatusHandler(w http.ResponseWriter, r *http.Request) {
	// Set CORS headers
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")

	// Handle preflight requests
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Only allow GET requests
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get authenticated user from context
	user, userOk := middleware.GetSupabaseUserFromContext(r.Context())

	// Get rate limit key (same logic as rate limiter)
	var key string
	if userOk && user != nil {
		key = "user:" + user.ID.String()
	} else {
		// Fall back to IP address for unauthenticated users
		key = "user:global"
	}

	// Get current usage from the global rate limiter
	usage := middleware.GetGlobalRateLimiter().GetOrCreateUsage(key)
	currentCount, resetTime, _, _ := usage.GetUsageInfo()

	// Get blocking information
	isBlocked, blockedUntil, recentRequests := usage.GetBlockingInfo()

	// Get the configuration
	config := middleware.GetDefaultConfig()
	dailyLimit := config.RequestsPerDay

	// Calculate remaining requests
	remaining := dailyLimit - currentCount
	if remaining < 0 {
		remaining = 0
	}

	// Determine current mode and message
	var currentMode middleware.RequestType
	var message string

	if isBlocked {
		currentMode = middleware.FreeRequest
		message = fmt.Sprintf("Your account is temporarily blocked due to suspicious activity until %s", blockedUntil.Format("15:04:05"))
	} else if currentCount < dailyLimit {
		currentMode = middleware.ProRequest
		if remaining == 1 {
			message = "You have 1 pro request remaining today"
		} else {
			message = fmt.Sprintf("You have %d pro requests remaining today", remaining)
		}
	} else {
		currentMode = middleware.FreeRequest
		message = "You've used all your pro requests for today."
	}

	// Create response
	status := RateLimitStatus{
		DailyLimit:        dailyLimit,
		RequestsUsed:      currentCount,
		RequestsRemaining: remaining,
		CurrentMode:       currentMode,
		ResetTime:         resetTime,
		ResetTimeUnix:     resetTime.Unix(),
		Message:           message,
		IsBlocked:         isBlocked,
		RecentRequests:    recentRequests,
	}

	// Add blocking information if blocked
	if isBlocked {
		status.BlockedUntil = blockedUntil
		status.BlockedUntilUnix = blockedUntil.Unix()
	}

	// Add suspicious activity configuration
	status.SuspiciousConfig.Threshold = config.SuspiciousThreshold
	status.SuspiciousConfig.Window = config.SuspiciousWindow.String()
	status.SuspiciousConfig.Duration = config.BlockDuration.String()

	// Add user info if authenticated
	if userOk && user != nil {
		status.UserID = user.ID.String()
		status.UserEmail = user.Email
	}

	// Return JSON response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(status); err != nil {
		logger.GetDailyLogger().Error("Error encoding rate limit status: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

// streamModelResponse handles streaming with fallback logic for different providers
func streamModelResponse(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, modelName string, displayName string, providerName string, prompt string, clientID int, previousMessages []models.ChatMessage, profileContext string, isThinkingModel bool) error {
	var err error

	// Route to appropriate provider based on provider name
	switch providerName {
	case "gemini":
		err = services.StreamGeminiResponse(ctx, w, flusher, prompt, modelName, displayName, clientID, previousMessages, profileContext, isThinkingModel)
	case "openrouter":
		err = services.StreamOpenRouterResponse(ctx, w, flusher, prompt, modelName, displayName, clientID, previousMessages, profileContext, isThinkingModel)
	case "groq":
		err = services.StreamGroqResponse(ctx, w, flusher, prompt, modelName, displayName, clientID, previousMessages, profileContext, isThinkingModel)
	default:
		return fmt.Errorf("unsupported provider: %s", providerName)
	}

	if err != nil {
		// Check if error is due to context cancellation
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}

	return nil
}

// streamWithFallback tries models in order with fallback logic
func streamWithFallback(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, modelResponse services.ModelResponse, prompt string, clientID int, previousMessages []models.ChatMessage, profileContext string) error {
	modelsToTry := []struct {
		modelName       string
		provider        string
		displayName     string
		isThinkingModel bool
	}{}

	// Extract model information from response metadata
	if len(modelResponse.Metadata.ModelScores) == 0 {
		modelsToTry = append(modelsToTry, struct {
			modelName       string
			provider        string
			displayName     string
			isThinkingModel bool
		}{
			modelName:       modelResponse.DefaultModel,
			provider:        "default", // Fallback provider
			displayName:     modelResponse.DefaultModelDisplayName,
			isThinkingModel: false, // Default to false for fallback
		})
	} else {
		if primaryScore, exists := modelResponse.Metadata.ModelScores[modelResponse.PrimaryModel]; exists {
			modelsToTry = append(modelsToTry, struct {
				modelName       string
				provider        string
				displayName     string
				isThinkingModel bool
			}{
				modelName:       primaryScore.ProviderModelName,
				provider:        primaryScore.Provider,
				displayName:     primaryScore.DisplayName,
				isThinkingModel: primaryScore.IsThinkingModel,
			})
		}

		if secondaryScore, exists := modelResponse.Metadata.ModelScores[modelResponse.SecondaryModel]; exists {
			modelsToTry = append(modelsToTry, struct {
				modelName       string
				provider        string
				displayName     string
				isThinkingModel bool
			}{
				modelName:       secondaryScore.ProviderModelName,
				provider:        secondaryScore.Provider,
				displayName:     secondaryScore.DisplayName,
				isThinkingModel: secondaryScore.IsThinkingModel,
			})
		}

		// Add default model as fallback
		if defaultScore, exists := modelResponse.Metadata.ModelScores[modelResponse.DefaultModel]; exists {
			modelsToTry = append(modelsToTry, struct {
				modelName       string
				provider        string
				displayName     string
				isThinkingModel bool
			}{
				modelName:       defaultScore.ProviderModelName,
				provider:        defaultScore.Provider,
				displayName:     defaultScore.DisplayName,
				isThinkingModel: defaultScore.IsThinkingModel,
			})
		}
	}

	// Try models in order
	var lastError error
	var errors []string

	for i, model := range modelsToTry {
		logger.GetDailyLogger().Info("Trying model %d/%d: %s (%s) for client %d", i+1, len(modelsToTry), model.displayName, model.provider, clientID)

		// Try to stream with this model
		err := streamModelResponse(ctx, w, flusher, model.modelName, model.displayName, model.provider, prompt, clientID, previousMessages, profileContext, model.isThinkingModel)

		if err == nil {
			// Success!
			logger.GetDailyLogger().Info("Successfully streamed with model %s for client %d", model.displayName, clientID)
			return nil
		}

		// Store the error for potential return
		lastError = err
		errors = append(errors, fmt.Sprintf("%s: %v", model.displayName, err))

		// Log the error and continue to next model
		logger.GetDailyLogger().Error("Model %s failed for client %d: %v", model.displayName, clientID, err)
	}

	// All models failed - log detailed error information
	logger.GetDailyLogger().Error("All %d models failed for client %d. Errors: %v", len(modelsToTry), clientID, errors)

	// Return the last error
	if lastError != nil {
		return lastError
	}
	return fmt.Errorf("all models failed to respond")
}
