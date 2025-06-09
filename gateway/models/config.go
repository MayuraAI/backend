package models

import (
	"os"
	"sync"
)

// SystemPromptConfig holds the configuration for system prompts
type SystemPromptConfig struct {
	// GlobalSystemPrompt is the default system prompt used for all conversations
	GlobalSystemPrompt string

	// ModelSpecificPrompts contains system prompts specific to each model
	ModelSpecificPrompts map[string]string

	// Lock for thread safety when updating prompts
	mux sync.RWMutex
}

// DefaultSystemPrompt is the default system prompt used when no specific prompt is provided
const DefaultSystemPrompt = `You are a helpful, harmless, and honest AI assistant. 
You always provide accurate information and never hallucinate facts. 
If you're unsure about something, admit it rather than making up an answer. 
You should be respectful, professional, and concise in your responses. 
You must not generate harmful, illegal, unethical or deceptive content. 
You should decline to produce content that is harmful, illegal, or unethical.`

// Config is the global configuration instance
var Config = &SystemPromptConfig{
	GlobalSystemPrompt:   DefaultSystemPrompt,
	ModelSpecificPrompts: map[string]string{},
}

// GetSystemPrompt returns the system prompt for the given model
func (c *SystemPromptConfig) GetSystemPrompt(model string) string {
	c.mux.RLock()
	defer c.mux.RUnlock()

	// Check if there's a model-specific prompt
	if prompt, ok := c.ModelSpecificPrompts[model]; ok && prompt != "" {
		return prompt
	}

	// Otherwise return the global prompt
	return c.GlobalSystemPrompt
}

// SetGlobalSystemPrompt updates the global system prompt
func (c *SystemPromptConfig) SetGlobalSystemPrompt(prompt string) {
	c.mux.Lock()
	defer c.mux.Unlock()

	c.GlobalSystemPrompt = prompt
}

// SetModelSystemPrompt sets a model-specific system prompt
func (c *SystemPromptConfig) SetModelSystemPrompt(model, prompt string) {
	c.mux.Lock()
	defer c.mux.Unlock()

	c.ModelSpecificPrompts[model] = prompt
}

// LoadSystemPromptsFromEnv loads system prompts from environment variables
func (c *SystemPromptConfig) LoadSystemPromptsFromEnv() {
	// Load global system prompt from environment variable if available
	if prompt := os.Getenv("GLOBAL_SYSTEM_PROMPT"); prompt != "" {
		c.SetGlobalSystemPrompt(prompt)
	}

	// Load model-specific prompts from environment variables
	// Format: MODEL_SYSTEM_PROMPT_<MODEL_NAME>
	// Example: MODEL_SYSTEM_PROMPT_GEMINI="You are Gemini..."
	// This can be extended as needed
}
