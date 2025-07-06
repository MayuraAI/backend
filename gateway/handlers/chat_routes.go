package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"gateway/aws"
	"gateway/middleware"
	"gateway/pkg/logger"
)

// handleChatCombined handles both collection and individual chat operations
func handleChatCombined(w http.ResponseWriter, r *http.Request) {
	// Extract potential chat ID from path
	chatID := extractPathParam(r.URL.Path, fmt.Sprintf("/%s/chats/", APIVersion))

	// If no chat ID, this is a collection operation
	if chatID == "" {
		// Handle collection operations (POST to create)
		if r.Method == http.MethodPost {
			CreateChatHandler(w, r)
		} else {
			sendAPIErrorResponse(w, "Method not allowed for collection", http.StatusMethodNotAllowed)
		}
	} else {
		// Handle individual chat operations
		ChatOperationsHandler(w, r)
	}
}

// SetupChatRoutes sets up all chat-related API routes
func SetupChatRoutes(mux *http.ServeMux, apiVersion string) {
	// Chat routes
	mux.HandleFunc(fmt.Sprintf("/%s/chats/user/", apiVersion), ChatsByUserIDHandler)
	mux.HandleFunc(fmt.Sprintf("/%s/chats/batch", apiVersion), BatchChatsHandler)
	mux.HandleFunc(fmt.Sprintf("/%s/chats/", apiVersion), handleChatCombined)
}

// ChatsByUserIDHandler handles GET /v1/chats/by-user-id/{userId}
func ChatsByUserIDHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		sendAPIErrorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get authenticated user from context instead of URL parameter
	user, ok := middleware.GetFirebaseUserFromContext(r.Context())
	if !ok || user == nil {
		sendAPIErrorResponse(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	ctx := context.Background()
	client := aws.GetDynamoDBClient(ctx)

	// Use authenticated user's UID instead of URL parameter
	chats, err := aws.GetChatsByUserID(ctx, client, user.UID)
	if err != nil {
		logger.GetDailyLogger().Error("Error getting chats by user ID: %v", err)
		sendAPIErrorResponse(w, "Failed to get chats", http.StatusInternalServerError)
		return
	}

	sendJSONResponse(w, chats, http.StatusOK)
}

// BatchChatsHandler handles POST /v1/chats/batch
func BatchChatsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		sendAPIErrorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var chats []aws.Chat
	if err := json.NewDecoder(r.Body).Decode(&chats); err != nil {
		sendAPIErrorResponse(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	client := aws.GetDynamoDBClient(ctx)

	// Create chats individually since we don't have a batch create function
	var createdChats []*aws.Chat
	for _, chat := range chats {
		chat.CreatedAt = time.Now()
		chat.UpdatedAt = time.Now()

		createdChat, err := aws.CreateChat(ctx, client, chat)
		if err != nil {
			logger.GetDailyLogger().Error("Error creating chat: %v", err)
			sendAPIErrorResponse(w, "Failed to create chats", http.StatusInternalServerError)
			return
		}
		createdChats = append(createdChats, createdChat)
	}

	sendJSONResponse(w, createdChats, http.StatusCreated)
}

// ChatOperationsHandler handles GET/PUT/DELETE /v1/chats/{chatId}
func ChatOperationsHandler(w http.ResponseWriter, r *http.Request) {
	chatID := extractPathParam(r.URL.Path, fmt.Sprintf("/%s/chats/", APIVersion))
	if chatID == "" {
		sendAPIErrorResponse(w, "Chat ID is required", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	client := aws.GetDynamoDBClient(ctx)

	switch r.Method {
	case http.MethodGet:
		chat, err := aws.GetChat(ctx, client, chatID)
		if err != nil {
			logger.GetDailyLogger().Error("Error getting chat: %v", err)
			sendAPIErrorResponse(w, "Failed to get chat", http.StatusInternalServerError)
			return
		}
		sendJSONResponse(w, chat, http.StatusOK)

	case http.MethodPut:
		var chat aws.Chat
		if err := json.NewDecoder(r.Body).Decode(&chat); err != nil {
			sendAPIErrorResponse(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		chat.ID = chatID // Ensure the ID matches the URL
		chat.UpdatedAt = time.Now()

		updatedChat, err := aws.UpdateChat(ctx, client, chat)
		if err != nil {
			logger.GetDailyLogger().Error("Error updating chat: %v", err)
			sendAPIErrorResponse(w, "Failed to update chat", http.StatusInternalServerError)
			return
		}
		sendJSONResponse(w, updatedChat, http.StatusOK)

	case http.MethodDelete:
		err := aws.DeleteChat(ctx, client, chatID)
		if err != nil {
			logger.GetDailyLogger().Error("Error deleting chat: %v", err)
			sendAPIErrorResponse(w, "Failed to delete chat", http.StatusInternalServerError)
			return
		}
		sendJSONResponse(w, map[string]bool{"success": true}, http.StatusOK)

	default:
		sendAPIErrorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// CreateChatHandler handles POST /v1/chats
func CreateChatHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		sendAPIErrorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var chat aws.Chat
	if err := json.NewDecoder(r.Body).Decode(&chat); err != nil {
		sendAPIErrorResponse(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	client := aws.GetDynamoDBClient(ctx)

	chat.CreatedAt = time.Now()
	chat.UpdatedAt = time.Now()

	createdChat, err := aws.CreateChat(ctx, client, chat)
	if err != nil {
		logger.GetDailyLogger().Error("Error creating chat: %v", err)
		sendAPIErrorResponse(w, "Failed to create chat", http.StatusInternalServerError)
		return
	}

	sendJSONResponse(w, createdChat, http.StatusCreated)
}
