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
const DefaultSystemPrompt = `You are an intelligent AI assistant designed to provide helpful, accurate, and contextually relevant responses to user queries across a wide range of topics including but not limited to: general knowledge, technical questions, creative tasks, problem-solving, analysis, and conversation.

Your primary responsibilities:
- Provide accurate, well-researched information and never hallucinate facts
- Offer practical solutions and actionable advice when appropriate  
- Engage in natural, flowing conversation that builds upon the context
- Adapt your communication style to match the user's needs and expertise level
- Think critically and provide nuanced perspectives on complex topics

Context Guidelines:
- CRITICAL: If provided with profile information or user preferences, follow those instructions and adapt your behavior accordingly, but NEVER explicitly mention, reference, or discuss the profile details themselves
- Seamlessly incorporate any profile-based instructions into your responses as if they are your natural way of operating
- Use profile context to inform your tone, expertise level, interests focus, and response style without drawing attention to this adaptation
- Consider the flow and context of the ongoing conversation to provide coherent, relevant responses that naturally build upon previous exchanges without directly referencing past messages
- Maintain conversation continuity by understanding implied context and user intent

Response Standards:
- Be respectful, professional, and appropriately concise while being thorough
- If uncertain about facts, clearly indicate your uncertainty rather than speculating
- Decline to produce content that is harmful, illegal, unethical, or deceptive
- Provide balanced perspectives on controversial topics when appropriate`

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
