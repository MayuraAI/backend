package firebase

import (
	"context"
	"fmt"
	"os"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
	"google.golang.org/api/option"
)

var AuthClient *auth.Client

// InitFirebase initializes Firebase app and auth client
func InitFirebase() error {
	ctx := context.Background()

	// Initialize Firebase app
	var app *firebase.App
	var err error

	// Option 1: Using service account key file
	serviceAccountPath := os.Getenv("FIREBASE_SERVICE_ACCOUNT_PATH")
	if serviceAccountPath != "" {
		// Check if file exists
		if _, err := os.Stat(serviceAccountPath); err == nil {
			opt := option.WithCredentialsFile(serviceAccountPath)
			app, err = firebase.NewApp(ctx, nil, opt)
			if err != nil {
				return fmt.Errorf("firebase init with file failed: %v", err)
			}
		} else {
			fmt.Printf("Warning: Firebase service account file not found at %s\n", serviceAccountPath)
		}
	}

	// Option 2: Using service account JSON from environment variable
	if app == nil {
		serviceAccountJSON := os.Getenv("FIREBASE_SERVICE_ACCOUNT_JSON")
		if serviceAccountJSON != "" {
			opt := option.WithCredentialsJSON([]byte(serviceAccountJSON))
			app, err = firebase.NewApp(ctx, nil, opt)
			if err != nil {
				return fmt.Errorf("firebase init with JSON failed: %v", err)
			}
		}
	}

	// Option 3: Using default credentials (for Google Cloud environments)
	if app == nil {
		// Check if we're in a development environment
		if os.Getenv("DEVELOPMENT") == "true" || os.Getenv("GIN_MODE") == "debug" {
			fmt.Println("Warning: Running in development mode without Firebase credentials")
			// Create a mock auth client for development
			return nil
		}

		// Try default credentials for production
		app, err = firebase.NewApp(ctx, nil)
		if err != nil {
			return fmt.Errorf("firebase init with default credentials failed: %v", err)
		}
	}

	// Initialize Auth client
	if app != nil {
		AuthClient, err = app.Auth(ctx)
		if err != nil {
			return fmt.Errorf("auth client init failed: %v", err)
		}
		fmt.Println("Firebase initialized successfully")
	}

	return nil
}

// VerifyIDToken verifies the Firebase ID token and returns the user UID
func VerifyIDToken(ctx context.Context, idToken string) (string, error) {
	if AuthClient == nil {
		// For development without Firebase
		if os.Getenv("DEVELOPMENT") == "true" || os.Getenv("GIN_MODE") == "debug" {
			// Return a mock user ID for development
			return "dev-user-123", nil
		}
		return "", fmt.Errorf("firebase auth client not initialized")
	}

	token, err := AuthClient.VerifyIDToken(ctx, idToken)
	if err != nil {
		return "", err
	}
	return token.UID, nil
}

// GetUserRecord retrieves user information from Firebase
func GetUserRecord(ctx context.Context, uid string) (*auth.UserRecord, error) {
	if AuthClient == nil {
		return nil, fmt.Errorf("firebase auth client not initialized")
	}
	return AuthClient.GetUser(ctx, uid)
}
