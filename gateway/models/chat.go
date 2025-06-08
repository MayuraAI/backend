package models

import (
	"encoding/json"
	"fmt"
	"time"
)

type Response struct {
	Message   string `json:"message,omitempty"` // Holds content for "chunk" type, or full message for others
	Content   string `json:"content,omitempty"` // Kept for potential frontend compatibility, can be removed if not used
	Type      string `json:"type"`              // "start", "chunk", "end", "error"
	Timestamp string `json:"timestamp,omitempty"`
	UserID    string `json:"user_id,omitempty"`
	Model     string `json:"model,omitempty"`
}

type ChatMessage struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	ModelName string `json:"model_name,omitempty"`
}

type RequestBody struct {
	Prompt                string        `json:"prompt,omitempty"`
	PreviousMessages      []ChatMessage `json:"previous_messages,omitempty"`
	ProfileContext        string        `json:"profile_context,omitempty"`
	WorkspaceInstructions string        `json:"workspace_instructions,omitempty"`
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
		Type:      "default", // Added a default type
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
