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
	Prompt                string               `json:"prompt,omitempty"`
	PreviousMessages      []models.ChatMessage `json:"previous_messages,omitempty"`
	ProfileContext        string               `json:"profile_context,omitempty"`
	WorkspaceInstructions string               `json:"workspace_instructions,omitempty"`
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

	// Create request context with ID
	requestID := logger.GenerateRequestID()
	ctx := logger.WithRequestID(r.Context(), requestID)
	log := logger.GetLogger("client")

	// Get authenticated user from context
	user, userOk := middleware.GetSupabaseUserFromContext(ctx)
	if userOk {
		log.InfoWithFieldsCtx(ctx, "Processing request for authenticated user", map[string]interface{}{
			"user_id": user.ID.String(),
			"email":   user.Email,
		})

		// Print user details as requested
		fmt.Printf("=== Request from Authenticated User ===\n")
		fmt.Printf("User ID: %s\n", user.ID.String())
		fmt.Printf("Email: %s\n", user.Email)
		fmt.Printf("Phone: %s\n", user.Phone)
		fmt.Printf("Role: %s\n", user.Role)
		fmt.Printf("Created At: %s\n", user.CreatedAt)
		fmt.Printf("======================================\n")
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

	log.InfoWithFieldsCtx(ctx, "Client connected, processing request", map[string]interface{}{
		"client_id":          clientID,
		"processing_time_ms": time.Since(startTime).Milliseconds(),
		"has_previous_msgs":  len(reqBody.PreviousMessages) > 0,
		"has_profile_ctx":    reqBody.ProfileContext != "",
		"has_workspace_inst": reqBody.WorkspaceInstructions != "",
	})

	// Create context with timeout for the entire request
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	// Monitor context cancellation (client disconnect)
	go func() {
		<-ctx.Done()
		if ctx.Err() == context.Canceled {
			log.InfoWithFieldsCtx(ctx, "Client disconnected", map[string]interface{}{
				"client_id": clientID,
			})
		} else if ctx.Err() == context.DeadlineExceeded {
			log.WarnWithFieldsCtx(ctx, "Client request timeout", map[string]interface{}{
				"client_id": clientID,
			})
		}
	}()

	// Call the model service with timeout
	modelResponse, err := callModelServiceWithTimeout(ctx, prompt)
	if err != nil {
		log.ErrorWithFieldsCtx(ctx, "Model service error", map[string]interface{}{
			"client_id": clientID,
		}, err)
		sendErrorResponse(w, flusher, fmt.Sprintf("Model service error: %v", err), clientID)
		atomic.AddInt64(&totalErrors, 1)
		return
	}

	log.InfoWithFieldsCtx(ctx, "Model selected", map[string]interface{}{
		"client_id":      clientID,
		"selected_model": modelResponse.Metadata.SelectedModel,
		"confidence":     modelResponse.Metadata.Confidence,
	})

	// Handle different models with streaming
	selectedModel := modelResponse.Metadata.SelectedModel
	if selectedModel == "gemma3" {
		err := streamLlamaResponse(ctx, w, flusher, prompt, clientID, reqBody.PreviousMessages, reqBody.ProfileContext, reqBody.WorkspaceInstructions)
		if err != nil {
			log.ErrorWithFieldsCtx(ctx, "Streaming error", map[string]interface{}{
				"client_id": clientID,
			}, err)
			sendErrorResponse(w, flusher, fmt.Sprintf("Streaming error: %v", err), clientID)
			atomic.AddInt64(&totalErrors, 1)
			return
		}
	} else if strings.HasPrefix(selectedModel, "gemini") {
		err := streamGeminiResponse(ctx, w, flusher, prompt, selectedModel, clientID, reqBody.PreviousMessages, reqBody.ProfileContext, reqBody.WorkspaceInstructions)
		if err != nil {
			log.ErrorWithFieldsCtx(ctx, "Gemini streaming error", map[string]interface{}{
				"client_id": clientID,
			}, err)
			sendErrorResponse(w, flusher, fmt.Sprintf("Gemini streaming error: %v", err), clientID)
			atomic.AddInt64(&totalErrors, 1)
			return
		}
	} else {
		// For other models, send immediate response
		sendImmediateResponse(w, flusher, prompt, clientID)
	}

	totalTime := time.Since(startTime)
	log.InfoWithFieldsCtx(ctx, "Request completed", map[string]interface{}{
		"client_id":    clientID,
		"total_time_s": totalTime.Seconds(),
	})
}

