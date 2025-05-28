package models

import (
	"encoding/json"
	"fmt"
	"time"
)

type Response struct {
	Message   string `json:"message"`
	Content   string `json:"content,omitempty"` // For frontend compatibility
	Type      string `json:"type,omitempty"`    // "chunk", "done", "error"
	Timestamp string `json:"timestamp"`
	UserID    string `json:"user_id,omitempty"`
	Model     string `json:"model,omitempty"`
}

type RequestBody struct {
	Message string `json:"message,omitempty"`
}

// GenerateResponse creates a new response with the given parameters
func GenerateResponse(clientID int, userID string, reqMessage string) Response {
	message := fmt.Sprintf("Hello from client %d", clientID)
	if reqMessage != "" {
		message = fmt.Sprintf("%s - %s", message, reqMessage)
	}

	return Response{
		Message:   message,
		Timestamp: time.Now().Format(time.RFC3339),
		UserID:    userID,
		Model:     "default",
	}
}

// FormatSSEMessage formats the response as an SSE message
func FormatSSEMessage(response Response) (string, error) {
	jsonData, err := json.Marshal(response)
	if err != nil {
		return "", fmt.Errorf("error marshaling response: %v", err)
	}

	return fmt.Sprintf("data: %s\n\n", jsonData), nil
}
