package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gateway/aws"
	"gateway/pkg/logger"
)

// SetupMessageRoutes sets up all message-related API routes
func SetupMessageRoutes(mux *http.ServeMux, apiVersion string) {
	// Message routes
	mux.HandleFunc(fmt.Sprintf("/%s/messages/chat/", apiVersion), MessageOperationsHandler)
	mux.HandleFunc(fmt.Sprintf("/%s/messages/batch", apiVersion), BatchMessagesHandler)
	mux.HandleFunc(fmt.Sprintf("/%s/messages/duplicate", apiVersion), DuplicateMessagesHandler)
	mux.HandleFunc(fmt.Sprintf("/%s/messages/", apiVersion), MessageByIDHandler)
	mux.HandleFunc(fmt.Sprintf("/%s/messages", apiVersion), CreateMessageHandler)
}

// MessageOperationsHandler handles various message operations based on the path
func MessageOperationsHandler(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	ctx := context.Background()
	client := aws.GetDynamoDBClient(ctx)

	// Handle /v1/messages/chat/{chatId} - Get messages by chat ID
	if strings.Contains(path, "/chat/") && !strings.Contains(path, "/after/") {
		chatID := extractPathParam(path, fmt.Sprintf("/%s/messages/chat/", APIVersion))
		if chatID == "" {
			sendAPIErrorResponse(w, "Chat ID is required", http.StatusBadRequest)
			return
		}

		if r.Method != http.MethodGet {
			sendAPIErrorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		messages, err := aws.GetMessagesByChatID(ctx, client, chatID)
		if err != nil {
			logger.GetDailyLogger().Error("Error getting messages by chat ID: %v", err)
			sendAPIErrorResponse(w, "Failed to get messages", http.StatusInternalServerError)
			return
		}
		sendJSONResponse(w, messages, http.StatusOK)
		return
	}

	// Handle /v1/messages/chat/{chatId}/after/{sequenceNumber} - Delete messages including and after
	if strings.Contains(path, "/after/") {
		if r.Method != http.MethodDelete {
			sendAPIErrorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		parts := strings.Split(path, "/")
		if len(parts) < 7 {
			sendAPIErrorResponse(w, "Invalid path format", http.StatusBadRequest)
			return
		}

		chatID := parts[4]
		sequenceNumStr := parts[6]

		sequenceNumber, err := strconv.Atoi(sequenceNumStr)
		if err != nil {
			sendAPIErrorResponse(w, "Invalid sequence number", http.StatusBadRequest)
			return
		}

		// Get userID from request body or context (would need to be extracted from auth)
		var req struct {
			UserID string `json:"user_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			sendAPIErrorResponse(w, "User ID is required", http.StatusBadRequest)
			return
		}

		err = aws.DeleteMessagesIncludingAndAfter(ctx, client, req.UserID, chatID, sequenceNumber)
		if err != nil {
			logger.GetDailyLogger().Error("Error deleting messages: %v", err)
			sendAPIErrorResponse(w, "Failed to delete messages", http.StatusInternalServerError)
			return
		}
		sendJSONResponse(w, map[string]bool{"success": true}, http.StatusOK)
		return
	}
}

// BatchMessagesHandler handles POST /v1/messages/batch
func BatchMessagesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		sendAPIErrorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var messages []aws.Message
	if err := json.NewDecoder(r.Body).Decode(&messages); err != nil {
		sendAPIErrorResponse(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	client := aws.GetDynamoDBClient(ctx)

	// Create messages individually since we don't have a batch create function
	var createdMessages []*aws.Message
	for _, message := range messages {
		message.CreatedAt = time.Now()
		message.UpdatedAt = time.Now()

		createdMessage, err := aws.CreateMessage(ctx, client, message)
		if err != nil {
			logger.GetDailyLogger().Error("Error creating message: %v", err)
			sendAPIErrorResponse(w, "Failed to create messages", http.StatusInternalServerError)
			return
		}
		createdMessages = append(createdMessages, createdMessage)
	}

	sendJSONResponse(w, createdMessages, http.StatusCreated)
}

// DuplicateMessagesHandler handles POST /v1/messages/duplicate
func DuplicateMessagesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		sendAPIErrorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		SourceChatID string `json:"source_chat_id"`
		TargetChatID string `json:"target_chat_id"`
		UserID       string `json:"user_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendAPIErrorResponse(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.SourceChatID == "" || req.TargetChatID == "" || req.UserID == "" {
		sendAPIErrorResponse(w, "Source chat ID, target chat ID, and user ID are required", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	client := aws.GetDynamoDBClient(ctx)

	// Get messages from source chat
	sourceMessages, err := aws.GetMessagesByChatID(ctx, client, req.SourceChatID)
	if err != nil {
		logger.GetDailyLogger().Error("Error getting source messages: %v", err)
		sendAPIErrorResponse(w, "Failed to get source messages", http.StatusInternalServerError)
		return
	}

	// Create duplicate messages for target chat
	var createdMessages []*aws.Message
	now := time.Now()

	for _, msg := range sourceMessages {
		duplicateMsg := aws.Message{
			ChatID:         req.TargetChatID,
			UserID:         req.UserID,
			CreatedAt:      now,
			UpdatedAt:      now,
			Content:        msg.Content,
			ModelName:      msg.ModelName,
			Role:           msg.Role,
			SequenceNumber: msg.SequenceNumber,
		}

		createdMessage, err := aws.CreateMessage(ctx, client, duplicateMsg)
		if err != nil {
			logger.GetDailyLogger().Error("Error creating duplicate message: %v", err)
			sendAPIErrorResponse(w, "Failed to create duplicate messages", http.StatusInternalServerError)
			return
		}
		createdMessages = append(createdMessages, createdMessage)
	}

	sendJSONResponse(w, createdMessages, http.StatusCreated)
}

// MessageByIDHandler handles GET/PUT/DELETE /v1/messages/{messageId}
func MessageByIDHandler(w http.ResponseWriter, r *http.Request) {
	messageID := extractPathParam(r.URL.Path, fmt.Sprintf("/%s/messages/", APIVersion))
	if messageID == "" {
		sendAPIErrorResponse(w, "Message ID is required", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	client := aws.GetDynamoDBClient(ctx)

	switch r.Method {
	case http.MethodGet:
		message, err := aws.GetMessage(ctx, client, messageID)
		if err != nil {
			logger.GetDailyLogger().Error("Error getting message: %v", err)
			sendAPIErrorResponse(w, "Failed to get message", http.StatusInternalServerError)
			return
		}
		sendJSONResponse(w, message, http.StatusOK)

	case http.MethodPut:
		var message aws.Message
		if err := json.NewDecoder(r.Body).Decode(&message); err != nil {
			sendAPIErrorResponse(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		message.ID = messageID // Ensure the ID matches the URL
		message.UpdatedAt = time.Now()

		updatedMessage, err := aws.UpdateMessage(ctx, client, message)
		if err != nil {
			logger.GetDailyLogger().Error("Error updating message: %v", err)
			sendAPIErrorResponse(w, "Failed to update message", http.StatusInternalServerError)
			return
		}
		sendJSONResponse(w, updatedMessage, http.StatusOK)

	case http.MethodDelete:
		err := aws.DeleteMessage(ctx, client, messageID)
		if err != nil {
			logger.GetDailyLogger().Error("Error deleting message: %v", err)
			sendAPIErrorResponse(w, "Failed to delete message", http.StatusInternalServerError)
			return
		}
		sendJSONResponse(w, map[string]bool{"success": true}, http.StatusOK)

	default:
		sendAPIErrorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// CreateMessageHandler handles POST /v1/messages
func CreateMessageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		sendAPIErrorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var message aws.Message
	if err := json.NewDecoder(r.Body).Decode(&message); err != nil {
		sendAPIErrorResponse(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	client := aws.GetDynamoDBClient(ctx)

	message.CreatedAt = time.Now()
	message.UpdatedAt = time.Now()

	createdMessage, err := aws.CreateMessage(ctx, client, message)
	if err != nil {
		logger.GetDailyLogger().Error("Error creating message: %v", err)
		sendAPIErrorResponse(w, "Failed to create message", http.StatusInternalServerError)
		return
	}

	sendJSONResponse(w, createdMessage, http.StatusCreated)
}
