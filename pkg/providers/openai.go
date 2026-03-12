// Package providers implements LLM provider clients for ZeroClaw.
// This module provides a universal OpenAI-compatible provider that works with
// OpenAI, Groq, OpenRouter, xAI, and any OpenAI-compatible API.
package providers

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

	"github.com/zeroclaw/zeroclaw-go/pkg/core"
)

// ============================================================================
// OPENAI-COMPATIBLE PROVIDER
// ============================================================================

// OpenAIProvider implements the Provider interface using OpenAI-compatible APIs.
// Works with: OpenAI, Groq, OpenRouter, xAI, Together, Fireworks, etc.
type OpenAIProvider struct {
	apiKey     string
	baseURL    string
	model      string
	httpClient *http.Client
}

// OpenAIConfig holds provider configuration.
type OpenAIConfig struct {
	// APIKey is required for authentication.
	APIKey string
	// BaseURL is the API endpoint (default: https://api.openai.com/v1).
	BaseURL string
	// Model is the model identifier (default: gpt-4o-mini).
	Model string
	// Timeout for HTTP requests (default: 60s).
	Timeout time.Duration
}

// NewOpenAIProvider creates a new provider from environment variables.
// Environment variables:
// - OPENAI_API_KEY (required)
// - OPENAI_BASE_URL (optional, default: https://api.openai.com/v1)
// - OPENAI_MODEL (optional, default: gpt-4o-mini)
func NewOpenAIProvider() *OpenAIProvider {
	return NewOpenAIProviderWithConfig(nil)
}

// NewOpenAIProviderWithConfig creates a provider with explicit config.
func NewOpenAIProviderWithConfig(cfg *OpenAIConfig) *OpenAIProvider {
	if cfg == nil {
		cfg = &OpenAIConfig{}
	}

	// Load from environment if not set
	if cfg.APIKey == "" {
		cfg.APIKey = os.Getenv("OPENAI_API_KEY")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = os.Getenv("OPENAI_BASE_URL")
		if cfg.BaseURL == "" {
			cfg.BaseURL = "https://api.openai.com/v1"
		}
	}
	if cfg.Model == "" {
		cfg.Model = os.Getenv("OPENAI_MODEL")
		if cfg.Model == "" {
			cfg.Model = "gpt-4o-mini"
		}
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 50 * time.Second
	}

	// Normalize base URL (remove trailing slash)
	cfg.BaseURL = strings.TrimSuffix(cfg.BaseURL, "/")

	return &OpenAIProvider{
		apiKey:  cfg.APIKey,
		baseURL: cfg.BaseURL,
		model:   cfg.Model,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

// ============================================================================
// PROVIDER INTERFACE IMPLEMENTATION
// ============================================================================

// Name returns the provider name.
func (p *OpenAIProvider) Name() string {
	return "openai-compatible"
}

// SimpleChat performs a one-shot chat without history.
func (p *OpenAIProvider) SimpleChat(ctx context.Context, message, model string, temperature float64) (string, error) {
	return p.ChatWithSystem(ctx, "", message, model, temperature)
}

// ChatWithSystem performs a chat with optional system prompt.
func (p *OpenAIProvider) ChatWithSystem(ctx context.Context, systemPrompt, message, model string, temperature float64) (string, error) {
	messages := []core.ChatMessage{}

	if systemPrompt != "" {
		messages = append(messages, core.NewSystemMessage(systemPrompt))
	}
	messages = append(messages, core.NewUserMessage(message))

	return p.ChatWithHistory(ctx, messages, model, temperature)
}

// ChatWithHistory performs a multi-turn conversation.
func (p *OpenAIProvider) ChatWithHistory(ctx context.Context, messages []core.ChatMessage, model string, temperature float64) (string, error) {
	resp, err := p.Chat(ctx, messages, nil, model, temperature)
	if err != nil {
		return "", err
	}
	return resp.TextOrEmpty(), nil
}

// Chat performs a structured chat with tool support.
func (p *OpenAIProvider) Chat(ctx context.Context, messages []core.ChatMessage, tools []core.ToolSpec, model string, temperature float64) (*core.ChatResponse, error) {
	// Use default model if not specified
	if model == "" {
		model = p.model
	}

	// Build request body
	reqBody := p.buildChatRequest(messages, tools, model, temperature)

	// Make API request
	resp, err := p.doRequest(ctx, "/chat/completions", reqBody)
	if err != nil {
		return nil, fmt.Errorf("chat request failed: %w", err)
	}

	// Parse response
	return p.parseChatResponse(resp)
}

// SupportsNativeTools returns true (OpenAI supports function calling).
func (p *OpenAIProvider) SupportsNativeTools() bool {
	return true
}

// SupportsVision returns true (most OpenAI-compatible APIs support vision).
func (p *OpenAIProvider) SupportsVision() bool {
	return true
}

// GetEmbedding generates an embedding for text.
func (p *OpenAIProvider) GetEmbedding(ctx context.Context, text string) ([]float32, error) {
	reqBody := map[string]interface{}{
		"model": "text-embedding-3-small",
		"input": text,
	}

	resp, err := p.doRequest(ctx, "/embeddings", reqBody)
	if err != nil {
		// Specific handling for Cerebras which does not support embeddings (returns 404)
		if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "Not Found") {
			return nil, fmt.Errorf("provider does not support embeddings (404 Not Found) - ignoring vector search")
		}
		return nil, err
	}

	if len(resp) == 0 {
		return nil, fmt.Errorf("empty response from provider")
	}

	// Parse embedding response
	var embResp struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}

	if err := json.Unmarshal(resp, &embResp); err != nil {
		return nil, fmt.Errorf("failed to parse embedding response: %w", err)
	}

	if len(embResp.Data) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}

	return embResp.Data[0].Embedding, nil
}

