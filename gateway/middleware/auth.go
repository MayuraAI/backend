package middleware

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"gateway/aws"
	"gateway/pkg/logger"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
	"google.golang.org/api/option"
)

// min returns the smaller of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Firebase app instance (singleton)
var firebaseApp *firebase.App
var firebaseAuth *auth.Client

// initFirebase initializes Firebase app and auth client
func initFirebase() error {
	if firebaseApp != nil {
		return nil // Already initialized
	}

	ctx := context.Background()

	// Initialize Firebase app
	// Option 1: Using service account key file
	serviceAccountPath := os.Getenv("FIREBASE_SERVICE_ACCOUNT_PATH")
	if serviceAccountPath != "" {
		opt := option.WithCredentialsFile(serviceAccountPath)
		app, err := firebase.NewApp(ctx, nil, opt)
		if err != nil {
			return err
		}
		firebaseApp = app
	} else {
		// Option 2: Using service account JSON from environment variable
		serviceAccountJSON := os.Getenv("FIREBASE_SERVICE_ACCOUNT_JSON")
		if serviceAccountJSON != "" {
			opt := option.WithCredentialsJSON([]byte(serviceAccountJSON))
			app, err := firebase.NewApp(ctx, nil, opt)
			if err != nil {
				return err
			}
			firebaseApp = app
		} else {
			// Option 3: Using default credentials (for Google Cloud environments)
			app, err := firebase.NewApp(ctx, nil)
			if err != nil {
				return err
			}
			firebaseApp = app
		}
	}

	// Initialize Auth client
	authClient, err := firebaseApp.Auth(ctx)
	if err != nil {
		return err
	}
	firebaseAuth = authClient

	return nil
}

type firebaseContextKey string

const (
	FirebaseUserContextKey  firebaseContextKey = "firebase_user"
	FirebaseTokenContextKey firebaseContextKey = "firebase_token"
)

// FirebaseAuthMiddleware validates Firebase ID tokens using the Firebase Admin SDK
func FirebaseAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log := logger.GetLogger("firebase_auth")

		// Debug: Log all headers
		log.InfoWithFields("Request headers", map[string]interface{}{
			"method":  r.Method,
			"path":    r.URL.Path,
			"headers": r.Header,
		})

		// Initialize Firebase if not already done
		if err := initFirebase(); err != nil {
			log.Error("Failed to initialize Firebase: %v", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error": "Internal server error", "status": 500}`))
			return
		}

		// Get the Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			log.Warn("Missing Authorization header")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error": "Authorization header required", "status": 401}`))
			return
		}

		// Check if the header has the Bearer prefix
		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			log.Warn("Invalid authorization header format")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error": "Invalid authorization header format", "status": 401}`))
			return
		}

		idToken := parts[1]

		// Verify the ID token
		token, err := firebaseAuth.VerifyIDToken(context.Background(), idToken)
		if err != nil {
			log.WarnWithFields("Token validation failed", map[string]interface{}{
				"error":         err.Error(),
				"error_type":    fmt.Sprintf("%T", err),
				"token_preview": idToken[:min(50, len(idToken))] + "...",
			})
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error": "Invalid or expired token", "status": 401}`))
			return
		}

		// Get user record from Firebase
		userRecord, err := firebaseAuth.GetUser(context.Background(), token.UID)
		if err != nil {
			log.WarnWithFields("Failed to get user record", map[string]interface{}{
				"error": err.Error(),
				"uid":   token.UID,
			})
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error": "User not found", "status": 401}`))
			return
		}

		// Check if user is anonymous and log appropriately
		if userRecord.Email == "" {
			log.InfoWithFields("Anonymous user authenticated", map[string]interface{}{
				"uid": userRecord.UID,
			})
		} else {
			log.InfoWithFields("Authenticated user", map[string]interface{}{
				"uid":   userRecord.UID,
				"email": userRecord.Email,
			})
		}

		// Add the user and token to the request context
		ctx := context.WithValue(r.Context(), FirebaseUserContextKey, userRecord)
		ctx = context.WithValue(ctx, FirebaseTokenContextKey, idToken)

		// Call the next handler with the updated context
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetFirebaseUserFromContext retrieves the authenticated user from the context
func GetFirebaseUserFromContext(ctx context.Context) (*auth.UserRecord, bool) {
	user, ok := ctx.Value(FirebaseUserContextKey).(*auth.UserRecord)
	return user, ok
}

// GetFirebaseTokenFromContext retrieves the ID token from the context
func GetFirebaseTokenFromContext(ctx context.Context) (string, bool) {
	token, ok := ctx.Value(FirebaseTokenContextKey).(string)
	return token, ok
}

// IsAnonymousUser checks if the Firebase user is anonymous
func IsAnonymousUser(user *auth.UserRecord) bool {
	return user.Email == ""
}

// AuthorizeUserResource validates that the authenticated user can access the requested user resource
func AuthorizeUserResource(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log := logger.GetLogger("user_authorization")

		// Get authenticated user from context
		user, ok := GetFirebaseUserFromContext(r.Context())
		if !ok || user == nil {
			log.Warn("No authenticated user found in context")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error": "Authentication required", "status": 401}`))
			return
		}

		// Extract user ID from URL path
		path := r.URL.Path
		var requestedUserID string

		// Handle different URL patterns
		if strings.Contains(path, "/by-user-id/") {
			// Extract from patterns like /v1/profiles/by-user-id/{userId} or /v1/chats/by-user-id/{userId}
			parts := strings.Split(path, "/by-user-id/")
			if len(parts) >= 2 {
				userPart := strings.Split(parts[1], "/")[0]
				requestedUserID = userPart
			}
		} else if strings.Contains(path, "/user/") {
			// Extract from patterns like /v1/profiles/user/{userId}
			parts := strings.Split(path, "/user/")
			if len(parts) >= 2 {
				userPart := strings.Split(parts[1], "/")[0]
				requestedUserID = userPart
			}
		}

		// If we found a user ID in the path, validate it matches the authenticated user
		if requestedUserID != "" {
			if user.UID != requestedUserID {
				log.WarnWithFields("User authorization failed", map[string]interface{}{
					"authenticated_uid": user.UID,
					"requested_uid":     requestedUserID,
					"path":              path,
				})
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte(`{"error": "Access denied: You can only access your own resources", "status": 403}`))
				return
			}
		}

		// Continue to next handler
		next.ServeHTTP(w, r)
	})
}

