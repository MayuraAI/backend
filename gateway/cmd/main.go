package main

import (
	"log"
	"math/rand"
	"net/http"
	"os"
	"time"

	"gateway/handlers"
	// "gateway/middleware"

	"github.com/joho/godotenv"
)

func main() {
	rand.New(rand.NewSource(time.Now().UnixNano()))

	// Get JWT secret from environment variable
	err := godotenv.Load()
	if err != nil {
		log.Printf("Warning: Error loading .env file: %v", err)
	}
	// supabaseJWTSecret := os.Getenv("SUPABASE_JWT_SECRET")
	// if supabaseJWTSecret == "" {
	// 	log.Fatal("SUPABASE_JWT_SECRET environment variable is required")
	// }

	// Create a new mux router
	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("/health", handlers.HealthHandler)

	// Protected route with auth middleware - only allow POST requests
	mux.HandleFunc("/complete", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// middleware.AuthMiddleware(http.HandlerFunc(handlers.ClientHandler)).ServeHTTP(w, r)
		http.HandlerFunc(handlers.ClientHandler).ServeHTTP(w, r)
	})

	// Get port from environment or use default
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	port = ":" + port

	log.Printf("Gateway server running on %s", port)
	log.Printf("Health check available at %s/health", port)
	log.Printf("Complete endpoint available at %s/complete", port)

	if err := http.ListenAndServe(port, mux); err != nil {
		log.Fatal(err)
	}
}