// GenerateEmbedding implements core.EmbeddingService interface.
// Delegates to GetEmbedding so OpenAIProvider can be used as a memory embedding service.
func (p *OpenAIProvider) GenerateEmbedding(ctx context.Context, text string) ([]float32, error) {
	return p.GetEmbedding(ctx, text)
}

// GenerateEmbeddings implements core.EmbeddingService for batch embedding.
func (p *OpenAIProvider) GenerateEmbeddings(ctx context.Context, texts []string) ([][]float32, error) {
	results := make([][]float32, len(texts))
	for i, text := range texts {
		emb, err := p.GetEmbedding(ctx, text)
		if err != nil {
			return nil, fmt.Errorf("failed to generate embedding for text %d: %w", i, err)
		}
		results[i] = emb
	}
	return results, nil
}

// Dimension implements core.EmbeddingService. Returns 1536 for text-embedding-3-small.
func (p *OpenAIProvider) Dimension() int {
	return 1536
}

// ============================================================================
// REQUEST BUILDING
// ============================================================================

// buildChatRequest constructs the chat completion request body.
func (p *OpenAIProvider) buildChatRequest(messages []core.ChatMessage, tools []core.ToolSpec, model string, temperature float64) map[string]interface{} {
	reqBody := map[string]interface{}{
		"model":      model,
		"temperature": temperature,
	}

	// Convert messages to OpenAI format
	apiMessages := make([]map[string]interface{}, len(messages))
	for i, msg := range messages {
		apiMessages[i] = map[string]interface{}{
			"role":    msg.Role,
			"content": msg.Content,
		}
	}
	reqBody["messages"] = apiMessages

	// Add tools if provided
	if len(tools) > 0 {
		reqBody["tools"] = p.convertTools(tools)
	}

	return reqBody
}

// convertTools converts ToolSpecs to OpenAI function format.
func (p *OpenAIProvider) convertTools(tools []core.ToolSpec) []map[string]interface{} {
	result := make([]map[string]interface{}, len(tools))
	for i, tool := range tools {
		result[i] = map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        tool.Name,
				"description": tool.Description,
				"parameters":  tool.Parameters,
			},
		}
	}
	return result
}

// ============================================================================
// HTTP REQUEST
// ============================================================================

