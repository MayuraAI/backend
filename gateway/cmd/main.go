package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gateway/handlers"
	"gateway/middleware"
	"gateway/pkg/logger"

	"github.com/joho/godotenv"
)

func main() {
	rand.New(rand.NewSource(time.Now().UnixNano()))

	// Initialize logging
	logger.InitFromEnv()
	log := logger.GetLogger("main")

	// Get JWT secret from environment variable
	err := godotenv.Load()
	if err != nil {
		log.Warn("Error loading .env file")
	}
	// supabaseJWTSecret := os.Getenv("SUPABASE_JWT_SECRET")
	// if supabaseJWTSecret == "" {
	// 	log.Fatal("SUPABASE_JWT_SECRET environment variable is required")
	// }

	// Create a new mux router
	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("/health", handlers.HealthHandler)

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
		middleware.RateLimitMiddleware()(
			middleware.SupabaseAuthMiddleware(
				http.HandlerFunc(handlers.ClientHandler))).ServeHTTP(w, r)
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

	// Get port from environment or use default
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	port = ":" + port

	// Create optimized HTTP server with performance configurations
	server := &http.Server{
		Addr:    port,
		Handler: mux,

		// Optimized timeouts for streaming
		ReadTimeout:       30 * time.Second,  // Time to read request
		WriteTimeout:      0,                 // Disabled for streaming (SSE)
		IdleTimeout:       120 * time.Second, // Keep-alive timeout
		ReadHeaderTimeout: 10 * time.Second,  // Time to read headers

		// Connection limits and optimizations
		MaxHeaderBytes: 1 << 20, // 1MB max header size

		// Enable HTTP/2 and connection reuse
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
	}

	// Configure transport for outbound requests (to classifier/ollama)
	http.DefaultTransport = &http.Transport{
		MaxIdleConns:        100,              // Max idle connections
		MaxIdleConnsPerHost: 20,               // Max idle per host
		MaxConnsPerHost:     50,               // Max total per host
		IdleConnTimeout:     90 * time.Second, // Idle timeout
		TLSHandshakeTimeout: 10 * time.Second, // TLS timeout

		// Connection timeouts
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,

		// Keep-alive settings
		DisableKeepAlives:  false,
		DisableCompression: false,
		ForceAttemptHTTP2:  true,
	}

	log.InfoWithFields("Gateway server starting", map[string]interface{}{
		"port":          port,
		"optimizations": "enabled",
	})
	log.InfoWithFields("Performance optimizations enabled", map[string]interface{}{
		"max_connections":    100,
		"keep_alive_timeout": server.IdleTimeout.String(),
		"streaming":          "optimized",
		"http2":              "enabled",
	})
	log.InfoWithFields("Endpoints configured", map[string]interface{}{
		"health":            port + "/health",
		"metrics":           port + "/metrics",
		"complete":          port + "/complete",
		"rate-limit-status": port + "/rate-limit-status",
	})

	// Graceful shutdown
	go func() {
		shutdownLogger := logger.GetLogger("shutdown")
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan

		shutdownLogger.Info("Shutting down server gracefully")

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			shutdownLogger.Error("Server shutdown error", err)
		} else {
			shutdownLogger.Info("Server shutdown complete")
		}
	}()

	// Start server
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.ErrorWithFields("Server failed to start", map[string]interface{}{
			"error": err.Error(),
		}, err)
		os.Exit(1)
	}
}
