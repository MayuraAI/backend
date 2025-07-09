package middleware

import (
	"net/http"
	"strings"
)

// getAllowedOrigins returns the list of allowed origins
func getAllowedOrigins() []string {
	return []string{
		"https://mayura.rocks",
		"http://localhost:3000",
		"http://localhost:3001",
		"http://127.0.0.1:3000",
		"http://127.0.0.1:3001",
	}
}

// isOriginAllowed checks if the origin is in the allowed list
func isOriginAllowed(origin string) bool {
	allowedOrigins := getAllowedOrigins()
	for _, allowedOrigin := range allowedOrigins {
		if strings.EqualFold(origin, allowedOrigin) {
			return true
		}
	}
	return false
}

// CORSMiddleware handles Cross-Origin Resource Sharing
func CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// origin := r.Header.Get("Origin")

		// Set CORS headers
		// if origin != "" && isOriginAllowed(origin) {
		// 	w.Header().Set("Access-Control-Allow-Origin", origin)
		// } else {
		// 	// Default to first allowed origin for non-matching origins
		// 	w.Header().Set("Access-Control-Allow-Origin", getAllowedOrigins()[0])
		// }

		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Max-Age", "86400")

		// Handle preflight requests
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		// Continue with the next handler
		next.ServeHTTP(w, r)
	})
}
