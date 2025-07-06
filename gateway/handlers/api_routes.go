package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"gateway/middleware"
	"gateway/pkg/redis"
)

// API version constant - can be changed for future versions
const APIVersion = "v1"

// SetupAPIRoutes sets up all the API routes using separate route modules with Firebase authentication
func SetupAPIRoutes(mux *http.ServeMux) {
	// Setup routes from separate modules with Firebase authentication middleware
	SetupProfileRoutesWithAuth(mux, APIVersion)
	SetupChatRoutesWithAuth(mux, APIVersion)
	SetupMessageRoutesWithAuth(mux, APIVersion)

	// Setup subscription routes
	SetupSubscriptionRoutesWithAuth(mux, APIVersion)
}

// SetupSubscriptionRoutesWithAuth sets up subscription routes with Firebase authentication
func SetupSubscriptionRoutesWithAuth(mux *http.ServeMux, apiVersion string) {
	// Get payment service URL from environment or use default
	paymentServiceURL := os.Getenv("PAYMENT_SERVICE_URL")
	if paymentServiceURL == "" {
		paymentServiceURL = "http://localhost:8081" // Default payment service URL
	}

	// Create subscription handler
	subscriptionHandler := NewSubscriptionHandler(redis.GetClient(), paymentServiceURL)

	// Get user subscription information
	mux.HandleFunc("/v1/profile/subscription", func(w http.ResponseWriter, r *http.Request) {
		middleware.CORSMiddleware(
			middleware.FirebaseAuthMiddleware(
				middleware.RequireUserResource(http.HandlerFunc(subscriptionHandler.GetUserSubscription)),
			),
		).ServeHTTP(w, r)
	})

	// Create checkout session
	mux.HandleFunc("/v1/subscription/checkout", func(w http.ResponseWriter, r *http.Request) {
		middleware.CORSMiddleware(
			middleware.FirebaseAuthMiddleware(
				middleware.RequireUserResource(http.HandlerFunc(subscriptionHandler.CreateCheckoutSession)),
			),
		).ServeHTTP(w, r)
	})

	// Get subscription management URL
	mux.HandleFunc("/v1/subscription/management", func(w http.ResponseWriter, r *http.Request) {
		middleware.CORSMiddleware(
			middleware.FirebaseAuthMiddleware(
				middleware.RequireUserResource(http.HandlerFunc(subscriptionHandler.GetManagementURL)),
			),
		).ServeHTTP(w, r)
	})
}

// SetupProfileRoutesWithAuth sets up profile routes with Firebase authentication
func SetupProfileRoutesWithAuth(mux *http.ServeMux, apiVersion string) {
	// Profile routes with CORS, Firebase authentication, and user authorization
	mux.HandleFunc("/v1/profiles/current", func(w http.ResponseWriter, r *http.Request) {
		middleware.CORSMiddleware(
			middleware.FirebaseAuthMiddleware(
				middleware.RequireUserResource(http.HandlerFunc(handleCurrentUserProfile)),
			),
		).ServeHTTP(w, r)
	})

	// Batch endpoint for current user
	mux.HandleFunc("/v1/profiles/current/all", func(w http.ResponseWriter, r *http.Request) {
		middleware.CORSMiddleware(
			middleware.FirebaseAuthMiddleware(
				middleware.RequireUserResource(http.HandlerFunc(handleCurrentUserProfileAll)),
			),
		).ServeHTTP(w, r)
	})

	// Username availability check endpoint (no user authorization needed)
	mux.HandleFunc("/v1/profiles/username-availability-check", func(w http.ResponseWriter, r *http.Request) {
		middleware.CORSMiddleware(
			middleware.FirebaseAuthMiddleware(http.HandlerFunc(handleUsernameCheckCombined)),
		).ServeHTTP(w, r)
	})

	// Get current user's username
	mux.HandleFunc("/v1/profiles/current/username", func(w http.ResponseWriter, r *http.Request) {
		middleware.CORSMiddleware(
			middleware.FirebaseAuthMiddleware(
				middleware.RequireUserResource(http.HandlerFunc(handleGetCurrentUserUsername)),
			),
		).ServeHTTP(w, r)
	})

	// Combined profile handler for both collection and individual operations
	mux.HandleFunc("/v1/profiles/", func(w http.ResponseWriter, r *http.Request) {
		middleware.CORSMiddleware(
			middleware.FirebaseAuthMiddleware(http.HandlerFunc(handleProfileCombined)),
		).ServeHTTP(w, r)
	})
}

