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
	mux.HandleFunc(fmt.Sprintf("/%s/profiles/users/", apiVersion), handleProfilesByUserID)
	mux.HandleFunc(fmt.Sprintf("/%s/profiles/username/check", apiVersion), handleUsernameCheckCombined)
	mux.HandleFunc(fmt.Sprintf("/%s/profiles/username/", apiVersion), handleGetUsernameByUserID)
	mux.HandleFunc(fmt.Sprintf("/%s/profiles/", apiVersion), handleProfileOperations)
	mux.HandleFunc(fmt.Sprintf("/%s/profiles", apiVersion), handleCreateProfile)
}

// handleProfileByUserID handles GET /v1/profiles/by-user-id/{userId} and GET /v1/profiles/by-user-id/{userId}/all
func handleProfileByUserID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		sendAPIErrorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := extractPathParam(r.URL.Path, fmt.Sprintf("/%s/profiles/by-user-id/", APIVersion))
	if userID == "" {
		sendAPIErrorResponse(w, "User ID is required", http.StatusBadRequest)
		return
	}
	logger := logger.GetDailyLogger()
	logger.Debug("handleProfileByUserID", "userID", userID)
	logger.Debug("handleProfileByUserID", "r.URL.Path", r.URL.Path)

	ctx := context.Background()
	client := aws.GetDynamoDBClient(ctx)

	// Check if it's the /all endpoint
	if strings.HasSuffix(r.URL.Path, "/all") {
		userID = strings.TrimSuffix(userID, "/all")
		// For now, we'll return a single profile as an array since we don't have GetProfilesByUserID
		profile, err := aws.GetProfileByUserID(ctx, client, userID)
		if err != nil {
			logger.Error("Error getting profile by user ID: %v", err)
			sendAPIErrorResponse(w, "Failed to get profiles", http.StatusInternalServerError)
			return
		}
		sendJSONResponse(w, []*aws.Profile{profile}, http.StatusOK)
		return
	}

	logger.Debug("getting profile by user ID")
	// Single profile with auto-create logic
	profile, err := aws.GetProfileByUserID(ctx, client, userID)
	logger.Debug("profile", "profile", profile)
	logger.Debug("profile", "err", err)
	if err != nil {
		// If profile doesn't exist, create one
		if strings.Contains(err.Error(), "not found") {
			logger.Debug("profile not found, creating profile")
			// Generate a unique username
			baseUsername := fmt.Sprintf("user_%s", userID[:8])
			finalUsername := baseUsername
			counter := 0

			// Ensure username is unique
			for counter < 100 {
				available, checkErr := aws.CheckUsernameAvailable(ctx, client, finalUsername, "")
				if checkErr != nil {
					logger.Error("Error checking username availability: %v", checkErr)
					sendAPIErrorResponse(w, "Failed to create profile", http.StatusInternalServerError)
					return
				}

				if available {
					break
				}

				counter++
				finalUsername = fmt.Sprintf("%s_%d", baseUsername, counter)
			}

			logger.Debug("finalUsername", "finalUsername", finalUsername)

			newProfile := aws.Profile{
				UserID:         userID,
				CreatedAt:      time.Now(),
				UpdatedAt:      time.Now(),
				HasOnboarded:   false,
				ProfileContext: "",
				DisplayName:    "",
				Username:       finalUsername,
			}

			logger.Debug("newProfile", "newProfile", newProfile)

			createdProfile, createErr := aws.CreateProfile(ctx, client, newProfile)
			logger.Debug("createdProfile", "createdProfile", createdProfile)
			logger.Debug("createErr", "createErr", createErr)
			if createErr != nil {
				logger.Error("Error creating profile: %v", createErr)
				sendAPIErrorResponse(w, "Failed to create profile", http.StatusInternalServerError)
				return
			}
			sendJSONResponse(w, createdProfile, http.StatusCreated)
			return
		}

		logger.Error("Error getting profile by user ID: %v", err)
		sendAPIErrorResponse(w, "Failed to get profile", http.StatusInternalServerError)
		return
	}

	sendJSONResponse(w, profile, http.StatusOK)
}

// handleProfileCombined handles both collection and individual profile operations
func handleProfileCombined(w http.ResponseWriter, r *http.Request) {
	// Extract potential profile ID from path
	profileID := extractPathParam(r.URL.Path, fmt.Sprintf("/%s/profiles/", APIVersion))

	// If no profile ID, this is a collection operation
	if profileID == "" {
		// Handle collection operations (POST to create)
		if r.Method == http.MethodPost {
			handleCreateProfile(w, r)
		} else {
			sendAPIErrorResponse(w, "Method not allowed for collection", http.StatusMethodNotAllowed)
		}
	} else {
		// Handle individual profile operations
		handleProfileOperations(w, r)
	}
}

// handleUsernameCheckCombined handles both GET and POST requests for username checking
func handleUsernameCheckCombined(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		handleCheckUsernameAvailabilityGET(w, r)
	case http.MethodPost:
		handleCheckUsernameAvailability(w, r)
	default:
		sendAPIErrorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleCheckUsernameAvailabilityGET handles GET /v1/profiles/username/check?username={username}&exclude_user_id={userId}
func handleCheckUsernameAvailabilityGET(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		sendAPIErrorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get username from query parameters
	username := r.URL.Query().Get("username")
	if username == "" {
		sendAPIErrorResponse(w, "Username query parameter is required", http.StatusBadRequest)
		return
	}

	// Get exclude_user_id from query parameters
	excludeUserID := r.URL.Query().Get("exclude_user_id")

	ctx := context.Background()
	client := aws.GetDynamoDBClient(ctx)

	available, err := aws.CheckUsernameAvailable(ctx, client, username, excludeUserID)
	if err != nil {
		logger.GetDailyLogger().Error("Error checking username availability: %v", err)
		sendAPIErrorResponse(w, "Failed to check username availability", http.StatusInternalServerError)
		return
	}

	sendJSONResponse(w, map[string]bool{"available": available}, http.StatusOK)
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

// handleGetUsernameByUserID handles GET /v1/profiles/get-username-by-user-id/{userId}
func handleGetUsernameByUserID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		sendAPIErrorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := extractPathParam(r.URL.Path, fmt.Sprintf("/%s/profiles/get-username-by-user-id/", APIVersion))
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

// handleProfilesByUserID handles GET /v1/profiles/users/{userId} - returns array of profiles
func handleProfilesByUserID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		sendAPIErrorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := extractPathParam(r.URL.Path, fmt.Sprintf("/%s/profiles/users/", APIVersion))
	if userID == "" {
		sendAPIErrorResponse(w, "User ID is required", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	client := aws.GetDynamoDBClient(ctx)

	// For now, we'll return a single profile as an array since we don't have GetProfilesByUserID
	// In the future, this could be expanded to return multiple profiles if needed
	profile, err := aws.GetProfileByUserID(ctx, client, userID)
	if err != nil {
		logger.GetDailyLogger().Error("Error getting profile by user ID: %v", err)
		sendAPIErrorResponse(w, "Failed to get profiles", http.StatusInternalServerError)
		return
	}

	sendJSONResponse(w, []*aws.Profile{profile}, http.StatusOK)
}
