// Package agent implements the core agent loop for ZeroClaw.
// Designed for serverless environments with strict timeout constraints (Vercel: 10-60s).
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/zeroclaw/zeroclaw-go/pkg/core"
)

// ============================================================================
// CONFIGURATION - STRICT LIMITS FOR SERVERLESS
// ============================================================================

const (
	// MaxToolIterations is the maximum tool-calling iterations.
	// Set to 2 to avoid Vercel timeouts (10-60s).
	MaxToolIterations = 2

	// DefaultTimeout is the default timeout for the entire loop.
	DefaultTimeout = 45 * time.Second

	// MinMessageCharsForMemory is minimum length to store in memory.
	MinMessageCharsForMemory = 20

	// MaxToolOutputChars limits tool output to prevent context overflow.
	MaxToolOutputChars = 2000

	// MaxHistoryMessages limits conversation history.
	MaxHistoryMessages = 10
)

// Config holds agent configuration.
type Config struct {
	MaxIterations int
	Timeout       time.Duration
	Temperature   float64
	Model         string
	SessionID     string
}

// DefaultConfig returns sensible defaults for serverless.
func DefaultConfig() *Config {
	return &Config{
		MaxIterations: MaxToolIterations,
		Timeout:       DefaultTimeout,
		Temperature:   0.7,
		Model:         "gpt-4o-mini",
		SessionID:     "default",
	}
}

// ============================================================================
// RESULT
// ============================================================================

// Result contains the agent's response.
type Result struct {
	Response     string
	Iterations   int
	MemoriesFound int
	SessionID    string
	ToolCalls    []string // Names of tools called
}

// ============================================================================
// AGENT
// ============================================================================

// Agent orchestrates the conversation flow.
type Agent struct {
	memory   core.Memory
	provider core.Provider
	tools    map[string]core.Tool
	config   *Config
	identity *core.IdentityConfig
}

// NewAgent creates a new agent with dependencies.
func NewAgent(
	memory core.Memory,
	provider core.Provider,
	tools []core.Tool,
	config *Config,
	identity *core.IdentityConfig,
) *Agent {
	if config == nil {
		config = DefaultConfig()
	}
	if identity == nil {
		identity = core.LoadIdentityConfig()
	}

	toolMap := make(map[string]core.Tool)
	for _, t := range tools {
		toolMap[t.Name()] = t
	}

	return &Agent{
		memory:   memory,
		provider: provider,
		tools:    toolMap,
		config:   config,
		identity: identity,
	}
}

// Run executes the agent loop for a user message.
// This is the main entry point.
func (a *Agent) Run(ctx context.Context, message string) (*Result, error) {
	// Create timeout context
	ctx, cancel := context.WithTimeout(ctx, a.config.Timeout)
	defer cancel()

	result := &Result{
		SessionID: a.config.SessionID,
	}

	// STEP 1: Retrieve memory context
	memories, err := a.recallMemory(ctx, message)
	if err != nil {
		// Log but continue without context
		fmt.Printf("[agent] memory recall error: %v\n", err)
	}
	result.MemoriesFound = len(memories)
	
	// Debug log for memory injection
	log.Printf("DEBUG: Injecting %d memories into prompt context", len(memories))

	// STEP 2: Build system prompt
	systemPrompt := a.buildPrompt(memories)

	// STEP 3: Run tool-calling loop
	response, iterations, toolCalls, err := a.runLoop(ctx, systemPrompt, message)
	if err != nil {
		return nil, fmt.Errorf("agent loop failed: %w", err)
	}

	result.Response = response
	result.Iterations = iterations
	result.ToolCalls = toolCalls

	// STEP 4: Store conversation (synchronous - required for serverless)
	if len(message) >= MinMessageCharsForMemory {
		a.storeConversation(message, response)
	}

	return result, nil
}

// ============================================================================
// TOOL LOOP
// ============================================================================

// runLoop executes the iterative tool-calling loop.
func (a *Agent) runLoop(ctx context.Context, systemPrompt, userMessage string) (string, int, []string, error) {
	// Build initial messages
	messages := []core.ChatMessage{
		core.NewSystemMessage(systemPrompt),
		core.NewUserMessage(userMessage),
	}

	// Get tool specs for provider
	toolSpecs := a.getToolSpecs()

	iterations := 0
	toolCallsMade := []string{}
	var lastResponse string

	for iterations < a.config.MaxIterations {
		iterations++

		// Call provider
		resp, err := a.provider.Chat(ctx, messages, toolSpecs, a.config.Model, a.config.Temperature)
		if err != nil {
			return "", iterations, toolCallsMade, fmt.Errorf("provider error: %w", err)
		}

		// Check for tool calls
		if !resp.HasToolCalls() {
			// No tools - we're done
			lastResponse = resp.TextOrEmpty()
			break
		}

		// Process tool calls
		toolResults, calledTools := a.executeTools(ctx, resp.ToolCalls)
		toolCallsMade = append(toolCallsMade, calledTools...)

		// Add assistant message to history
		assistantContent := resp.TextOrEmpty()
		if len(resp.ToolCalls) > 0 {
			assistantContent += "\n\n[Tool calls made]"
		}
		messages = append(messages, core.NewAssistantMessage(assistantContent))

		// Add tool results to history
		for _, result := range toolResults {
			messages = append(messages, core.ChatMessage{
				Role:    "tool",
				Content: result,
			})
		}

		// Truncate history if too long
		messages = a.truncateHistory(messages)
	}

	// If we hit max iterations without final response, get one more
	if lastResponse == "" && len(messages) > 0 {
		finalResp, err := a.provider.Chat(ctx, messages, nil, a.config.Model, a.config.Temperature)
		if err != nil {
			return "", iterations, toolCallsMade, fmt.Errorf("final provider error: %w", err)
		}
		lastResponse = finalResp.TextOrEmpty()
	}

	return lastResponse, iterations, toolCallsMade, nil
}

