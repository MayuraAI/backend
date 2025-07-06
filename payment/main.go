package main

import (
	"log"
	"os"

	"payment/dynamo"
	"payment/firebase"
	"payment/handlers"

	"github.com/gin-gonic/gin"
)

// getEnvWithDefault returns environment variable value or default if not set
func getEnvWithDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// setupCORS sets up CORS middleware
func setupCORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")

		// List of allowed origins
		allowedOrigins := []string{
			"https://mayura.rocks",
			"http://localhost:3000",
			"http://localhost:3001",
			"http://127.0.0.1:3000",
			"http://127.0.0.1:3001",
		}

		// Check if origin is allowed
		isAllowed := false
		for _, allowedOrigin := range allowedOrigins {
			if origin == allowedOrigin {
				isAllowed = true
				break
			}
		}

		if isAllowed {
			c.Header("Access-Control-Allow-Origin", origin)
		} else {
			c.Header("Access-Control-Allow-Origin", allowedOrigins[0])
		}

		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
		c.Header("Access-Control-Allow-Credentials", "true")
		c.Header("Access-Control-Max-Age", "86400")

		// Handle preflight requests
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

// setupRoutes sets up all the routes
func setupRoutes(r *gin.Engine) {
	// Add CORS middleware
	r.Use(setupCORS())

	// Health check endpoint (no auth required)
	r.GET("/health", handlers.HealthCheckHandler)

	// API routes
	api := r.Group("/api")
	{
		// Subscription management endpoints (require auth)
		api.POST("/checkout", handlers.CreateCheckoutHandler)
		api.GET("/tier", handlers.GetUserTierHandler)
		api.GET("/subscription", handlers.GetSubscriptionDetailsHandler)
		api.GET("/subscription/urls", handlers.GetSubscriptionURLsHandler)
		api.POST("/cancel-subscription", handlers.CancelSubscriptionHandler)

		// Webhook endpoint (no auth required, signature verified)
		api.POST("/webhook", handlers.WebhookHandler)
	}
}

func main() {
	// Set Gin mode based on environment
	ginMode := getEnvWithDefault("GIN_MODE", "release")
	gin.SetMode(ginMode)

	log.Println("Payment service starting up...")

	// Initialize Firebase
	log.Println("Initializing Firebase...")
	if err := firebase.InitFirebase(); err != nil {
		log.Fatalf("Failed to initialize Firebase: %v", err)
	}
	log.Println("Firebase initialized successfully")

	// Initialize DynamoDB
	log.Println("Initializing DynamoDB...")
	if err := dynamo.Init(); err != nil {
		log.Fatalf("Failed to initialize DynamoDB: %v", err)
	}
	log.Println("DynamoDB initialized successfully")

	// Create Gin router
	r := gin.Default()

	// Add request logging middleware
	r.Use(gin.LoggerWithConfig(gin.LoggerConfig{
		SkipPaths: []string{"/health"}, // Skip health check logs
	}))

	// Add recovery middleware
	r.Use(gin.Recovery())

	// Setup routes
	setupRoutes(r)

	// Get port from environment
	port := getEnvWithDefault("PORT", "8081")

	// Print startup information
	log.Printf("Payment service starting on port %s", port)
	log.Printf("Environment: %s", ginMode)
	log.Printf("DynamoDB table: %s", dynamo.TableName)

	// Print available endpoints
	log.Println("Available endpoints:")
	log.Println("  GET  /health - Health check")
	log.Println("  POST /api/checkout - Create checkout session")
	log.Println("  GET  /api/tier - Get user subscription tier")
	log.Println("  GET  /api/subscription - Get subscription details")
	log.Println("  GET  /api/subscription/urls - Get subscription management URLs")
	log.Println("  POST /api/cancel-subscription - Cancel subscription")
	log.Println("  POST /api/webhook - LemonSqueezy webhook handler")

	// Start server
	if err := r.Run(":" + port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
