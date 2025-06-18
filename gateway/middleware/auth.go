package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"gateway/pkg/logger"

	"github.com/supabase-community/auth-go"
	"github.com/supabase-community/auth-go/types"
)

const (
	supabaseProjectRef = "joqxsmypurgigczeyktl"
	supabaseAnonKey    = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJzdXBhYmFzZSIsInJlZiI6ImpvcXhzbXlwdXJnaWdjemV5a3RsIiwicm9sZSI6ImFub24iLCJpYXQiOjE3NDg0MjE0NzIsImV4cCI6MjA2Mzk5NzQ3Mn0.kNIbZj7a4RVTgrvvm69-YuyrTalVxrZa32pidyMogxg"
)

type supabaseContextKey string

const (
	SupabaseUserContextKey  supabaseContextKey = "supabase_user"
	SupabaseTokenContextKey supabaseContextKey = "supabase_token"
)

// SupabaseAuthMiddleware validates Supabase access tokens using the auth-go client
func SupabaseAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log := logger.GetLogger("supabase_auth")

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

		tokenStr := parts[1]

		// Create auth client and verify token
		client := auth.New(supabaseProjectRef, supabaseAnonKey).WithToken(tokenStr)

		user, err := client.GetUser()
		if err != nil {
			log.WarnWithFields("Token validation failed", map[string]interface{}{
				"error": err.Error(),
			})
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error": "Invalid or expired token", "status": 401}`))
			return
		}

		// Print user details as requested
		log.InfoWithFields("User authenticated successfully", map[string]interface{}{
			"user_id":            user.ID.String(),
			"email":              user.Email,
			"phone":              user.Phone,
			"created_at":         user.CreatedAt,
			"updated_at":         user.UpdatedAt,
			"role":               user.Role,
			"email_confirmed_at": user.EmailConfirmedAt,
			"phone_confirmed_at": user.PhoneConfirmedAt,
			"last_sign_in_at":    user.LastSignInAt,
		})

		// Log additional user details for debugging
		fmt.Printf("=== Authenticated User Details ===\n")
		fmt.Printf("User ID: %s\n", user.ID.String())
		fmt.Printf("Email: %s\n", user.Email)
		fmt.Printf("Phone: %s\n", user.Phone)
		fmt.Printf("Created At: %s\n", user.CreatedAt)
		fmt.Printf("Updated At: %s\n", user.UpdatedAt)
		fmt.Printf("Role: %s\n", user.Role)
		if user.EmailConfirmedAt != nil {
			fmt.Printf("Email Confirmed At: %s\n", *user.EmailConfirmedAt)
		}
		if user.PhoneConfirmedAt != nil {
			fmt.Printf("Phone Confirmed At: %s\n", *user.PhoneConfirmedAt)
		}
		if user.LastSignInAt != nil {
			fmt.Printf("Last Sign In At: %s\n", *user.LastSignInAt)
		}
		fmt.Printf("==================================\n")

		// Add the user and token to the request context
		ctx := context.WithValue(r.Context(), SupabaseUserContextKey, user)
		ctx = context.WithValue(ctx, SupabaseTokenContextKey, tokenStr)

		// Call the next handler with the updated context
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetSupabaseUserFromContext retrieves the authenticated user from the context
func GetSupabaseUserFromContext(ctx context.Context) (*types.UserResponse, bool) {
	user, ok := ctx.Value(SupabaseUserContextKey).(*types.UserResponse)
	return user, ok
}

// GetSupabaseTokenFromContext retrieves the access token from the context
func GetSupabaseTokenFromContext(ctx context.Context) (string, bool) {
	token, ok := ctx.Value(SupabaseTokenContextKey).(string)
	return token, ok
}
