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
		opt := option.WithCredentialsFile(serviceAccountPath)
		app, err = firebase.NewApp(ctx, nil, opt)
		if err != nil {
			return fmt.Errorf("firebase init failed: %v", err)
		}
	} else {
		// Option 2: Using service account JSON from environment variable
		serviceAccountJSON := os.Getenv("FIREBASE_SERVICE_ACCOUNT_JSON")
		if serviceAccountJSON != "" {
			opt := option.WithCredentialsJSON([]byte(serviceAccountJSON))
			app, err = firebase.NewApp(ctx, nil, opt)
			if err != nil {
				return fmt.Errorf("firebase init failed: %v", err)
			}
		} else {
			// Option 3: Using default credentials (for Google Cloud environments)
			app, err = firebase.NewApp(ctx, nil)
			if err != nil {
				return fmt.Errorf("firebase init failed: %v", err)
			}
		}
	}

	// Initialize Auth client
	AuthClient, err = app.Auth(ctx)
	if err != nil {
		return fmt.Errorf("auth client init failed: %v", err)
	}

	return nil
}

// VerifyIDToken verifies the Firebase ID token and returns the user UID
func VerifyIDToken(ctx context.Context, idToken string) (string, error) {
	token, err := AuthClient.VerifyIDToken(ctx, idToken)
	if err != nil {
		return "", err
	}
	return token.UID, nil
}

// GetUserRecord retrieves user information from Firebase
func GetUserRecord(ctx context.Context, uid string) (*auth.UserRecord, error) {
	return AuthClient.GetUser(ctx, uid)
}