// callModelServiceWithTimeout calls the model service with context timeout
func callModelServiceWithTimeout(ctx context.Context, prompt string) (services.ModelResponse, error) {
	// Create a channel to receive the result
	resultChan := make(chan struct {
		response services.ModelResponse
		err      error
	}, 1)

	// Call model service in goroutine
	go func() {
		response, err := services.CallModelService(prompt)
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

// streamLlamaResponse handles streaming response from Ollama
func streamLlamaResponse(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, prompt string, clientID int, previousMessages []models.ChatMessage, profileContext string, workspaceInstructions string) error {
	// chunkCount := 0
	// startTime := time.Now()

	// Send a "start" event with metadata
	startResponse := models.Response{
		Type:      "start",
		Timestamp: time.Now().Format(time.RFC3339),
		UserID:    "user_id", // Replace with actual user ID if available
		Model:     "gemma3",
	}
	msg, err := models.FormatSSEMessage(startResponse)
	if err != nil {
		return fmt.Errorf("error formatting start event: %v", err)
	}
	_, err = fmt.Fprint(w, msg)
	if err != nil {
		return fmt.Errorf("error sending start event: %v", err)
	}
	flusher.Flush()

	// Stream response from Ollama with context monitoring
	err = services.StreamOllamaResponse(ctx, w, flusher, prompt, clientID, previousMessages, profileContext, workspaceInstructions)
	if err != nil {
		// Check if error is due to context cancellation
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}

	return nil
}

// streamGeminiResponse handles streaming response from Gemini
func streamGeminiResponse(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, prompt string, model string, clientID int, previousMessages []models.ChatMessage, profileContext string, workspaceInstructions string) error {
	// chunkCount := 0
	// startTime := time.Now()

	// Send a "start" event with metadata
	startResponse := models.Response{
		Type:      "start",
		Timestamp: time.Now().Format(time.RFC3339),
		UserID:    "user_id", // Replace with actual user ID if available
		Model:     model,
	}
	msg, err := models.FormatSSEMessage(startResponse)
	if err != nil {
		return fmt.Errorf("error formatting start event: %v", err)
	}
	_, err = fmt.Fprint(w, msg)
	if err != nil {
		return fmt.Errorf("error sending start event: %v", err)
	}
	flusher.Flush()

	// Stream response from Gemini with context monitoring
	err = services.StreamGeminiResponse(ctx, w, flusher, prompt, model, clientID, previousMessages, profileContext, workspaceInstructions)
	if err != nil {
		// Check if error is due to context cancellation
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}

	return nil
}

// sendImmediateResponse sends a non-streaming response
func sendImmediateResponse(w http.ResponseWriter, flusher http.Flusher, message string, clientID int) {
	// For non-streaming, we can send a single event with all data.
	// The client can differentiate based on the "type".
	response := models.Response{
		Message:   message, // This would be the full response from the non-streaming model
		Type:      "full_response",
		Timestamp: time.Now().Format(time.RFC3339),
		UserID:    "user_id",             // Replace with actual user ID
		Model:     "non_streaming_model", // Indicate the model used
	}

	msg, err := models.FormatSSEMessage(response)
	if err != nil {
		responseLogger := logger.GetLogger("response")
		responseLogger.ErrorWithFields("Error formatting immediate response", map[string]interface{}{
			"client_id": clientID,
		}, err)
		return
	}

	fmt.Fprint(w, msg)
	flusher.Flush()
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
		errorLogger := logger.GetLogger("error")
		errorLogger.ErrorWithFields("Error formatting error response", map[string]interface{}{
			"client_id": clientID,
		}, err)
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