// SetupChatRoutesWithAuth sets up chat routes with Firebase authentication
func SetupChatRoutesWithAuth(mux *http.ServeMux, apiVersion string) {
	// Chat routes with CORS and Firebase authentication
	mux.HandleFunc("/v1/chats/current", func(w http.ResponseWriter, r *http.Request) {
		middleware.CORSMiddleware(
			middleware.FirebaseAuthMiddleware(
				middleware.RequireUserResource(http.HandlerFunc(handleCurrentUserChats)),
			),
		).ServeHTTP(w, r)
	})

	mux.HandleFunc("/v1/chats/batch-operations", func(w http.ResponseWriter, r *http.Request) {
		middleware.CORSMiddleware(
			middleware.FirebaseAuthMiddleware(http.HandlerFunc(BatchChatsHandler)),
		).ServeHTTP(w, r)
	})

	// Combined chat handler - authorization is handled in the handler itself
	mux.HandleFunc("/v1/chats/", func(w http.ResponseWriter, r *http.Request) {
		middleware.CORSMiddleware(
			middleware.FirebaseAuthMiddleware(http.HandlerFunc(handleChatCombined)),
		).ServeHTTP(w, r)
	})
}

// SetupMessageRoutesWithAuth sets up message routes with Firebase authentication
func SetupMessageRoutesWithAuth(mux *http.ServeMux, apiVersion string) {
	// Message routes with CORS and Firebase authentication - authorization handled in handlers
	mux.HandleFunc("/v1/messages/by-chat-id/", func(w http.ResponseWriter, r *http.Request) {
		middleware.CORSMiddleware(
			middleware.FirebaseAuthMiddleware(
				middleware.RequireChatOwnership(http.HandlerFunc(MessageOperationsHandler)),
			),
		).ServeHTTP(w, r)
	})

	mux.HandleFunc("/v1/messages/batch-operations", func(w http.ResponseWriter, r *http.Request) {
		middleware.CORSMiddleware(
			middleware.FirebaseAuthMiddleware(http.HandlerFunc(BatchMessagesHandler)),
		).ServeHTTP(w, r)
	})

	mux.HandleFunc("/v1/messages/duplicate-check", func(w http.ResponseWriter, r *http.Request) {
		middleware.CORSMiddleware(
			middleware.FirebaseAuthMiddleware(http.HandlerFunc(DuplicateMessagesHandler)),
		).ServeHTTP(w, r)
	})

	mux.HandleFunc("/v1/messages/delete-from-sequence", func(w http.ResponseWriter, r *http.Request) {
		middleware.CORSMiddleware(
			middleware.FirebaseAuthMiddleware(http.HandlerFunc(DeleteFromSequenceHandler)),
		).ServeHTTP(w, r)
	})

	// Combined message handler - authorization handled in handler itself
	mux.HandleFunc("/v1/messages/", func(w http.ResponseWriter, r *http.Request) {
		middleware.CORSMiddleware(
			middleware.FirebaseAuthMiddleware(http.HandlerFunc(handleMessageCombined)),
		).ServeHTTP(w, r)
	})
}

// Helper function to extract path parameter
func extractPathParam(path, prefix string) string {
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	param := strings.TrimPrefix(path, prefix)
	return strings.TrimSuffix(param, "/")
}

// Helper function to send JSON response
func sendJSONResponse(w http.ResponseWriter, data interface{}, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}

// Helper function to send error response
func sendAPIErrorResponse(w http.ResponseWriter, message string, statusCode int) {
	sendJSONResponse(w, map[string]string{"error": message}, statusCode)
}
