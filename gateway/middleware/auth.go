package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"log"
	"os"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/joho/godotenv"
)


// get from .env file godotenv
func getSupabaseJWTSecret() string {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}
	supabaseJWTSecret := os.Getenv("SUPABASE_JWT_SECRET")
	return supabaseJWTSecret
}

var supabaseJWTSecret = getSupabaseJWTSecret()

type SupabaseClaims struct {
	Aud       string   `json:"aud"`
	Exp       int64    `json:"exp"`
	Sub       string   `json:"sub"`
	Email     string   `json:"email"`
	Phone     string   `json:"phone"`
	AppMeta   AppMeta  `json:"app_metadata"`
	UserMeta  UserMeta `json:"user_metadata"`
	Role      string   `json:"role"`
	SessionId string   `json:"session_id"`
}

type AppMeta struct {
	Provider string `json:"provider"`
}

type UserMeta struct {
	Email string `json:"email"`
}

type contextKey string

const UserContextKey contextKey = "user"

func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Get the Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "Authorization header required", http.StatusUnauthorized)
			return
		}

		// Check if the header has the Bearer prefix
		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			http.Error(w, "Invalid authorization header format", http.StatusUnauthorized)
			return
		}

		tokenString := parts[1]

		// Parse and validate the JWT token
		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			// Validate the signing method
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}

			// Get the JWT secret from environment variable
			// You should set this in your environment
			jwtSecret := []byte(supabaseJWTSecret) // Replace with your actual JWT secret
			return jwtSecret, nil
		})

		if err != nil {
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
			// Convert claims to SupabaseClaims
			claimsJSON, err := json.Marshal(claims)
			if err != nil {
				http.Error(w, "Error processing token claims", http.StatusInternalServerError)
				return
			}

			var supabaseClaims SupabaseClaims
			if err := json.Unmarshal(claimsJSON, &supabaseClaims); err != nil {
				http.Error(w, "Error processing token claims", http.StatusInternalServerError)
				return
			}

			// Add the claims to the request context
			ctx := context.WithValue(r.Context(), UserContextKey, supabaseClaims)
			next.ServeHTTP(w, r.WithContext(ctx))
		} else {
			http.Error(w, "Invalid token claims", http.StatusUnauthorized)
			return
		}
	})
}

// GetUserFromContext retrieves the user claims from the context
func GetUserFromContext(ctx context.Context) (SupabaseClaims, bool) {
	user, ok := ctx.Value(UserContextKey).(SupabaseClaims)
	return user, ok
}
