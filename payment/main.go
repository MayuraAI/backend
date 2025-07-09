package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"payment/dynamo"
	"payment/firebase"
	"payment/handlers"

	"github.com/gin-gonic/gin"
)

// getEnvWithDefault returns environment variable value or default if not set
func getEnvWithDefault(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		log.Printf("📝 Environment variable %s not set, using default: %s", key, defaultValue)
		return defaultValue
	}
	log.Printf("📝 Environment variable %s = %s", key, value)
	return value
}

// logEnvironmentConfig logs all relevant environment variables (safely)
func logEnvironmentConfig() {
	log.Println("🔧 Payment Service Configuration:")
	log.Printf("  PORT: %s", getEnvWithDefault("PORT", "8081"))
	log.Printf("  GIN_MODE: %s", getEnvWithDefault("GIN_MODE", "release"))
	log.Printf("  DEVELOPMENT: %s", getEnvWithDefault("DEVELOPMENT", "false"))
	log.Printf("  DYNAMO_TABLE: %s", getEnvWithDefault("DYNAMO_TABLE", "subscriptions"))
	log.Printf("  AWS_REGION: %s", getEnvWithDefault("AWS_REGION", "us-east-1"))

	// Log presence of sensitive variables without exposing values
	if os.Getenv("LSZ_API_KEY") != "" {
		log.Println("  LSZ_API_KEY: ✅ Set")
	} else {
		log.Println("  LSZ_API_KEY: ❌ Not set")
	}

	if os.Getenv("LSZ_WEBHOOK_SECRET") != "" {
		log.Println("  LSZ_WEBHOOK_SECRET: ✅ Set")
	} else {
		log.Println("  LSZ_WEBHOOK_SECRET: ❌ Not set")
	}

	if os.Getenv("AWS_ACCESS_KEY_ID") != "" {
		log.Println("  AWS_ACCESS_KEY_ID: ✅ Set")
	} else {
		log.Println("  AWS_ACCESS_KEY_ID: ❌ Not set")
	}

	if os.Getenv("FIREBASE_SERVICE_ACCOUNT_PATH") != "" {
		log.Printf("  FIREBASE_SERVICE_ACCOUNT_PATH: %s", os.Getenv("FIREBASE_SERVICE_ACCOUNT_PATH"))
	} else {
		log.Println("  FIREBASE_SERVICE_ACCOUNT_PATH: ❌ Not set")
	}

	if os.Getenv("FIREBASE_SERVICE_ACCOUNT_JSON") != "" {
		log.Println("  FIREBASE_SERVICE_ACCOUNT_JSON: ✅ Set")
	} else {
		log.Println("  FIREBASE_SERVICE_ACCOUNT_JSON: ❌ Not set")
	}
}

// setupCORS sets up CORS middleware
func setupCORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		startTime := time.Now()
		origin := c.GetHeader("Origin")
		method := c.Request.Method

		log.Printf("🌐 CORS Request: %s %s from origin: %s", method, c.Request.URL.Path, origin)

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
			log.Printf("✅ Origin allowed: %s", origin)
			c.Header("Access-Control-Allow-Origin", origin)
		} else {
			log.Printf("⚠️ Origin not in allowed list, using default: %s", origin)
			c.Header("Access-Control-Allow-Origin", allowedOrigins[0])
		}

		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
		c.Header("Access-Control-Allow-Credentials", "true")
		c.Header("Access-Control-Max-Age", "86400")

		// Handle preflight requests
		if c.Request.Method == "OPTIONS" {
			log.Printf("✈️ CORS Preflight request handled for %s", c.Request.URL.Path)
			c.AbortWithStatus(204)
			return
		}

		c.Next()

		duration := time.Since(startTime)
		log.Printf("🌐 CORS Response: %s %s completed in %v", method, c.Request.URL.Path, duration)
	}
}

