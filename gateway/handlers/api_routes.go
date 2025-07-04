package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"gateway/middleware"
)

// API version constant - can be changed for future versions
const APIVersion = "v1"

// SetupAPIRoutes sets up all the API routes using separate route modules with Firebase authentication
func SetupAPIRoutes(mux *http.ServeMux) {
	// Setup routes from separate modules with Firebase authentication middleware
	SetupProfileRoutesWithAuth(mux, APIVersion)
	SetupChatRoutesWithAuth(mux, APIVersion)
	SetupMessageRoutesWithAuth(mux, APIVersion)
}

// SetupProfileRoutesWithAuth sets up profile routes with Firebase authentication
func SetupProfileRoutesWithAuth(mux *http.ServeMux, apiVersion string) {
	// Profile routes with CORS and Firebase authentication
	mux.HandleFunc("/v1/profiles/user/", func(w http.ResponseWriter, r *http.Request) {
		middleware.CORSMiddleware(
			middleware.FirebaseAuthMiddleware(http.HandlerFunc(handleProfileByUserID)),
		).ServeHTTP(w, r)
	})
	mux.HandleFunc("/v1/profiles/users/", func(w http.ResponseWriter, r *http.Request) {
		middleware.CORSMiddleware(
			middleware.FirebaseAuthMiddleware(http.HandlerFunc(handleProfilesByUserID)),
		).ServeHTTP(w, r)
	})
	// Combined username check handler for both GET and POST
	mux.HandleFunc("/v1/profiles/username/check", func(w http.ResponseWriter, r *http.Request) {
		middleware.CORSMiddleware(
			middleware.FirebaseAuthMiddleware(http.HandlerFunc(handleUsernameCheckCombined)),
		).ServeHTTP(w, r)
	})
	mux.HandleFunc("/v1/profiles/username/", func(w http.ResponseWriter, r *http.Request) {
		middleware.CORSMiddleware(
			middleware.FirebaseAuthMiddleware(http.HandlerFunc(handleGetUsernameByUserID)),
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
	mux.HandleFunc("/v1/chats/user/", func(w http.ResponseWriter, r *http.Request) {
		middleware.CORSMiddleware(
			middleware.FirebaseAuthMiddleware(http.HandlerFunc(ChatsByUserIDHandler)),
		).ServeHTTP(w, r)
	})
	mux.HandleFunc("/v1/chats/batch", func(w http.ResponseWriter, r *http.Request) {
		middleware.CORSMiddleware(
			middleware.FirebaseAuthMiddleware(http.HandlerFunc(BatchChatsHandler)),
		).ServeHTTP(w, r)
	})
	// Combined chat handler for both collection and individual operations
	mux.HandleFunc("/v1/chats/", func(w http.ResponseWriter, r *http.Request) {
		middleware.CORSMiddleware(
			middleware.FirebaseAuthMiddleware(http.HandlerFunc(handleChatCombined)),
		).ServeHTTP(w, r)
	})
}

// SetupMessageRoutesWithAuth sets up message routes with Firebase authentication
func SetupMessageRoutesWithAuth(mux *http.ServeMux, apiVersion string) {
	// Message routes with CORS and Firebase authentication
	mux.HandleFunc("/v1/messages/chat/", func(w http.ResponseWriter, r *http.Request) {
		middleware.CORSMiddleware(
			middleware.FirebaseAuthMiddleware(http.HandlerFunc(MessageOperationsHandler)),
		).ServeHTTP(w, r)
	})
	mux.HandleFunc("/v1/messages/batch", func(w http.ResponseWriter, r *http.Request) {
		middleware.CORSMiddleware(
			middleware.FirebaseAuthMiddleware(http.HandlerFunc(BatchMessagesHandler)),
		).ServeHTTP(w, r)
	})
	mux.HandleFunc("/v1/messages/duplicate", func(w http.ResponseWriter, r *http.Request) {
		middleware.CORSMiddleware(
			middleware.FirebaseAuthMiddleware(http.HandlerFunc(DuplicateMessagesHandler)),
		).ServeHTTP(w, r)
	})
	mux.HandleFunc("/v1/messages/delete-from-sequence", func(w http.ResponseWriter, r *http.Request) {
		middleware.CORSMiddleware(
			middleware.FirebaseAuthMiddleware(http.HandlerFunc(DeleteFromSequenceHandler)),
		).ServeHTTP(w, r)
	})
	// Combined message handler for both collection and individual operations
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
