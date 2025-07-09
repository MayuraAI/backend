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

	"gateway/aws"
	"gateway/config"
	"gateway/middleware"
	"gateway/models"
	"gateway/pkg/logger"
	"gateway/services"
)

// Helper function to get max of two integers
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

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
	ChatID           string               `json:"chat_id,omitempty"`
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
	user, userOk := middleware.GetFirebaseUserFromContext(ctx)
	if !userOk || user == nil {
		sendErrorResponse(w, nil, "Authentication required", clientID)
		atomic.AddInt64(&totalErrors, 1)
		return
	}

	logger.GetDailyLogger().Info("Processing request for user: %s (%s)", user.Email, user.UID)

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
	// Note: CORS headers are handled by the CORS middleware
	// w.Header().Set("Access-Control-Allow-Origin", "*")
	// w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	// w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")

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

	// STEP 1: Determine chat_id - create new chat if needed
	chatID := reqBody.ChatID

	// If no chat_id is provided, try to extract from previous messages
	if chatID == "" && len(reqBody.PreviousMessages) > 0 {
		// Try to extract chat_id from previous messages
		for _, msg := range reqBody.PreviousMessages {
			if msg.ChatID != "" {
				chatID = msg.ChatID
				break
			}
		}
	}

	// If we still don't have a chat_id, create a new chat
	if chatID == "" {
		dbCtx := context.Background()
		dbClient := aws.GetDynamoDBClient(dbCtx)

		// Generate a simple chat name from the prompt (first 50 chars)
		chatName := strings.TrimSpace(prompt)
		if len(chatName) > 50 {
			chatName = chatName[:50] + "..."
		}

		newChat := aws.Chat{
			UserID:  user.UID,
			Name:    chatName,
			Sharing: "private",
		}

		createdChat, err := aws.CreateChat(dbCtx, dbClient, newChat)
		if err != nil {
			logger.GetDailyLogger().Error("Error creating chat for client %d: %v", clientID, err)
			sendErrorResponse(w, flusher, "Failed to create chat", clientID)
			atomic.AddInt64(&totalErrors, 1)
			return
		}

		chatID = createdChat.ID
		logger.GetDailyLogger().Info("Client %d: Created new chat %s", clientID, chatID)
	} else {
		logger.GetDailyLogger().Info("Client %d: Using existing chat %s", clientID, chatID)
	}

	// STEP 2: Determine sequence number from previous messages (latest + 1)
	var nextSeq int
	if len(reqBody.PreviousMessages) > 0 {
		// Find the highest sequence number and add 1
		maxSeq := 0
		for _, msg := range reqBody.PreviousMessages {
			if msg.SequenceNumber > maxSeq {
				maxSeq = msg.SequenceNumber
			}
		}
		nextSeq = maxSeq + 1
	} else {
		// No previous messages, start with sequence 0
		nextSeq = 0
	}

	logger.GetDailyLogger().Info("Client %d: Using sequence number %d", clientID, nextSeq)

	// STEP 3: Save user message to database (blocking - must complete before proceeding)
	dbCtx := context.Background()
	dbClient := aws.GetDynamoDBClient(dbCtx)

	userMessage := aws.Message{
		ChatID:         chatID,
		UserID:         user.UID,
		Content:        prompt,
		ModelName:      "user",
		Role:           "user",
		SequenceNumber: nextSeq,
	}

	savedUserMessage, err := aws.CreateMessage(dbCtx, dbClient, userMessage)
	if err != nil {
		logger.GetDailyLogger().Error("Error saving user message for client %d: %v", clientID, err)
		sendErrorResponse(w, flusher, "Failed to save user message", clientID)
		atomic.AddInt64(&totalErrors, 1)
		return
	}

	logger.GetDailyLogger().Info("Client %d: Saved user message %s", clientID, savedUserMessage.ID)

	// STEP 4: Get model classification (can be parallel with other setup)
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

	// STEP 5: Stream response and save assistant message after completion
	err = streamWithFallbackAndSaveAfterCompletion(ctx, w, flusher, modelResponse, prompt, clientID, reqBody.PreviousMessages, reqBody.ProfileContext, user.UID, chatID, nextSeq+1)
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

