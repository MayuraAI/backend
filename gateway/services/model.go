package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

// ModelRequest represents the request to the model service
type ModelRequest struct {
	Prompt string `json:"prompt"`
}

// ModelResponse represents the response from the model service
type ModelResponse struct {
	Model    string                `json:"model"`
	Metadata ModelResponseMetadata `json:"metadata"`
}

type ModelResponseMetadata struct {
	ProcessingTime        float64               `json:"processing_time"`
	PredictedCategory     string                `json:"predicted_category"`
	CategoryProbabilities map[string]float64    `json:"category_probabilities"`
	ModelScores           map[string]ModelScore `json:"model_scores"`
	SelectedModel         string                `json:"selected_model"`
	Confidence            float64               `json:"confidence"`
}

type ModelScore struct {
	QualityScore      float64 `json:"quality_score"`
	NormalizedQuality float64 `json:"normalized_quality"`
	Cost              float64 `json:"cost"`
	NormalizedCost    float64 `json:"normalized_cost"`
	FinalScore        float64 `json:"final_score"`
}

// getClassifierURL returns the classifier service URL from environment or default
func getClassifierURL() string {
	if url := os.Getenv("CLASSIFIER_URL"); url != "" {
		return url
	}
	return "http://localhost:8000" // Default for local development
}

// CallModelService calls the local model service and returns the response
func CallModelService(prompt string) (ModelResponse, error) {
	startTime := time.Now()

	// Prepare the request
	reqBody := ModelRequest{
		Prompt: prompt,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return ModelResponse{}, fmt.Errorf("error marshaling request: %v", err)
	}

	// Get classifier URL from environment
	classifierURL := getClassifierURL()

	// Make the request to the model service
	resp, err := http.Post(
		classifierURL+"/complete",
		"application/json",
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		return ModelResponse{}, fmt.Errorf("error calling model service: %v", err)
	}
	defer resp.Body.Close()

	// Parse the response
	var modelResp ModelResponse
	if err := json.NewDecoder(resp.Body).Decode(&modelResp); err != nil {
		return ModelResponse{}, fmt.Errorf("error decoding response: %v", err)
	}

	// Log the response details
	logModelResponse(modelResp, time.Since(startTime))

	return modelResp, nil
}

// logModelResponse logs the model response details in a formatted way
func logModelResponse(resp ModelResponse, requestTime time.Duration) {
	log.Printf("\n=== Model Service Response ===\n")
	log.Printf("Selected Model: %s\n", resp.Metadata.SelectedModel)
	log.Printf("Confidence: %.2f\n", resp.Metadata.Confidence)
	log.Printf("Processing Time: %.2fms\n", resp.Metadata.ProcessingTime*1000)
	log.Printf("Predicted Category: %s\n", resp.Metadata.PredictedCategory)

	log.Printf("\nCategory Probabilities:\n")
	for category, prob := range resp.Metadata.CategoryProbabilities {
		log.Printf("  %-20s: %.2f%%\n", category, prob*100)
	}

	log.Printf("\nModel Scores:\n")
	for model, score := range resp.Metadata.ModelScores {
		log.Printf("  %s:\n", model)
		log.Printf("    Quality Score: %.2f\n", score.QualityScore)
		log.Printf("    Normalized Quality: %.2f\n", score.NormalizedQuality)
		log.Printf("    Cost: $%.2f\n", score.Cost)
		log.Printf("    Normalized Cost: %.2f\n", score.NormalizedCost)
		log.Printf("    Final Score: %.2f\n", score.FinalScore)
	}

	log.Printf("\nRequest Processing Time: %v\n", requestTime)
	log.Printf("===========================\n")
}
