package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"gateway/aws"
	"gateway/pkg/logger"
)

// SetupProfileRoutes sets up all profile-related API routes
func SetupProfileRoutes(mux *http.ServeMux, apiVersion string) {
	// Profile routes
	mux.HandleFunc(fmt.Sprintf("/%s/profiles/user/", apiVersion), handleProfileByUserID)
	mux.HandleFunc(fmt.Sprintf("/%s/profiles/username/check", apiVersion), handleCheckUsernameAvailability)
	mux.HandleFunc(fmt.Sprintf("/%s/profiles/username/", apiVersion), handleGetUsernameByUserID)
	mux.HandleFunc(fmt.Sprintf("/%s/profiles/", apiVersion), handleProfileOperations)
	mux.HandleFunc(fmt.Sprintf("/%s/profiles", apiVersion), handleCreateProfile)
}

// handleProfileByUserID handles GET /v1/profiles/user/{userId} and GET /v1/profiles/user/{userId}/all
func handleProfileByUserID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		sendAPIErrorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := extractPathParam(r.URL.Path, fmt.Sprintf("/%s/profiles/user/", APIVersion))
	if userID == "" {
		sendAPIErrorResponse(w, "User ID is required", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	client := aws.GetDynamoDBClient(ctx)

	// Check if it's the /all endpoint
	if strings.HasSuffix(r.URL.Path, "/all") {
		userID = strings.TrimSuffix(userID, "/all")
		// For now, we'll return a single profile as an array since we don't have GetProfilesByUserID
		profile, err := aws.GetProfileByUserID(ctx, client, userID)
		if err != nil {
			logger.GetDailyLogger().Error("Error getting profile by user ID: %v", err)
			sendAPIErrorResponse(w, "Failed to get profiles", http.StatusInternalServerError)
			return
		}
		sendJSONResponse(w, []*aws.Profile{profile}, http.StatusOK)
		return
	}

	// Single profile with auto-create logic
	profile, err := aws.GetProfileByUserID(ctx, client, userID)
	if err != nil {
		// If profile doesn't exist, create one
		if strings.Contains(err.Error(), "not found") {
			// Generate a unique username
			baseUsername := fmt.Sprintf("user_%s", userID[:8])
			finalUsername := baseUsername
			counter := 0

			// Ensure username is unique
			for counter < 100 {
				available, checkErr := aws.CheckUsernameAvailable(ctx, client, finalUsername, "")
				if checkErr != nil {
					logger.GetDailyLogger().Error("Error checking username availability: %v", checkErr)
					sendAPIErrorResponse(w, "Failed to create profile", http.StatusInternalServerError)
					return
				}

				if available {
					break
				}

				counter++
				finalUsername = fmt.Sprintf("%s_%d", baseUsername, counter)
			}

			newProfile := aws.Profile{
				UserID:         userID,
				CreatedAt:      time.Now(),
				UpdatedAt:      time.Now(),
				HasOnboarded:   false,
				ProfileContext: "",
				DisplayName:    "",
				Username:       finalUsername,
			}

			createdProfile, createErr := aws.CreateProfile(ctx, client, newProfile)
			if createErr != nil {
				logger.GetDailyLogger().Error("Error creating profile: %v", createErr)
				sendAPIErrorResponse(w, "Failed to create profile", http.StatusInternalServerError)
				return
			}
			sendJSONResponse(w, createdProfile, http.StatusCreated)
			return
		}

		logger.GetDailyLogger().Error("Error getting profile by user ID: %v", err)
		sendAPIErrorResponse(w, "Failed to get profile", http.StatusInternalServerError)
		return
	}

	sendJSONResponse(w, profile, http.StatusOK)
}

// handleCheckUsernameAvailability handles POST /v1/profiles/username/check
func handleCheckUsernameAvailability(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		sendAPIErrorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Username string `json:"username"`
		UserID   string `json:"user_id,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendAPIErrorResponse(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Username == "" {
		sendAPIErrorResponse(w, "Username is required", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	client := aws.GetDynamoDBClient(ctx)

	available, err := aws.CheckUsernameAvailable(ctx, client, req.Username, req.UserID)
	if err != nil {
		logger.GetDailyLogger().Error("Error checking username availability: %v", err)
		sendAPIErrorResponse(w, "Failed to check username availability", http.StatusInternalServerError)
		return
	}

	sendJSONResponse(w, map[string]bool{"available": available}, http.StatusOK)
}

// handleGetUsernameByUserID handles GET /v1/profiles/username/{userId}
func handleGetUsernameByUserID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		sendAPIErrorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := extractPathParam(r.URL.Path, fmt.Sprintf("/%s/profiles/username/", APIVersion))
	if userID == "" {
		sendAPIErrorResponse(w, "User ID is required", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	client := aws.GetDynamoDBClient(ctx)

	profile, err := aws.GetProfileByUserID(ctx, client, userID)
	if err != nil {
		logger.GetDailyLogger().Error("Error getting profile by user ID: %v", err)
		sendAPIErrorResponse(w, "Failed to get profile", http.StatusInternalServerError)
		return
	}

	sendJSONResponse(w, map[string]string{"username": profile.Username}, http.StatusOK)
}

// handleProfileOperations handles GET/PUT/DELETE /v1/profiles/{profileId}
func handleProfileOperations(w http.ResponseWriter, r *http.Request) {
	profileID := extractPathParam(r.URL.Path, fmt.Sprintf("/%s/profiles/", APIVersion))
	if profileID == "" {
		sendAPIErrorResponse(w, "Profile ID is required", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	client := aws.GetDynamoDBClient(ctx)

	switch r.Method {
	case http.MethodGet:
		profile, err := aws.GetProfile(ctx, client, profileID)
		if err != nil {
			logger.GetDailyLogger().Error("Error getting profile: %v", err)
			sendAPIErrorResponse(w, "Failed to get profile", http.StatusInternalServerError)
			return
		}
		sendJSONResponse(w, profile, http.StatusOK)

	case http.MethodPut:
		var profile aws.Profile
		if err := json.NewDecoder(r.Body).Decode(&profile); err != nil {
			sendAPIErrorResponse(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		profile.UserID = profileID // Ensure the ID matches the URL
		profile.UpdatedAt = time.Now()

		updatedProfile, err := aws.UpdateProfile(ctx, client, profile)
		if err != nil {
			logger.GetDailyLogger().Error("Error updating profile: %v", err)
			sendAPIErrorResponse(w, "Failed to update profile", http.StatusInternalServerError)
			return
		}
		sendJSONResponse(w, updatedProfile, http.StatusOK)

	case http.MethodDelete:
		err := aws.DeleteProfile(ctx, client, profileID)
		if err != nil {
			logger.GetDailyLogger().Error("Error deleting profile: %v", err)
			sendAPIErrorResponse(w, "Failed to delete profile", http.StatusInternalServerError)
			return
		}
		sendJSONResponse(w, map[string]bool{"success": true}, http.StatusOK)

	default:
		sendAPIErrorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleCreateProfile handles POST /v1/profiles
func handleCreateProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		sendAPIErrorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var profile aws.Profile
	if err := json.NewDecoder(r.Body).Decode(&profile); err != nil {
		sendAPIErrorResponse(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	client := aws.GetDynamoDBClient(ctx)

	profile.CreatedAt = time.Now()
	profile.UpdatedAt = time.Now()

	createdProfile, err := aws.CreateProfile(ctx, client, profile)
	if err != nil {
		logger.GetDailyLogger().Error("Error creating profile: %v", err)
		sendAPIErrorResponse(w, "Failed to create profile", http.StatusInternalServerError)
		return
	}

	sendJSONResponse(w, createdProfile, http.StatusCreated)
}
