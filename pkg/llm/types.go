package llm

import (
	"net/http"

	"clawbridge/pkg/mcp"
)

// ProviderType identifies the underlying LLM service.
type ProviderType string

const (
	ProviderGemini    ProviderType = "gemini"
	ProviderOpenAI    ProviderType = "openai"
	ProviderOllama    ProviderType = "ollama"
	ProviderAnthropic ProviderType = "anthropic"
)

// ToolCall represents a model-requested tool invocation.
type ToolCall struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// Message represents a single chat turn in the conversation.
type Message struct {
	Role       string     `json:"role"` // "system", "user", "assistant", "tool"
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ChatRequest contains messages, model parameters, and available tools.
type ChatRequest struct {
	Model        string               `json:"model"`
	Messages     []Message            `json:"messages"`
	Tools        []mcp.ToolDefinition `json:"tools,omitempty"`
	SystemPrompt string               `json:"system_prompt,omitempty"`
	Temperature  float64              `json:"temperature,omitempty"`
}

// ChatResponse contains the assistant output and any requested tool calls.
type ChatResponse struct {
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// HTTPDoer is an interface matching http.Client.Do.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// ClientConfig configures the native HTTP LLM client.
type ClientConfig struct {
	Provider   ProviderType
	APIKey     string
	BaseURL    string
	Model      string
	HTTPClient HTTPDoer
}
