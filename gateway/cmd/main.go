package main

import (
	"log"
	"math/rand"
	"net/http"
	// "os"
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
		log.Fatal("Error loading .env file")
	}
	// supabaseJWTSecret := os.Getenv("SUPABASE_JWT_SECRET")
	// if supabaseJWTSecret == "" {
	// 	log.Fatal("SUPABASE_JWT_SECRET environment variable is required")
	// }

	// Create a new mux router
	mux := http.NewServeMux()

	// Protected route with auth middleware - only allow POST requests
	mux.HandleFunc("/complete", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// middleware.AuthMiddleware(http.HandlerFunc(handlers.ClientHandler)).ServeHTTP(w, r)
		http.HandlerFunc(handlers.ClientHandler).ServeHTTP(w, r)
	})

	port := ":8080"
	log.Printf("SSE server (unique per client) running on %s", port)
	if err := http.ListenAndServe(port, mux); err != nil {
		log.Fatal(err)
	}
}
