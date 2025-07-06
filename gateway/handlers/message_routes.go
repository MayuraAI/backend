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
	"gateway/middleware"
	"gateway/pkg/logger"
)

// handleMessageCombined handles both collection and individual message operations
func handleMessageCombined(w http.ResponseWriter, r *http.Request) {
	// Extract potential message ID from path
	messageID := extractPathParam(r.URL.Path, fmt.Sprintf("/%s/messages/", APIVersion))

	// If no message ID, this is a collection operation
	if messageID == "" {
		// Handle collection operations (POST to create)
		if r.Method == http.MethodPost {
			CreateMessageHandler(w, r)
		} else {
			sendAPIErrorResponse(w, "Method not allowed for collection", http.StatusMethodNotAllowed)
		}
	} else {
		// Handle individual message operations
		MessageByIDHandler(w, r)
	}
}

// SetupMessageRoutes sets up all message-related API routes
func SetupMessageRoutes(mux *http.ServeMux, apiVersion string) {
	// Message routes
	mux.HandleFunc(fmt.Sprintf("/%s/messages/chat/", apiVersion), MessageOperationsHandler)
	mux.HandleFunc(fmt.Sprintf("/%s/messages/batch", apiVersion), BatchMessagesHandler)
	mux.HandleFunc(fmt.Sprintf("/%s/messages/duplicate", apiVersion), DuplicateMessagesHandler)
	mux.HandleFunc(fmt.Sprintf("/%s/messages/delete-from-sequence", apiVersion), DeleteFromSequenceHandler)
	mux.HandleFunc(fmt.Sprintf("/%s/messages/", apiVersion), handleMessageCombined)
}