// executeTools runs all requested tools.
func (a *Agent) executeTools(ctx context.Context, toolCalls []core.ToolCall) ([]string, []string) {
	results := make([]string, 0, len(toolCalls))
	calledTools := make([]string, 0, len(toolCalls))

	for _, tc := range toolCalls {
		calledTools = append(calledTools, tc.Name)

		// Find the tool
		tool, exists := a.tools[tc.Name]
		if !exists {
			results = append(results, fmt.Sprintf("Error: Unknown tool '%s'", tc.Name))
			continue
		}

		// Parse arguments
		var args map[string]interface{}
		if tc.Arguments != "" {
			if err := json.Unmarshal([]byte(tc.Arguments), &args); err != nil {
				results = append(results, fmt.Sprintf("Error: Invalid arguments: %v", err))
				continue
			}
		}

		// Execute tool
		result, err := tool.Execute(ctx, args)
		if err != nil {
			results = append(results, fmt.Sprintf("Error: %v", err))
			continue
		}

		// Format output
		output := result.Output
		if !result.Success && result.Error != nil {
			output = fmt.Sprintf("Error: %s", *result.Error)
		}

		// Truncate if too long
		if len(output) > MaxToolOutputChars {
			output = output[:MaxToolOutputChars] + "...[truncated]"
		}

		results = append(results, output)
	}

	return results, calledTools
}

// ============================================================================
// MEMORY OPERATIONS
// ============================================================================

// recallMemory fetches relevant context from memory.
func (a *Agent) recallMemory(ctx context.Context, query string) ([]core.MemoryEntry, error) {
	if a.memory == nil {
		return nil, nil
	}

	sessionID := a.config.SessionID
	memories, err := a.memory.Recall(ctx, query, 5, &sessionID)
	if err != nil {
		return nil, err
	}

	return memories, nil
}

// storeConversation saves to memory asynchronously.
func (a *Agent) storeConversation(userMessage, assistantResponse string) {
	if a.memory == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sessionID := a.config.SessionID
	timestamp := time.Now().UnixNano()

	// Store user message
	userKey := fmt.Sprintf("conv_%s_%d", sessionID, timestamp)
	a.memory.Store(ctx, userKey, userMessage, core.MemoryCategoryConversation, &sessionID)

	// Store assistant response
	assistantKey := fmt.Sprintf("conv_%s_%d_resp", sessionID, timestamp)
	a.memory.Store(ctx, assistantKey, assistantResponse, core.MemoryCategoryConversation, &sessionID)
}

// ============================================================================
// PROMPT BUILDING
// ============================================================================

// buildPrompt constructs the system prompt.
func (a *Agent) buildPrompt(memories []core.MemoryEntry) string {
	return core.NewSystemPromptBuilder(a.identity).
		WithTools(a.getToolSpecs()).
		WithMemories(memories).
		Build()
}

// getToolSpecs returns specs for all registered tools.
func (a *Agent) getToolSpecs() []core.ToolSpec {
	specs := make([]core.ToolSpec, 0, len(a.tools))
	for _, tool := range a.tools {
		specs = append(specs, tool.Spec())
	}
	return specs
}

// truncateHistory limits conversation history.
func (a *Agent) truncateHistory(messages []core.ChatMessage) []core.ChatMessage {
	if len(messages) <= MaxHistoryMessages {
		return messages
	}

	// Keep system message + recent messages
	systemMsg := messages[0]
	recent := messages[len(messages)-MaxHistoryMessages+1:]

	result := make([]core.ChatMessage, 0, MaxHistoryMessages)
	result = append(result, systemMsg)
	result = append(result, recent...)

	return result
}

// ============================================================================
// CREDENTIAL SCRUBBING
// ============================================================================

// sensitivePatterns matches sensitive data patterns.
var sensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(api[_-]?key|apikey)["']?\s*[:=]\s*["']?[a-zA-Z0-9_\-]{20,}`),
	regexp.MustCompile(`(?i)bearer\s+[a-zA-Z0-9_\-\.]{20,}`),
	regexp.MustCompile(`(?i)(password|passwd)["']?\s*[:=]\s*["']?[^\s"']{8,}`),
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	regexp.MustCompile(`sk-[a-zA-Z0-9]{48}`),
	regexp.MustCompile(`-----BEGIN.*PRIVATE KEY-----`),
}

// ScrubCredentials removes sensitive data from text.
func ScrubCredentials(text string) string {
	for _, pattern := range sensitivePatterns {
		text = pattern.ReplaceAllString(text, "[REDACTED]")
	}
	return text
}

// ============================================================================
// CONVENIENCE FUNCTION
// ============================================================================

// Run is a convenience function for one-shot execution.
func Run(
	ctx context.Context,
	message string,
	memory core.Memory,
	provider core.Provider,
	tools []core.Tool,
	config *Config,
) (*Result, error) {
	agent := NewAgent(memory, provider, tools, config, nil)
	return agent.Run(ctx, message)
}