// AuthorizeChatResource validates that the authenticated user owns the requested chat
func AuthorizeChatResource(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log := logger.GetLogger("chat_authorization")

		// Get authenticated user from context
		user, ok := GetFirebaseUserFromContext(r.Context())
		if !ok || user == nil {
			log.Warn("No authenticated user found in context")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error": "Authentication required", "status": 401}`))
			return
		}

		// Extract chat ID from URL path
		path := r.URL.Path
		var chatID string

		if strings.Contains(path, "/by-chat-id/") {
			// Extract from patterns like /v1/messages/by-chat-id/{chatId}
			parts := strings.Split(path, "/by-chat-id/")
			if len(parts) >= 2 {
				chatPart := strings.Split(parts[1], "/")[0]
				chatID = chatPart
			}
		} else if strings.Contains(path, "/chats/") && !strings.Contains(path, "/by-user-id/") {
			// Extract from patterns like /v1/chats/{chatId}
			parts := strings.Split(path, "/chats/")
			if len(parts) >= 2 {
				chatPart := strings.Split(parts[1], "/")[0]
				if chatPart != "" && chatPart != "batch-operations" {
					chatID = chatPart
				}
			}
		}

		// If we found a chat ID, validate the user owns this chat
		if chatID != "" {
			ctx := context.Background()
			client := aws.GetDynamoDBClient(ctx)

			chat, err := aws.GetChat(ctx, client, chatID)
			if err != nil {
				log.WarnWithFields("Chat not found", map[string]interface{}{
					"chat_id": chatID,
					"error":   err.Error(),
				})
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"error": "Chat not found", "status": 404}`))
				return
			}

			if chat.UserID != user.UID {
				log.WarnWithFields("Chat authorization failed", map[string]interface{}{
					"authenticated_uid": user.UID,
					"chat_owner_uid":    chat.UserID,
					"chat_id":           chatID,
				})
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte(`{"error": "Access denied: You can only access your own chats", "status": 403}`))
				return
			}
		}

		// Continue to next handler
		next.ServeHTTP(w, r)
	})
}

// RequireUserResource validates that the request is for the authenticated user's own resources
// This middleware should be used for endpoints that operate on the current user's data
func RequireUserResource(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log := logger.GetLogger("user_authorization")

		// Get authenticated user from context
		user, ok := GetFirebaseUserFromContext(r.Context())
		if !ok || user == nil {
			log.Warn("No authenticated user found in context")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error": "Authentication required", "status": 401}`))
			return
		}

		// Add user ID to context for handlers to use
		ctx := context.WithValue(r.Context(), "authenticated_user_id", user.UID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetAuthenticatedUserID retrieves the authenticated user ID from context
func GetAuthenticatedUserID(ctx context.Context) (string, bool) {
	userID, ok := ctx.Value("authenticated_user_id").(string)
	return userID, ok
}

// RequireChatOwnership validates that the authenticated user owns the specified chat
// This should be used for endpoints that operate on specific chats
func RequireChatOwnership(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log := logger.GetLogger("chat_authorization")

		// Get authenticated user from context
		user, ok := GetFirebaseUserFromContext(r.Context())
		if !ok || user == nil {
			log.Warn("No authenticated user found in context")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error": "Authentication required", "status": 401}`))
			return
		}

		// Extract chat ID from URL path
		path := r.URL.Path
		var chatID string

		if strings.Contains(path, "/by-chat-id/") {
			// Extract from patterns like /v1/messages/by-chat-id/{chatId}
			parts := strings.Split(path, "/by-chat-id/")
			if len(parts) >= 2 {
				chatPart := strings.Split(parts[1], "/")[0]
				chatID = chatPart
			}
		} else if strings.Contains(path, "/chats/") && !strings.Contains(path, "/by-user-id/") {
			// Extract from patterns like /v1/chats/{chatId}
			parts := strings.Split(path, "/chats/")
			if len(parts) >= 2 {
				chatPart := strings.Split(parts[1], "/")[0]
				if chatPart != "" && chatPart != "batch-operations" {
					chatID = chatPart
				}
			}
		}

		// If we found a chat ID, validate the user owns this chat
		if chatID != "" {
			ctx := context.Background()
			client := aws.GetDynamoDBClient(ctx)

			chat, err := aws.GetChat(ctx, client, chatID)
			if err != nil {
				log.WarnWithFields("Chat not found", map[string]interface{}{
					"chat_id": chatID,
					"error":   err.Error(),
				})
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"error": "Chat not found", "status": 404}`))
				return
			}

			if chat.UserID != user.UID {
				log.WarnWithFields("Chat authorization failed", map[string]interface{}{
					"authenticated_uid": user.UID,
					"chat_owner_uid":    chat.UserID,
					"chat_id":           chatID,
				})
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"error": "Chat not found", "status": 404}`))
				return
			}
		}

		// Continue to next handler
		next.ServeHTTP(w, r)
	})
}
