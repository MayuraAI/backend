package middleware

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

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