// MessageOperationsHandler handles various message operations based on the path
func MessageOperationsHandler(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	ctx := context.Background()
	client := aws.GetDynamoDBClient(ctx)

	// Handle /v1/messages/by-chat-id/{chatId} - Get messages by chat ID
	if strings.Contains(path, "/by-chat-id/") && !strings.Contains(path, "/after/") {
		chatID := extractPathParam(path, fmt.Sprintf("/%s/messages/by-chat-id/", APIVersion))
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

	// Handle /v1/messages/by-chat-id/{chatId}/after/{sequenceNumber} - Delete messages including and after
	if strings.Contains(path, "/after/") {
		if r.Method != http.MethodDelete {
			sendAPIErrorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Get authenticated user from context
		user, ok := middleware.GetFirebaseUserFromContext(r.Context())
		if !ok || user == nil {
			sendAPIErrorResponse(w, "Authentication required", http.StatusUnauthorized)
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

		// Verify user owns the chat
		chat, err := aws.GetChat(ctx, client, chatID)
		if err != nil {
			sendAPIErrorResponse(w, "Chat not found", http.StatusNotFound)
			return
		}

		if chat.UserID != user.UID {
			sendAPIErrorResponse(w, "Access denied: You can only delete messages from your own chats", http.StatusForbidden)
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

		// Validate the user_id matches the authenticated user
		if req.UserID != user.UID {
			sendAPIErrorResponse(w, "Access denied: You can only delete your own messages", http.StatusForbidden)
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

	// Get authenticated user from context
	user, ok := middleware.GetFirebaseUserFromContext(r.Context())
	if !ok || user == nil {
		sendAPIErrorResponse(w, "Authentication required", http.StatusUnauthorized)
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
		// Force the user ID to match the authenticated user
		message.UserID = user.UID

		// Verify user owns the chat
		if message.ChatID != "" {
			chat, err := aws.GetChat(ctx, client, message.ChatID)
			if err != nil {
				sendAPIErrorResponse(w, "Chat not found", http.StatusNotFound)
				return
			}

			if chat.UserID != user.UID {
				sendAPIErrorResponse(w, "Access denied: You can only create messages in your own chats", http.StatusForbidden)
				return
			}
		}

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

	// Get authenticated user from context
	user, ok := middleware.GetFirebaseUserFromContext(r.Context())
	if !ok || user == nil {
		sendAPIErrorResponse(w, "Authentication required", http.StatusUnauthorized)
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

	// Validate the user_id matches the authenticated user
	if req.UserID != user.UID {
		sendAPIErrorResponse(w, "Access denied: You can only duplicate your own messages", http.StatusForbidden)
		return
	}

	ctx := context.Background()
	client := aws.GetDynamoDBClient(ctx)

	// Verify user owns both chats
	sourceChat, err := aws.GetChat(ctx, client, req.SourceChatID)
	if err != nil {
		sendAPIErrorResponse(w, "Source chat not found", http.StatusNotFound)
		return
	}

	if sourceChat.UserID != user.UID {
		sendAPIErrorResponse(w, "Access denied: You can only duplicate messages from your own chats", http.StatusForbidden)
		return
	}

	targetChat, err := aws.GetChat(ctx, client, req.TargetChatID)
	if err != nil {
		sendAPIErrorResponse(w, "Target chat not found", http.StatusNotFound)
		return
	}

	if targetChat.UserID != user.UID {
		sendAPIErrorResponse(w, "Access denied: You can only duplicate messages to your own chats", http.StatusForbidden)
		return
	}

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

	// Get authenticated user from context
	user, ok := middleware.GetFirebaseUserFromContext(r.Context())
	if !ok || user == nil {
		sendAPIErrorResponse(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	ctx := context.Background()
	client := aws.GetDynamoDBClient(ctx)

	switch r.Method {
	case http.MethodGet:
		message, err := aws.GetMessage(ctx, client, messageID)
		if err != nil {
			logger.GetDailyLogger().Error("Error getting message: %v", err)
			sendAPIErrorResponse(w, "Message not found", http.StatusNotFound)
			return
		}

		// Verify user owns this message
		if message.UserID != user.UID {
			sendAPIErrorResponse(w, "Message not found", http.StatusNotFound)
			return
		}

		sendJSONResponse(w, message, http.StatusOK)

	case http.MethodPut:
		// First check if message exists and user owns it
		existingMessage, err := aws.GetMessage(ctx, client, messageID)
		if err != nil {
			logger.GetDailyLogger().Error("Error getting message for update: %v", err)
			sendAPIErrorResponse(w, "Message not found", http.StatusNotFound)
			return
		}

		// Verify user owns this message
		if existingMessage.UserID != user.UID {
			sendAPIErrorResponse(w, "Message not found", http.StatusNotFound)
			return
		}

		var message aws.Message
		if err := json.NewDecoder(r.Body).Decode(&message); err != nil {
			sendAPIErrorResponse(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		// Ensure the user can't change ownership
		message.ID = messageID
		message.UserID = user.UID
		message.UpdatedAt = time.Now()

		updatedMessage, err := aws.UpdateMessage(ctx, client, message)
		if err != nil {
			logger.GetDailyLogger().Error("Error updating message: %v", err)
			sendAPIErrorResponse(w, "Failed to update message", http.StatusInternalServerError)
			return
		}
		sendJSONResponse(w, updatedMessage, http.StatusOK)

	case http.MethodDelete:
		// First check if message exists and user owns it
		existingMessage, err := aws.GetMessage(ctx, client, messageID)
		if err != nil {
			logger.GetDailyLogger().Error("Error getting message for deletion: %v", err)
			sendAPIErrorResponse(w, "Message not found", http.StatusNotFound)
			return
		}

		// Verify user owns this message
		if existingMessage.UserID != user.UID {
			sendAPIErrorResponse(w, "Message not found", http.StatusNotFound)
			return
		}

		err = aws.DeleteMessage(ctx, client, messageID)
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

	// Get authenticated user from context
	user, ok := middleware.GetFirebaseUserFromContext(r.Context())
	if !ok || user == nil {
		sendAPIErrorResponse(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	var message aws.Message
	if err := json.NewDecoder(r.Body).Decode(&message); err != nil {
		sendAPIErrorResponse(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	client := aws.GetDynamoDBClient(ctx)

	// Force the user ID to match the authenticated user
	message.UserID = user.UID

	// Verify user owns the chat
	if message.ChatID != "" {
		chat, err := aws.GetChat(ctx, client, message.ChatID)
		if err != nil {
			sendAPIErrorResponse(w, "Chat not found", http.StatusNotFound)
			return
		}

		if chat.UserID != user.UID {
			sendAPIErrorResponse(w, "Access denied: You can only create messages in your own chats", http.StatusForbidden)
			return
		}
	}

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

// DeleteFromSequenceHandler handles POST /v1/messages/delete-from-sequence
func DeleteFromSequenceHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		sendAPIErrorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get authenticated user from context
	user, ok := middleware.GetFirebaseUserFromContext(r.Context())
	if !ok || user == nil {
		sendAPIErrorResponse(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	var req struct {
		UserID         string `json:"user_id"`
		ChatID         string `json:"chat_id"`
		SequenceNumber int    `json:"sequence_number"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendAPIErrorResponse(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.UserID == "" || req.ChatID == "" {
		sendAPIErrorResponse(w, "User ID and Chat ID are required", http.StatusBadRequest)
		return
	}

	// Validate the user_id matches the authenticated user
	if req.UserID != user.UID {
		sendAPIErrorResponse(w, "Access denied: You can only delete your own messages", http.StatusForbidden)
		return
	}

	ctx := context.Background()
	client := aws.GetDynamoDBClient(ctx)

	// Verify user owns the chat
	chat, err := aws.GetChat(ctx, client, req.ChatID)
	if err != nil {
		sendAPIErrorResponse(w, "Chat not found", http.StatusNotFound)
		return
	}

	if chat.UserID != user.UID {
		sendAPIErrorResponse(w, "Access denied: You can only delete messages from your own chats", http.StatusForbidden)
		return
	}

	err = aws.DeleteMessagesIncludingAndAfter(ctx, client, req.UserID, req.ChatID, req.SequenceNumber)
	if err != nil {
		logger.GetDailyLogger().Error("Error deleting messages from sequence: %v", err)
		sendAPIErrorResponse(w, "Failed to delete messages", http.StatusInternalServerError)
		return
	}

	sendJSONResponse(w, map[string]bool{"success": true}, http.StatusOK)
}
