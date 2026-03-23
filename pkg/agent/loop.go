// Package agent implements the core agent loop for ZeroClaw.
// Designed for serverless environments with strict timeout constraints (Vercel: 10-60s).
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/zeroclaw/zeroclaw-go/pkg/core"
)

// ============================================================================
// CONFIGURATION - STRICT LIMITS FOR SERVERLESS
// ============================================================================

const (
	// MaxToolIterations is the maximum tool-calling iterations.
	// Set to 5 to allow multi-step tasks (memory + multiple searches)
	// while staying within Vercel's 50s timeout (~5s per iteration).
	MaxToolIterations = 5

	// DefaultTimeout is the default timeout for the entire loop.
	DefaultTimeout = 45 * time.Second

	// MinMessageCharsForMemory is minimum length to store in memory.
	// Reduced to 1 to ensure short conversational queries (e.g., "suma 3+3") are retained.
	MinMessageCharsForMemory = 1

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
func (a *Agent) Run(ctx context.Context, message string, streamCallback core.StreamCallback) (*Result, error) {
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

	// STEP 2: Retrieve recent history (matches MaxHistoryMessages to avoid wasting network)
	var recentHistory []core.MemoryEntry
	if a.memory != nil && a.config.SessionID != "" {
		history, err := a.memory.GetRecentHistory(ctx, &a.config.SessionID, MaxHistoryMessages)
		if err != nil {
			fmt.Printf("[agent] history retrieval error: %v\n", err)
		} else {
			recentHistory = history
			log.Printf("DEBUG: Found %d recent history messages", len(recentHistory))
		}
	}

	// STEP 3: Build system prompt
	systemPrompt := a.buildPrompt(memories)

	// STEP 4: Run tool-calling loop
	response, iterations, toolCalls, err := a.runLoop(ctx, systemPrompt, message, recentHistory, streamCallback)
	if err != nil {
		return nil, fmt.Errorf("agent loop failed: %w", err)
	}

	result.Response = response
	result.Iterations = iterations
	result.ToolCalls = toolCalls

	// STEP 5: Store conversation (synchronous - required for serverless)
	if len(message) >= MinMessageCharsForMemory {
		a.storeConversation(message, response)
	}

	return result, nil
}

// ============================================================================
// TOOL LOOP
// ============================================================================

// runLoop executes the iterative tool-calling loop.
func (a *Agent) runLoop(ctx context.Context, systemPrompt, userMessage string, history []core.MemoryEntry, streamCallback core.StreamCallback) (string, int, []string, error) {
	// Build initial messages
	messages := []core.ChatMessage{
		core.NewSystemMessage(systemPrompt),
	}

	// Add recent history messages
	for _, mem := range history {
		if strings.HasSuffix(mem.Key, "_resp") {
			messages = append(messages, core.NewAssistantMessage(mem.Content))
		} else {
			messages = append(messages, core.NewUserMessage(mem.Content))
		}
	}

	// Add the current user message
	messages = append(messages, core.NewUserMessage(userMessage))

	// Get tool specs for provider
	toolSpecs := a.getToolSpecs()

	iterations := 0
	toolCallsMade := []string{}
	var lastResponse string

	for iterations < a.config.MaxIterations {
		iterations++

		// Call provider. Use streamCallback only if we are taking the first response or if there are no tools? 
		// Actually, we can just pass the streamCallback always. If it's a tool call, streamCallback receives nothing (because delta.content is empty) 
		// or it receives the "thought process" which is good!
		resp, err := a.provider.ChatStream(ctx, messages, toolSpecs, a.config.Model, a.config.Temperature, streamCallback)
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

		// Add assistant message to history.
		// Sanitize the text to keep the LLM's reasoning (e.g. "I'll search for...")
		// but strip any leaked tool call XML/JSON artifacts.
		// We must NOT discard the text entirely or the LLM loses context about
		// why it called the tools and can't connect results to the user's question.
		assistantContent := sanitizeResponse(resp.TextOrEmpty())
		assistantMsg := core.NewAssistantMessage(assistantContent)
		assistantMsg.ToolCalls = resp.ToolCalls
		messages = append(messages, assistantMsg)

		// Add tool results to history
		for i, result := range toolResults {
			toolCallID := ""
			if i < len(resp.ToolCalls) {
				toolCallID = resp.ToolCalls[i].ID
			}
			
			messages = append(messages, core.ChatMessage{
				Role:       "tool",
				Content:    result,
				ToolCallID: toolCallID,
			})
		}

		// Truncate history if too long
		messages = a.truncateHistory(messages)
	}

	// If we hit max iterations without final response, get one more
	if lastResponse == "" && len(messages) > 0 {
		// Add explicit instruction to NOT use tools in the final call
		messages = append(messages, core.NewUserMessage(
			"Please provide your final answer now based on the tool results above. Do NOT call any more tools. Respond directly in natural language."))
		finalResp, err := a.provider.ChatStream(ctx, messages, nil, a.config.Model, a.config.Temperature, streamCallback)
		if err != nil {
			return "", iterations, toolCallsMade, fmt.Errorf("final provider error: %w", err)
		}
		lastResponse = finalResp.TextOrEmpty()
	}

	// Sanitize: strip any leaked tool call artifacts from the final response
	lastResponse = sanitizeResponse(lastResponse)

	// Safety net: never return an empty response (Telegram will reject it)
	if lastResponse == "" {
		lastResponse = "He procesado tu solicitud pero no pude generar una respuesta clara. ¿Podrías reformular tu pregunta?"
	}

	return lastResponse, iterations, toolCallsMade, nil
}

// executeTools runs all requested tools concurrently to minimize latency.
func (a *Agent) executeTools(ctx context.Context, toolCalls []core.ToolCall) ([]string, []string) {
	start := time.Now()
	
	results := make([]string, len(toolCalls))
	calledTools := make([]string, len(toolCalls))

	var wg sync.WaitGroup
	var mu sync.Mutex // Ensure thread-safe writes to the results slice

	for i, tc := range toolCalls {
		calledTools[i] = tc.Name

		wg.Add(1)
		go func(idx int, call core.ToolCall) {
			defer wg.Done()

			var finalOutput string

			// Find the tool
			tool, exists := a.tools[call.Name]
			if !exists {
				finalOutput = fmt.Sprintf("Error: Unknown tool '%s'", call.Name)
			} else {
				// Parse arguments
				var args map[string]interface{}
				var parseErr error
				if call.Arguments != "" {
					if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
						parseErr = err
					}
				}

				if parseErr != nil {
					finalOutput = fmt.Sprintf("Error: Invalid arguments: %v", parseErr)
				} else {
					// Execute tool
					result, err := tool.Execute(ctx, args)
					if err != nil {
						finalOutput = fmt.Sprintf("Error: %v", err)
					} else {
						// Format output
						output := result.Output
						if !result.Success && result.Error != nil {
							output = fmt.Sprintf("Error: %s", *result.Error)
						}

						// Truncate if too long
						if len(output) > MaxToolOutputChars {
							output = output[:MaxToolOutputChars] + "...[truncated]"
						}

						finalOutput = output
					}
				}
			}

			// Safely write to the results slice using Mutex
			mu.Lock()
			results[idx] = finalOutput
			mu.Unlock()

		}(i, tc)
	}

	wg.Wait() // CRITICAL: Wait for all goroutines to finish before returning to the synchronous Vercel thread.

	if len(toolCalls) > 0 {
		log.Printf("ZeroClaw: Parallel execution of %d tools took %v", len(toolCalls), time.Since(start))
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
	memories, err := a.memory.Recall(ctx, query, 10, &sessionID)
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
	if err := a.memory.Store(ctx, userKey, userMessage, core.MemoryCategoryConversation, &sessionID); err != nil {
		log.Printf("[agent] WARNING - failed to store user memory: %v", err)
	}

	// Store assistant response
	assistantKey := fmt.Sprintf("conv_%s_%d_resp", sessionID, timestamp)
	if err := a.memory.Store(ctx, assistantKey, assistantResponse, core.MemoryCategoryConversation, &sessionID); err != nil {
		log.Printf("[agent] WARNING - failed to store assistant memory: %v", err)
	}
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

// sanitizeResponse strips any leaked tool call artifacts from the final response.
// Qwen/Cerebras models sometimes include raw tool call XML/JSON in their text output.
func sanitizeResponse(text string) string {
	// Remove <tool_call>...</tool_call> blocks
	toolCallPattern := regexp.MustCompile(`(?s)<tool_call>.*?</tool_call>`)
	text = toolCallPattern.ReplaceAllString(text, "")

	// Remove [Tool calls made] markers
	text = strings.ReplaceAll(text, "[Tool calls made]", "")

	// Remove {"tool": ...} JSON blocks that leaked into text
	jsonToolPattern := regexp.MustCompile(`(?s)\{"tool"\s*:\s*"[^"]+"\s*,\s*"arguments"\s*:\s*\{.*?\}\}`)
	text = jsonToolPattern.ReplaceAllString(text, "")

	// Remove {"name": ...} tool call blocks
	jsonNamePattern := regexp.MustCompile(`(?s)\{"name"\s*:\s*"[^"]+"\s*,\s*"arguments"\s*:\s*\{.*?\}\}`)
	text = jsonNamePattern.ReplaceAllString(text, "")

	// Clean up excessive whitespace left behind
	text = regexp.MustCompile(`\n{3,}`).ReplaceAllString(text, "\n\n")
	text = strings.TrimSpace(text)

	return text
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
	return agent.Run(ctx, message, nil)
}
