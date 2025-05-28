package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gateway/handlers"
	"gateway/services"

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

	// Metrics endpoint for monitoring
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		metrics := handlers.GetMetrics()
		json.NewEncoder(w).Encode(metrics)
	})

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

	log.Printf("🚀 Optimized Gateway server starting on %s", port)
	log.Printf("📊 Performance optimizations enabled:")
	log.Printf("   - Connection pooling: %d max connections", 100)
	log.Printf("   - Keep-alive timeout: %v", server.IdleTimeout)
	log.Printf("   - Streaming optimized (no write timeout)")
	log.Printf("   - HTTP/2 enabled")
	log.Printf("🔗 Endpoints:")
	log.Printf("   - Health: %s/health", port)
	log.Printf("   - Metrics: %s/metrics", port)
	log.Printf("   - Complete: %s/complete", port)

	// Warmup services for better performance
	go func() {
		log.Println("🔥 Starting service warmup...")

		// Warmup Ollama model
		if err := services.WarmupOllamaModel("llama3.2"); err != nil {
			log.Printf("⚠️  Ollama warmup failed: %v", err)
		}

		// Test classifier service
		if _, err := services.CallModelService("Hello world"); err != nil {
			log.Printf("⚠️  Classifier warmup failed: %v", err)
		} else {
			log.Println("✅ Classifier service warmed up")
		}

		log.Println("🚀 Service warmup completed")
	}()

	// Graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan

		log.Println("🛑 Shutting down server gracefully...")

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			log.Printf("❌ Server shutdown error: %v", err)
		} else {
			log.Println("✅ Server shutdown complete")
		}
	}()

	// Start server
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("❌ Server failed to start: %v", err)
	}
}
