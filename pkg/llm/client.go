package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"clawbridge/pkg/mcp"
)

// Client is a lightweight, zero-dependency LLM client communicating directly with APIs.
type Client struct {
	config ClientConfig
	client HTTPDoer
}

// DetectProviderFromEnv inspects environment variables to choose the best available LLM provider.
func DetectProviderFromEnv() (ProviderType, string, string) {
	if key := os.Getenv("GEMINI_API_KEY"); key != "" {
		return ProviderGemini, key, "gemini-1.5-flash"
	}
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		return ProviderOpenAI, key, "gpt-4o-mini"
	}
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		return ProviderAnthropic, key, "claude-3-5-haiku-20241022"
	}
	if host := os.Getenv("OLLAMA_HOST"); host != "" {
		return ProviderOllama, "", "llama3.2"
	}
	// Default to Gemini if nothing specified
	return ProviderGemini, "", "gemini-1.5-flash"
}

// NewClient initializes a new native HTTP LLM Client.
func NewClient(config ClientConfig) *Client {
	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}

	return &Client{
		config: config,
		client: httpClient,
	}
}

// Chat sends a conversational turn to the configured LLM and returns the response.
func (c *Client) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	switch c.config.Provider {
	case ProviderGemini:
		return c.chatGemini(ctx, req)
	default: // ProviderOpenAI, ProviderOllama, etc.
		return c.chatOpenAI(ctx, req)
	}
}

func (c *Client) chatOpenAI(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	baseURL := c.config.BaseURL
	if baseURL == "" {
		if c.config.Provider == ProviderOllama {
			baseURL = "http://localhost:11434/v1"
		} else {
			baseURL = "https://api.openai.com/v1"
		}
	}

	endpoint := fmt.Sprintf("%s/chat/completions", strings.TrimRight(baseURL, "/"))

	// Map messages
	var openAIMessages []map[string]any
	if req.SystemPrompt != "" {
		openAIMessages = append(openAIMessages, map[string]any{
			"role":    "system",
			"content": req.SystemPrompt,
		})
	}
	for _, m := range req.Messages {
		msgMap := map[string]any{
			"role":    m.Role,
			"content": m.Content,
		}
		if m.ToolCallID != "" {
			msgMap["tool_call_id"] = m.ToolCallID
		}
		if len(m.ToolCalls) > 0 {
			var tcList []map[string]any
			for _, tc := range m.ToolCalls {
				argsJSON, _ := json.Marshal(tc.Arguments)
				tcList = append(tcList, map[string]any{
					"id":   tc.ID,
					"type": "function",
					"function": map[string]any{
						"name":      tc.Name,
						"arguments": string(argsJSON),
					},
				})
			}
			msgMap["tool_calls"] = tcList
		}
		openAIMessages = append(openAIMessages, msgMap)
	}

	payload := map[string]any{
		"model":    req.Model,
		"messages": openAIMessages,
	}

	if len(req.Tools) > 0 {
		var openAITools []map[string]any
		for _, tool := range req.Tools {
			openAITools = append(openAITools, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        tool.Name,
					"description": tool.Description,
					"parameters":  tool.InputSchema,
				},
			})
		}
		payload["tools"] = openAITools
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal openai request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create http request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if c.config.APIKey != "" {
		httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.config.APIKey))
	}

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("openai returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Role      string `json:"role"`
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse openai response: %w", err)
	}

	if len(parsed.Choices) == 0 {
		return &ChatResponse{}, nil
	}

	choiceMsg := parsed.Choices[0].Message
	var toolCalls []ToolCall
	for _, tc := range choiceMsg.ToolCalls {
		var args map[string]any
		_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
		toolCalls = append(toolCalls, ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: args,
		})
	}

	return &ChatResponse{
		Content:   choiceMsg.Content,
		ToolCalls: toolCalls,
	}, nil
}

