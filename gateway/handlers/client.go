package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"time"

	// "backend/middleware"
	"gateway/models"
	"gateway/services"
)

type Response struct {
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
	UserID    string `json:"user_id,omitempty"`
	Model     string `json:"model,omitempty"`
}

type RequestBody struct {
	Message string `json:"message,omitempty"`
}

func ClientHandler(w http.ResponseWriter, r *http.Request) {
	// Get user from context
	// user, ok := middleware.GetUserFromContext(r.Context())
	// if !ok {
	// 	http.Error(w, "Unauthorized", http.StatusUnauthorized)
	// 	return
	// }

	// Read request body
	var reqBody models.RequestBody
	if r.Body != nil {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Error reading request body", http.StatusBadRequest)
			return
		}
		if len(body) > 0 {
			if err := json.Unmarshal(body, &reqBody); err != nil {
				http.Error(w, "Invalid request body", http.StatusBadRequest)
				return
			}
		}
	}

	// Set response headers for streaming
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Flusher to flush data to client
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	// Assign a random ID to this client
	clientID := rand.Intn(1000000)

	// Call the model service to get the selected model
	modelResponse, err := services.CallModelService(reqBody.Message)
	if err != nil {
		log.Printf("Error calling model service: %v", err)
		// Send error response and close
		errorResponse := models.Response{
			Message:   fmt.Sprintf("Error: %v", err),
			Timestamp: time.Now().Format(time.RFC3339),
			UserID:    "user_id",
			Model:     "error",
		}
		msg, _ := models.FormatSSEMessage(errorResponse)
		fmt.Fprint(w, msg)
		flusher.Flush()
		return
	}

	// Check if the selected model is llama3.2
	selectedModel := strings.ToLower(modelResponse.Model)
	if selectedModel == "llama3.2" {
		log.Printf("Selected model is llama3.2, streaming from Ollama...")

		// Accumulate the full response
		var fullResponse strings.Builder

		// Stream response from Ollama
		err := services.StreamOllamaResponse("llama3.2", reqBody.Message, func(chunk string) error {
			// Accumulate the chunk
			fullResponse.WriteString(chunk)

			// Create SSE message with accumulated text
			response := models.Response{
				Message:   fullResponse.String(),
				Timestamp: time.Now().Format(time.RFC3339),
				UserID:    "user_id",
				Model:     "llama3.2",
			}

			msg, err := models.FormatSSEMessage(response)
			if err != nil {
				return err
			}

			_, err = fmt.Fprint(w, msg)
			if err != nil {
				return err
			}
			flusher.Flush()
			return nil
		})

		if err != nil {
			log.Printf("Error streaming from Ollama: %v", err)
			// Send error message
			errorResponse := models.Response{
				Message:   fmt.Sprintf("Ollama Error: %v", err),
				Timestamp: time.Now().Format(time.RFC3339),
				UserID:    "user_id",
				Model:     "llama3.2",
			}
			msg, _ := models.FormatSSEMessage(errorResponse)
			fmt.Fprint(w, msg)
			flusher.Flush()
		}

		// Send a final message to indicate completion
		finalResponse := models.Response{
			Message:   "[DONE]",
			Timestamp: time.Now().Format(time.RFC3339),
			UserID:    "user_id",
			Model:     "llama3.2",
		}
		msg, _ := models.FormatSSEMessage(finalResponse)
		fmt.Fprint(w, msg)
		flusher.Flush()

		// Connection will close automatically when function returns
		return
	}

	// For other models, use the original response generation
	response := models.GenerateResponse(clientID, "user_id", reqBody.Message)
	response.Timestamp = time.Now().Format(time.RFC3339)

	// Format the response as SSE message
	msg, err := models.FormatSSEMessage(response)
	if err != nil {
		log.Printf("Error formatting SSE message: %v", err)
		http.Error(w, "Error formatting response", http.StatusInternalServerError)
		return
	}

	// Send the response
	_, err = fmt.Fprint(w, msg)
	if err != nil {
		log.Printf("Error sending response: %v", err)
		return
	}
	flusher.Flush()
}
