// Package core provides the fundamental interfaces and types for the ZeroClaw Go framework.
// These interfaces are designed to be modular and interchangeable, following the same
// philosophy as the original Rust implementation but adapted for Go's idioms.
package core

import (
	"context"
	"time"
)

// ============================================================================
// MEMORY TYPES
// ============================================================================

// MemoryCategory represents the classification of a memory entry.
// This mirrors the Rust implementation's MemoryCategory enum.
type MemoryCategory string

const (
	// MemoryCategoryCore represents long-term facts, preferences, and decisions.
	MemoryCategoryCore MemoryCategory = "core"
	// MemoryCategoryDaily represents daily session logs.
	MemoryCategoryDaily MemoryCategory = "daily"
	// MemoryCategoryConversation represents conversation context.
	MemoryCategoryConversation MemoryCategory = "conversation"
)

// MemoryEntry represents a single memory entry stored in the memory backend.
type MemoryEntry struct {
	// ID is the unique identifier for the memory entry.
	ID string `json:"id"`
	// Key is the lookup key for the memory (e.g., "user_preference_theme").
	Key string `json:"key"`
	// Content is the actual text content of the memory.
	Content string `json:"content"`
	// Category classifies the memory type.
	Category MemoryCategory `json:"category"`
	// Timestamp is when the memory was created (ISO 8601 format).
	Timestamp time.Time `json:"timestamp"`
	// SessionID optionally groups related memories.
	SessionID *string `json:"session_id,omitempty"`
	// Score is the similarity score (populated during search).
	Score *float64 `json:"score,omitempty"`
	// Embedding is the vector representation (1536 dimensions for OpenAI).
	Embedding []float32 `json:"embedding,omitempty"`
	// Metadata contains additional flexible data.
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// ============================================================================
// PROVIDER TYPES
// ============================================================================

// ChatMessage represents a single message in a conversation.
type ChatMessage struct {
	// Role is the message role: "system", "user", "assistant", or "tool".
	Role string `json:"role"`
	// Content is the text content of the message.
	Content string `json:"content"`
	// ToolCallID identifies the tool execution (used for "tool" role).
	ToolCallID string `json:"tool_call_id,omitempty"`
	// ToolCalls contains the list of tools the assistant asked to invoke.
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// NewSystemMessage creates a new system message.
func NewSystemMessage(content string) ChatMessage {
	return ChatMessage{Role: "system", Content: content}
}

// NewUserMessage creates a new user message.
func NewUserMessage(content string) ChatMessage {
	return ChatMessage{Role: "user", Content: content}
}

// NewAssistantMessage creates a new assistant message.
func NewAssistantMessage(content string) ChatMessage {
	return ChatMessage{Role: "assistant", Content: content}
}

// ToolCall represents a tool call requested by the LLM.
type ToolCall struct {
	// ID is the unique identifier for the tool call.
	ID string `json:"id"`
	// Name is the name of the tool to invoke.
	Name string `json:"name"`
	// Arguments is the JSON-encoded arguments for the tool.
	Arguments string `json:"arguments"`
}

// TokenUsage represents token counts from an LLM API response.
type TokenUsage struct {
	// InputTokens is the number of tokens in the prompt.
	InputTokens *int64 `json:"input_tokens,omitempty"`
	// OutputTokens is the number of tokens in the response.
	OutputTokens *int64 `json:"output_tokens,omitempty"`
}

// ChatResponse represents an LLM response that may contain text or tool calls.
type ChatResponse struct {
	// Text is the text content of the response (may be empty if only tool calls).
	Text *string `json:"text,omitempty"`
	// ToolCalls are the tool calls requested by the LLM.
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	// Usage contains token usage information.
	Usage *TokenUsage `json:"usage,omitempty"`
	// ReasoningContent contains raw reasoning from thinking models.
	ReasoningContent *string `json:"reasoning_content,omitempty"`
}

// HasToolCalls returns true if the response contains tool calls.
func (r *ChatResponse) HasToolCalls() bool {
	return len(r.ToolCalls) > 0
}

// TextOrEmpty returns the text content or an empty string.
func (r *ChatResponse) TextOrEmpty() string {
	if r.Text == nil {
		return ""
	}
	return *r.Text
}

// ToolSpec describes a tool for LLM function calling.
type ToolSpec struct {
	// Name is the tool identifier used in function calls.
	Name string `json:"name"`
	// Description is a human-readable description of what the tool does.
	Description string `json:"description"`
	// Parameters is the JSON Schema for the tool's parameters.
	Parameters map[string]interface{} `json:"parameters"`
}

// ToolResult represents the result of a tool execution.
type ToolResult struct {
	// Success indicates whether the tool executed successfully.
	Success bool `json:"success"`
	// Output is the human-readable output from the tool.
	Output string `json:"output"`
	// Error contains the error message if execution failed.
	Error *string `json:"error,omitempty"`
}

// NewSuccessResult creates a successful tool result.
func NewSuccessResult(output string) *ToolResult {
	return &ToolResult{Success: true, Output: output}
}

// NewErrorResult creates a failed tool result.
func NewErrorResult(err string) *ToolResult {
	return &ToolResult{Success: false, Error: &err}
}

// ============================================================================
// CHANNEL TYPES
// ============================================================================

// ChannelMessage represents a message received from or sent to a channel.
type ChannelMessage struct {
	// ID is the unique message identifier.
	ID string `json:"id"`
	// Sender is the sender's identifier.
	Sender string `json:"sender"`
	// ReplyTarget is where replies should be sent.
	ReplyTarget string `json:"reply_target"`
	// Content is the message text.
	Content string `json:"content"`
	// Channel is the channel identifier.
	Channel string `json:"channel"`
	// Timestamp is the Unix timestamp of the message.
	Timestamp int64 `json:"timestamp"`
	// ThreadTS is the platform thread identifier for threaded replies.
	ThreadTS *string `json:"thread_ts,omitempty"`
}

// SendMessage represents a message to send through a channel.
type SendMessage struct {
	// Content is the message text.
	Content string `json:"content"`
	// Recipient is the target recipient.
	Recipient string `json:"recipient"`
	// Subject is an optional subject line.
	Subject *string `json:"subject,omitempty"`
	// ThreadTS is the thread identifier for threaded replies.
	ThreadTS *string `json:"thread_ts,omitempty"`
}

// NewSendMessage creates a new message with content and recipient.
func NewSendMessage(content, recipient string) SendMessage {
	return SendMessage{Content: content, Recipient: recipient}
}

// ============================================================================
// INTERFACES
// ============================================================================

// Memory is the core interface for memory persistence backends.
// Implement this interface to create custom memory storage (e.g., Supabase, Redis).
type Memory interface {
	// Name returns the backend name (e.g., "supabase", "sqlite").
	Name() string

	// Store saves a memory entry, optionally scoped to a session.
	Store(ctx context.Context, key, content string, category MemoryCategory, sessionID *string) error

	// StoreWithEmbedding saves a memory entry with a pre-computed embedding.
	StoreWithEmbedding(ctx context.Context, key, content string, category MemoryCategory, sessionID *string, embedding []float32) error

	// Recall retrieves memories matching a query using semantic similarity.
	Recall(ctx context.Context, query string, limit int, sessionID *string) ([]MemoryEntry, error)

	// RecallWithEmbedding retrieves memories using a pre-computed embedding vector.
	RecallWithEmbedding(ctx context.Context, embedding []float32, limit int, sessionID *string) ([]MemoryEntry, error)

	// Get retrieves a specific memory by key.
	Get(ctx context.Context, key string) (*MemoryEntry, error)

	// GetRecentHistory retrieves the most recent N conversation turns for a session.
	GetRecentHistory(ctx context.Context, sessionID *string, limit int) ([]MemoryEntry, error)

	// List returns all memory keys, optionally filtered by category and session.
	List(ctx context.Context, category *MemoryCategory, sessionID *string) ([]MemoryEntry, error)

	// Forget removes a memory by key.
	Forget(ctx context.Context, key string) (bool, error)

	// Count returns the total number of memories.
	Count(ctx context.Context) (int, error)

	// HealthCheck verifies the backend is healthy.
	HealthCheck(ctx context.Context) bool
}

// StreamCallback is a function that receives text chunks as they are generated.
type StreamCallback func(chunk string)

// Provider is the interface for LLM providers (e.g., OpenAI, Anthropic).
type Provider interface {
	// Name returns the provider name (e.g., "openai", "anthropic").
	Name() string

	// SimpleChat performs a one-shot chat without history.
	SimpleChat(ctx context.Context, message, model string, temperature float64) (string, error)

	// ChatWithSystem performs a one-shot chat with an optional system prompt.
	ChatWithSystem(ctx context.Context, systemPrompt, message, model string, temperature float64) (string, error)

	// ChatWithHistory performs a multi-turn conversation.
	ChatWithHistory(ctx context.Context, messages []ChatMessage, model string, temperature float64) (string, error)

	// Chat performs a structured chat for agent loop callers.
	Chat(ctx context.Context, messages []ChatMessage, tools []ToolSpec, model string, temperature float64) (*ChatResponse, error)

	// ChatStream performs a structured chat and streams text content to a callback.
	ChatStream(ctx context.Context, messages []ChatMessage, tools []ToolSpec, model string, temperature float64, callback StreamCallback) (*ChatResponse, error)

	// SupportsNativeTools returns true if the provider supports native tool calling.
	SupportsNativeTools() bool

	// SupportsVision returns true if the provider supports image inputs.
	SupportsVision() bool

	// GetEmbedding generates an embedding vector for the given text.
	GetEmbedding(ctx context.Context, text string) ([]float32, error)
}

// Channel is the interface for messaging platforms (e.g., Slack, Discord, Webhook).
type Channel interface {
	// Name returns the channel name (e.g., "webhook", "slack").
	Name() string

	// Send delivers a message through this channel.
	Send(ctx context.Context, message *SendMessage) error

	// SendTyping sends a typing indicator to the recipient.
	SendTyping(ctx context.Context, recipient string) error

	// HealthCheck verifies the channel is healthy.
	HealthCheck(ctx context.Context) bool
}

// Tool is the interface for executable tools that the AI can invoke.
type Tool interface {
	// Name returns the tool identifier.
	Name() string

	// Description returns a human-readable description.
	Description() string

	// ParametersSchema returns the JSON Schema for parameters.
	ParametersSchema() map[string]interface{}

	// Execute runs the tool with the given arguments.
	Execute(ctx context.Context, args map[string]interface{}) (*ToolResult, error)

	// Spec returns the full tool specification for LLM registration.
	Spec() ToolSpec
}

// EmbeddingService is the interface for embedding generation services.
type EmbeddingService interface {
	// GenerateEmbedding creates an embedding vector for the given text.
	GenerateEmbedding(ctx context.Context, text string) ([]float32, error)

	// GenerateEmbeddings creates embedding vectors for multiple texts.
	GenerateEmbeddings(ctx context.Context, texts []string) ([][]float32, error)

	// Dimension returns the dimensionality of the embeddings.
	Dimension() int
}

// ============================================================================
// CONFIG TYPES
// ============================================================================

// Config holds the application configuration from environment variables.
type Config struct {
	// Supabase
	SupabaseURL    string
	SupabaseKey    string
	SupabaseDBURL  string // Direct PostgreSQL connection URL

	// Provider
	ProviderAPIKey  string
	ProviderModel   string
	ProviderBaseURL string
	EmbeddingModel  string

	// Server
	Port string

	// Session
	DefaultSessionID string
}

// LoadConfig reads configuration from environment variables.
func LoadConfig() *Config {
	return &Config{
		SupabaseURL:      getEnv("SUPABASE_URL", ""),
		SupabaseKey:      getEnv("SUPABASE_KEY", ""),
		SupabaseDBURL:    getEnv("SUPABASE_DB_URL", ""),
		ProviderAPIKey:   getEnv("PROVIDER_API_KEY", ""),
		ProviderModel:    getEnv("PROVIDER_MODEL", "gpt-4o-mini"),
		ProviderBaseURL:  getEnv("PROVIDER_BASE_URL", "https://api.openai.com/v1"),
		EmbeddingModel:   getEnv("EMBEDDING_MODEL", "text-embedding-3-small"),
		Port:             getEnv("PORT", "8080"),
		DefaultSessionID: getEnv("DEFAULT_SESSION_ID", "default"),
	}
}

// getEnv retrieves an environment variable or returns the default value.
func getEnv(key, defaultValue string) string {
	if value := getEnvRaw(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvRaw retrieves an environment variable (to be implemented with os.Getenv).
func getEnvRaw(key string) string {
	// This will be replaced by os.Getenv in the actual implementation
	// We can't import os here to avoid circular dependencies
	return ""
}