// doRequest makes an HTTP request to the API.
func (p *OpenAIProvider) doRequest(ctx context.Context, endpoint string, body interface{}) ([]byte, error) {
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := p.baseURL + endpoint
	
	maxRetries := 3
	var lastErr error
	var respBody []byte
	var statusCode int

	for i := 0; i <= maxRetries; i++ {
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+p.apiKey)

		// Add OpenRouter-specific headers if using OpenRouter
		if strings.Contains(p.baseURL, "openrouter.ai") {
			req.Header.Set("HTTP-Referer", "https://zeroclaw.dev")
			req.Header.Set("X-Title", "ZeroClaw")
		}

		resp, err := p.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("HTTP request failed: %w", err)
			// Network error, usually safe to retry depending on context
			if i < maxRetries {
				sleepTime := time.Duration(1<<i) * time.Second
				fmt.Printf("ZeroClaw Provider: Request failed (%v), retrying in %v...\n", err, sleepTime)
				time.Sleep(sleepTime)
			}
			continue
		}

		respBody, err = io.ReadAll(resp.Body)
		statusCode = resp.StatusCode
		resp.Body.Close()

		if err != nil {
			lastErr = fmt.Errorf("failed to read response: %w", err)
			continue
		}

		// Success or non-retryable error
		if statusCode != http.StatusTooManyRequests && statusCode < 500 {
			break
		}
		
		lastErr = fmt.Errorf("API error (status %d): %s", statusCode, string(respBody))
		if i < maxRetries {
			// Exponential backoff
			sleepTime := time.Duration(1<<i) * time.Second
			fmt.Printf("ZeroClaw Provider: Rate limited or server error (%d), retrying in %v...\n", statusCode, sleepTime)
			time.Sleep(sleepTime)
		}
	}

	if statusCode >= 400 {
		return nil, lastErr
	}

	return respBody, nil
}

// ============================================================================
// RESPONSE PARSING
// ============================================================================

// parseChatResponse parses the chat completion response.
func (p *OpenAIProvider) parseChatResponse(body []byte) (*core.ChatResponse, error) {
	var resp struct {
		Choices []struct {
			Message struct {
				Role      string `json:"role"`
				Content   interface{} `json:"content"`
				ToolCalls []struct {
					ID   string `json:"id"`
					Type string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
			TotalTokens      int64 `json:"total_tokens"`
		} `json:"usage"`
		Error *struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
		} `json:"error"`
	}

	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Check for API error
	if resp.Error != nil {
		return nil, fmt.Errorf("API error: %s", resp.Error.Message)
	}

	// Check for empty response
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	choice := resp.Choices[0]

	// Extract text content
	var text string
	switch v := choice.Message.Content.(type) {
	case string:
		text = v
	case nil:
		text = ""
	default:
		text = fmt.Sprintf("%v", v)
	}

	// Extract tool calls
	var toolCalls []core.ToolCall
	for _, tc := range choice.Message.ToolCalls {
		toolCalls = append(toolCalls, core.ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		})
	}

	// Build response
	result := &core.ChatResponse{
		Text:      &text,
		ToolCalls: toolCalls,
	}

	// Add usage if available
	if resp.Usage.TotalTokens > 0 {
		result.Usage = &core.TokenUsage{
			InputTokens:  &resp.Usage.PromptTokens,
			OutputTokens: &resp.Usage.CompletionTokens,
		}
	}

	return result, nil
}

// ============================================================================
// CONVENIENCE FUNCTIONS
// ============================================================================

// NewGroqProvider creates a provider configured for Groq.
func NewGroqProvider(apiKey string) *OpenAIProvider {
	return NewOpenAIProviderWithConfig(&OpenAIConfig{
		APIKey:  apiKey,
		BaseURL: "https://api.groq.com/openai/v1",
		Model:   "llama-3.1-8b-instant",
	})
}

// NewOpenRouterProvider creates a provider configured for OpenRouter.
func NewOpenRouterProvider(apiKey string) *OpenAIProvider {
	return NewOpenAIProviderWithConfig(&OpenAIConfig{
		APIKey:  apiKey,
		BaseURL: "https://openrouter.ai/api/v1",
		Model:   "openai/gpt-4o-mini",
	})
}

// NewXAIProvider creates a provider configured for xAI (Grok).
func NewXAIProvider(apiKey string) *OpenAIProvider {
	return NewOpenAIProviderWithConfig(&OpenAIConfig{
		APIKey:  apiKey,
		BaseURL: "https://api.x.ai/v1",
		Model:   "grok-beta",
	})
}

// NewTogetherProvider creates a provider configured for Together AI.
func NewTogetherProvider(apiKey string) *OpenAIProvider {
	return NewOpenAIProviderWithConfig(&OpenAIConfig{
		APIKey:  apiKey,
		BaseURL: "https://api.together.xyz/v1",
		Model:   "meta-llama/Llama-3-8b-chat-hf",
	})
}

// NewFireworksProvider creates a provider configured for Fireworks AI.
func NewFireworksProvider(apiKey string) *OpenAIProvider {
	return NewOpenAIProviderWithConfig(&OpenAIConfig{
		APIKey:  apiKey,
		BaseURL: "https://api.fireworks.ai/inference/v1",
		Model:   "accounts/fireworks/models/llama-v3-8b-chat",
	})
}
