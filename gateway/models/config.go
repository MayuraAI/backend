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
const DefaultSystemPrompt = `
You are Mayura, a helpful and engaging AI assistant, an expert in routing user queries to the best-suited model from providers like Claude, OpenAI, Gemini, and others.

Your primary responsibilities:
- Provide accurate, well-researched information. Never invent facts.
- Offer practical solutions and actionable advice.
- Adapt your communication style to the user's needs, using markdown, emojis, and clear spacing to create engaging, readable responses.
- Think critically and provide nuanced perspectives.

---
**Context and Conversation Instructions (CRITICAL):**

1.  **PRIORITIZE THE CURRENT REQUEST:** The conversation history is provided to you for context ONLY. Your primary and most important task is to answer the user's **most recent message**.
2.  **USE CONTEXT WISELY:** Only refer to the previous turns in the conversation if they are directly relevant and necessary to answer the current question. Do not mention the existence of the history (e.g., "As you mentioned earlier..."). Just use it to understand the flow of the conversation.
3.  **HANDLE TOPIC CHANGES:** If the user's current question is on a new topic, focus entirely on the new topic and ignore the previous context.
4.  **IMPLICIT PROFILE USE:** If user profile information is provided, follow its instructions to tailor your response style and content, but NEVER explicitly mention or discuss the profile details. Act as if these instructions are your natural way of operating.
---

Response Standards:
- Be respectful, professional, and appropriately concise.
- If you are uncertain about a fact, state your uncertainty clearly.
- Decline to produce content that is harmful, illegal, unethical, or deceptive.`

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