// RateLimitStatusHandler returns the current rate limit status for the authenticated user
func RateLimitStatusHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get user tier from context (includes subscription service lookup)
	tier, isAnonymous := middleware.GetUserTierFromContext(ctx, r)

	// Generate rate limit key based on user context
	user, userOk := middleware.GetFirebaseUserFromContext(ctx)
	var key string

	if userOk && user != nil {
		if middleware.IsAnonymousUser(user) {
			key = "anonymous:" + user.UID
		} else {
			key = "user:" + user.UID
		}
	} else {
		// Fall back to IP address for unauthenticated users
		key = "ip:" + r.RemoteAddr
	}

	// Get tier configuration
	tierConfig, err := config.GetRateLimitConfig(tier)
	if err != nil {
		logger.GetDailyLogger().Error("Error getting tier config: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Get current usage from Redis
	freeCount, proCount, resetTime, _, _, err := middleware.GetUsageInfo(ctx, key, tier, isAnonymous)
	if err != nil {
		logger.GetDailyLogger().Error("Error getting usage info: %v", err)
		// Use fallback values
		freeCount = 0
		proCount = 0
		resetTime = time.Date(time.Now().Year(), time.Now().Month(), time.Now().Day()+1, 0, 0, 0, 0, time.Now().Location())
	}

	// Get blocking information
	isBlocked, blockedUntil, recentRequests, err := middleware.GetBlockingInfo(ctx, key, tier, isAnonymous)
	if err != nil {
		logger.GetDailyLogger().Error("Error getting blocking info: %v", err)
		// Use fallback values
		isBlocked = false
		blockedUntil = time.Time{}
		recentRequests = 0
	}

	// Calculate total requests used and remaining based on tier
	var totalUsed int
	var totalRemaining int
	var currentMode middleware.RequestType
	var message string

	if isBlocked {
		currentMode = middleware.FreeRequest
		message = fmt.Sprintf("Your account is temporarily blocked due to suspicious activity until %s", blockedUntil.Format("15:04:05"))
		totalUsed = freeCount + proCount
		totalRemaining = 0
	} else {
		// Determine current mode and calculate remaining requests
		if isAnonymous {
			// Anonymous users only have free requests
			totalUsed = freeCount + proCount
			totalRemaining = max(0, tierConfig.RequestsPerDay-totalUsed)
			currentMode = middleware.FreeRequest

			if tierConfig.LifetimeLimit {
				if totalRemaining == 0 {
					message = "You've used all your free requests. Sign up to get 100 free requests per day!"
				} else if totalRemaining == 1 {
					message = "You have 1 free request remaining. Sign up to get 100 free requests per day!"
				} else {
					message = fmt.Sprintf("You have %d free requests remaining. Sign up to get 100 free requests per day!", totalRemaining)
				}
			} else {
				message = "Anonymous users should have lifetime limits - configuration error"
			}
		} else {
			// Authenticated users - check max requests first
			if tierConfig.MaxRequests > 0 && proCount < tierConfig.MaxRequests {
				// Still have max requests
				currentMode = middleware.MaxRequest
				totalUsed = proCount
				totalRemaining = tierConfig.MaxRequests - proCount

				if totalRemaining == 1 {
					message = "You have 1 max request remaining today"
				} else {
					message = fmt.Sprintf("You have %d max requests remaining today", totalRemaining)
				}
			} else {
				// Max requests exhausted, check free requests
				currentMode = middleware.FreeRequest

				if config.IsUnlimited(tierConfig.FreeRequests) {
					totalUsed = freeCount
					totalRemaining = 999999 // Large number to indicate unlimited
					message = "You've used all your max requests for today. Continuing with unlimited free requests."
				} else {
					totalUsed = freeCount
					totalRemaining = max(0, tierConfig.FreeRequests-freeCount)

					if totalRemaining == 0 {
						message = "You've used all your requests for today."
					} else if totalRemaining == 1 {
						message = "You have 1 free request remaining today"
					} else {
						message = fmt.Sprintf("You have %d free requests remaining today", totalRemaining)
					}
				}
			}
		}
	}

	// Create response
	status := RateLimitStatus{
		DailyLimit:        tierConfig.RequestsPerDay,
		RequestsUsed:      totalUsed,
		RequestsRemaining: totalRemaining,
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
	suspiciousConfig, _ := config.GetSuspiciousActivityConfig()
	status.SuspiciousConfig.Threshold = suspiciousConfig.Threshold
	status.SuspiciousConfig.Window = fmt.Sprintf("%ds", suspiciousConfig.Window)
	status.SuspiciousConfig.Duration = fmt.Sprintf("%ds", suspiciousConfig.BlockDuration)

	// Add user info if authenticated
	if userOk && user != nil {
		status.UserID = user.UID
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

// streamWithFallbackAndSaveAfterCompletion handles streaming with fallback logic and saves assistant message AFTER streaming completes
func streamWithFallbackAndSaveAfterCompletion(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, modelResponse services.ModelResponse, prompt string, clientID int, previousMessages []models.ChatMessage, profileContext string, userID string, chatID string, assistantSeq int) error {
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
	var assistantResponse strings.Builder

	for i, model := range modelsToTry {
		logger.GetDailyLogger().Info("Trying model %d/%d: %s (%s) for client %d", i+1, len(modelsToTry), model.displayName, model.provider, clientID)

		// Create a custom response writer to capture the assistant's response
		responseCapture := &responseWriterWithCapture{
			ResponseWriter: w,
			response:       &assistantResponse,
		}

		// Try to stream with this model
		err := streamModelResponse(ctx, responseCapture, flusher, model.modelName, model.displayName, model.provider, prompt, clientID, previousMessages, profileContext, model.isThinkingModel)

		if err == nil {
			// Success! Now save the assistant's response to database AFTER streaming is complete
			if assistantResponse.Len() > 0 {
				dbCtx := context.Background()
				dbClient := aws.GetDynamoDBClient(dbCtx)

				assistantMessage := aws.Message{
					ChatID:         chatID,
					UserID:         userID,
					Content:        assistantResponse.String(),
					ModelName:      model.displayName,
					Role:           "assistant",
					SequenceNumber: assistantSeq,
				}

				savedAssistantMessage, err := aws.CreateMessage(dbCtx, dbClient, assistantMessage)
				if err != nil {
					logger.GetDailyLogger().Error("Error saving assistant message for client %d: %v", clientID, err)
					// Don't fail the request if we can't save the message, just log it
				} else {
					logger.GetDailyLogger().Info("Client %d: Saved assistant message %s after streaming completion", clientID, savedAssistantMessage.ID)
				}
			}

			logger.GetDailyLogger().Info("Successfully streamed with model %s for client %d", model.displayName, clientID)
			return nil
		}

		// Store the error for potential return
		lastError = err
		errors = append(errors, fmt.Sprintf("%s: %v", model.displayName, err))

		// Log the error and continue to next model
		logger.GetDailyLogger().Error("Model %s failed for client %d: %v", model.displayName, clientID, err)

		// Reset the response builder for the next attempt
		assistantResponse.Reset()
	}

	// All models failed - log detailed error information
	logger.GetDailyLogger().Error("All %d models failed for client %d. Errors: %v", len(modelsToTry), clientID, errors)

	// Return the last error
	if lastError != nil {
		return lastError
	}
	return fmt.Errorf("all models failed to respond")
}

// responseWriterWithCapture wraps http.ResponseWriter to capture the response content while preserving streaming
type responseWriterWithCapture struct {
	http.ResponseWriter
	response *strings.Builder
}

func (rw *responseWriterWithCapture) Write(b []byte) (int, error) {
	// Parse SSE data to extract message content
	data := string(b)
	if strings.HasPrefix(data, "data: ") {
		jsonData := strings.TrimPrefix(data, "data: ")
		jsonData = strings.TrimSuffix(jsonData, "\n\n")

		// Try to parse the JSON to extract message content
		var response struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		}

		if err := json.Unmarshal([]byte(jsonData), &response); err == nil {
			if response.Type == "chunk" && response.Message != "" {
				rw.response.WriteString(response.Message)
			}
		}
	}

	// Always write to the original response writer to maintain streaming
	return rw.ResponseWriter.Write(b)
}