// setupRoutes sets up all the routes
func setupRoutes(r *gin.Engine) {
	log.Println("🛣️ Setting up API routes...")

	// Add CORS middleware
	r.Use(setupCORS())

	// Health check endpoint (no auth required)
	r.GET("/health", handlers.HealthCheckHandler)
	log.Println("  ✅ GET /health - Health check endpoint")

	// API routes
	api := r.Group("/api")
	{
		// Subscription management endpoints (require auth)
		api.POST("/checkout", handlers.CreateCheckoutHandler)
		log.Println("  ✅ POST /api/checkout - Create checkout session")

		api.GET("/tier", handlers.GetUserTierHandler)
		log.Println("  ✅ GET /api/tier - Get user subscription tier")

		api.GET("/subscription", handlers.GetSubscriptionDetailsHandler)
		log.Println("  ✅ GET /api/subscription - Get subscription details")

		api.GET("/subscription/status/:user_id", handlers.GetSubscriptionStatusHandler)
		log.Println("  ✅ GET /api/subscription/status/:user_id - Get subscription status for user")

		api.GET("/subscription/management/:user_id", handlers.GetUserManagementURLHandler)
		log.Println("  ✅ GET /api/subscription/management/:user_id - Get subscription management URL")

		api.GET("/subscription/urls", handlers.GetSubscriptionURLsHandler)
		log.Println("  ✅ GET /api/subscription/urls - Get subscription management URLs")

		api.POST("/cancel-subscription", handlers.CancelSubscriptionHandler)
		log.Println("  ✅ POST /api/cancel-subscription - Cancel subscription")

		// Webhook endpoint (no auth required, signature verified)
		api.POST("/webhook", handlers.WebhookHandler)
		log.Println("  ✅ POST /api/webhook - LemonSqueezy webhook handler")
	}

	log.Println("🛣️ All routes configured successfully")
}

func main() {
	// Set up logging format
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	// Set Gin mode based on environment
	ginMode := getEnvWithDefault("GIN_MODE", "release")
	gin.SetMode(ginMode)

	log.Println("🚀 Payment service starting up...")
	log.Printf("⏰ Start time: %s", time.Now().Format(time.RFC3339))

	// Log environment configuration
	logEnvironmentConfig()

	// Initialize Firebase
	log.Println("🔥 Initializing Firebase...")
	if err := firebase.InitFirebase(); err != nil {
		log.Fatalf("❌ Failed to initialize Firebase: %v", err)
	}
	log.Println("✅ Firebase initialized successfully")

	// Initialize DynamoDB
	log.Println("🗄️ Initializing DynamoDB...")
	if err := dynamo.Init(); err != nil {
		log.Fatalf("❌ Failed to initialize DynamoDB: %v", err)
	}
	log.Println("✅ DynamoDB initialized successfully")

	// Create Gin router
	log.Println("🌐 Creating Gin router...")
	r := gin.Default()

	// Add request logging middleware
	r.Use(gin.LoggerWithConfig(gin.LoggerConfig{
		SkipPaths: []string{"/health"}, // Skip health check logs to reduce noise
		Formatter: func(param gin.LogFormatterParams) string {
			return fmt.Sprintf("📊 %s - [%s] \"%s %s %s\" %d %s \"%s\" \"%s\" %v\n",
				param.ClientIP,
				param.TimeStamp.Format(time.RFC3339),
				param.Method,
				param.Path,
				param.Request.Proto,
				param.StatusCode,
				param.Latency,
				param.Request.UserAgent(),
				param.ErrorMessage,
				param.Latency,
			)
		},
	}))

	// Add recovery middleware
	r.Use(gin.RecoveryWithWriter(os.Stdout, func(c *gin.Context, recovered interface{}) {
		log.Printf("💥 PANIC RECOVERED: %v", recovered)
		log.Printf("   Request: %s %s", c.Request.Method, c.Request.URL.String())
		log.Printf("   Headers: %+v", c.Request.Header)
	}))

	// Setup routes
	setupRoutes(r)

	// Get port from environment
	port := getEnvWithDefault("PORT", "8081")

	// Print startup information
	log.Printf("🌟 Payment service configuration complete!")
	log.Printf("📡 Server will start on port %s", port)
	log.Printf("🔧 Environment: %s", ginMode)
	log.Printf("🗄️ DynamoDB table: %s", dynamo.TableName)

	// Print available endpoints
	log.Println("🛣️ Available endpoints:")
	log.Println("  📋 GET  /health - Health check")
	log.Println("  💳 POST /api/checkout - Create checkout session")
	log.Println("  🎫 GET  /api/tier - Get user subscription tier")
	log.Println("  📄 GET  /api/subscription - Get subscription details")
	log.Println("  🔗 GET  /api/subscription/urls - Get subscription management URLs")
	log.Println("  ❌ POST /api/cancel-subscription - Cancel subscription")
	log.Println("  🪝 POST /api/webhook - LemonSqueezy webhook handler")

	log.Printf("🚀 Starting HTTP server on :%s...", port)

	// Start server
	if err := r.Run(":" + port); err != nil {
		log.Fatalf("💥 Failed to start server: %v", err)
	}
}
