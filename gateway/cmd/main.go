package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"gateway/handlers"
	"gateway/middleware"
)

// getEnvWithDefault gets environment variable with default value
func getEnvWithDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvIntWithDefault gets environment variable as int with default value
func getEnvIntWithDefault(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// loggingMiddleware logs all incoming requests
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		log.Printf("→ %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)

		next.ServeHTTP(w, r)

		log.Printf("← %s %s completed in %v", r.Method, r.URL.Path, time.Since(start))
	})
}

// setupRoutes configures all the HTTP routes
func setupRoutes() http.Handler {
	mux := http.NewServeMux()

	// Metrics endpoint for monitoring
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		metrics := handlers.GetMetrics()

		// Add rate limiting stats
		rateLimitStats := middleware.GetRateLimitStats()
		metrics["rate_limiting"] = rateLimitStats

		json.NewEncoder(w).Encode(metrics)
	})

	// Protected route with rate limiting and Supabase auth middleware - only allow POST requests
	mux.HandleFunc("/complete", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// Apply rate limiting first, then authentication middleware
		middleware.SupabaseAuthMiddleware(
			middleware.RateLimitMiddleware(
				http.HandlerFunc(handlers.ClientHandler),
				middleware.GetDefaultConfig(),
			),
		).ServeHTTP(w, r)
	})

	// Rate limit status endpoint - requires authentication
	mux.HandleFunc("/rate-limit-status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodOptions {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// Apply authentication middleware (rate limiting not needed for status check)
		middleware.SupabaseAuthMiddleware(
			http.HandlerFunc(handlers.RateLimitStatusHandler)).ServeHTTP(w, r)
	})

	// Wrap with logging middleware to log ALL requests
	return loggingMiddleware(mux)
}

func main() {
	// Set maximum number of CPUs to use
	maxProcs := runtime.GOMAXPROCS(0)
	log.Printf("Gateway server initializing with %d CPU cores", maxProcs)

	// Get port from environment
	port := getEnvWithDefault("PORT", "8080")

	log.Printf("Starting gateway server on port %s", port)

	// Create HTTP server with optimizations
	server := &http.Server{
		Addr:    ":" + port,
		Handler: setupRoutes(),

		// Timeouts for better resource management
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      0, // Disabled for streaming
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,

		// Buffer sizes
		MaxHeaderBytes: 1 << 20, // 1MB
	}

	// Channel to listen for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// Start server in a goroutine
	go func() {
		log.Printf("Server listening on :%s", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Server failed to start: %v", err)
		}
	}()

	// Wait for interrupt signal
	<-quit
	log.Println("Server shutting down...")

	// Create a timeout context for shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// Attempt graceful shutdown
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	} else {
		log.Println("Server shutdown complete")
	}
}