func (c *Client) chatGemini(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	baseURL := c.config.BaseURL
	var endpoint string
	if baseURL != "" {
		endpoint = baseURL
	} else {
		endpoint = fmt.Sprintf(
			"https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent",
			req.Model,
		)
	}

	var contents []map[string]any
	for _, m := range req.Messages {
		role := "user"
		if m.Role == "assistant" {
			role = "model"
		}
		var parts []map[string]any
		if m.Content != "" {
			parts = append(parts, map[string]any{"text": m.Content})
		}
		for _, tc := range m.ToolCalls {
			parts = append(parts, map[string]any{
				"functionCall": map[string]any{
					"name": tc.Name,
					"args": tc.Arguments,
				},
			})
		}
		contents = append(contents, map[string]any{
			"role":  role,
			"parts": parts,
		})
	}

	payload := map[string]any{
		"contents": contents,
	}
	if req.SystemPrompt != "" {
		payload["systemInstruction"] = map[string]any{
			"parts": []map[string]any{
				{"text": req.SystemPrompt},
			},
		}
	}

	if len(req.Tools) > 0 {
		var funcDecls []map[string]any
		for _, tool := range req.Tools {
			funcDecls = append(funcDecls, map[string]any{
				"name":        tool.Name,
				"description": tool.Description,
				"parameters":  tool.InputSchema,
			})
		}
		payload["tools"] = []map[string]any{
			{"functionDeclarations": funcDecls},
		}
	}

	bodyBytes, _ := json.Marshal(payload)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create gemini request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if c.config.APIKey != "" {
		httpReq.Header.Set("x-goog-api-key", c.config.APIKey)
	}

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("gemini request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("gemini returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var parsed struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text         string `json:"text"`
					FunctionCall *struct {
						Name string         `json:"name"`
						Args map[string]any `json:"args"`
					} `json:"functionCall"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}

	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse gemini response: %w", err)
	}

	if len(parsed.Candidates) == 0 {
		return &ChatResponse{}, nil
	}

	var fullText string
	var toolCalls []ToolCall

	for _, part := range parsed.Candidates[0].Content.Parts {
		if part.Text != "" {
			fullText += part.Text
		}
		if part.FunctionCall != nil {
			toolCalls = append(toolCalls, ToolCall{
				ID:        fmt.Sprintf("gemini_call_%s", part.FunctionCall.Name),
				Name:      part.FunctionCall.Name,
				Arguments: part.FunctionCall.Args,
			})
		}
	}

	return &ChatResponse{
		Content:   fullText,
		ToolCalls: toolCalls,
	}, nil
}

// ChatWithToolExecution executes an autonomous tool execution loop between the LLM and ClawBridge tools.
func (c *Client) ChatWithToolExecution(
	ctx context.Context,
	req ChatRequest,
	tools *mcp.Tools,
	maxTurns int,
) (*ChatResponse, error) {
	if maxTurns <= 0 {
		maxTurns = 5
	}

	currentReq := req

	for turn := 0; turn < maxTurns; turn++ {
		resp, err := c.Chat(ctx, currentReq)
		if err != nil {
			return nil, err
		}

		if len(resp.ToolCalls) == 0 {
			return resp, nil
		}

		// Append assistant tool calls turn
		currentReq.Messages = append(currentReq.Messages, Message{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		// Execute each requested tool through ClawBridge tools
		for _, tc := range resp.ToolCalls {
			receipt, err := tools.CallTool(ctx, tc.Name, tc.Arguments)
			var receiptStr string
			if err != nil {
				receiptStr = fmt.Sprintf(`{"status":"error","message":"%s"}`, err.Error())
			} else {
				b, _ := json.Marshal(receipt)
				receiptStr = string(b)
			}

			currentReq.Messages = append(currentReq.Messages, Message{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    receiptStr,
			})
		}
	}

	return nil, fmt.Errorf("max turns (%d) reached without completing tool execution loop", maxTurns)
}
