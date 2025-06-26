package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"gateway/handlers"
	"gateway/middleware"
	"gateway/pkg/logger"

	"github.com/joho/godotenv"
)

// getEnvWithDefault gets environment variable with default value
func getEnvWithDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// setupRoutes configures all the HTTP routes
func setupRoutes() http.Handler {
	mux := http.NewServeMux()

	// Metrics endpoint for monitoring
	// mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
	// 	if r.Method != http.MethodGet {
	// 		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	// 		return
	// 	}

	// 	w.Header().Set("Content-Type", "application/json")
	// 	metrics := handlers.GetMetrics()

	// 	// Add rate limiting stats
	// 	rateLimitStats := middleware.GetRateLimitStats()
	// 	metrics["rate_limiting"] = rateLimitStats

	// 	json.NewEncoder(w).Encode(metrics)
	// })

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
	return mux
}

func main() {
	// Load environment variables from .env file
	if err := godotenv.Load(); err != nil {
		logger.GetDailyLogger().Warn("Warning: Error loading .env file: %v", err)
		logger.GetDailyLogger().Warn("Continuing with system environment variables...")
	} else {
		logger.GetDailyLogger().Info("Successfully loaded .env file")
	}

	// Set maximum number of CPUs to use
	maxProcs := runtime.GOMAXPROCS(0)
	logger.GetDailyLogger().Info("Gateway server initializing with %d CPU cores", maxProcs)

	// Get port from environment
	port := getEnvWithDefault("PORT", "8080")

	logger.GetDailyLogger().Info("Starting gateway server on port %s", port)

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
		logger.GetDailyLogger().Info("Server listening on :%s", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.GetDailyLogger().Error("Server failed to start: %v", err)
		}
	}()

	// Wait for interrupt signal
	<-quit
	logger.GetDailyLogger().Info("Server shutting down...")

	// Create a timeout context for shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// Attempt graceful shutdown
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.GetDailyLogger().Error("Server forced to shutdown: %v", err)
	} else {
		logger.GetDailyLogger().Info("Server shutdown complete")
	}
}
