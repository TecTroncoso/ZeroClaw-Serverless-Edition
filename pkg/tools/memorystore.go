package tools

import (
	"context"
	"fmt"

	"github.com/zeroclaw/zeroclaw-go/pkg/core"
)

// MemoryStoreTool allows the agent to explicitly save core memories.
type MemoryStoreTool struct {
	memory  core.Memory
	session *string
}

// NewMemoryStoreTool creates a new memory store tool.
func NewMemoryStoreTool(memory core.Memory, session *string) *MemoryStoreTool {
	return &MemoryStoreTool{memory: memory, session: session}
}

func (t *MemoryStoreTool) Name() string {
	return "core_memory_save"
}

func (t *MemoryStoreTool) Description() string {
	return "Saves an important fact, user preference, or key information to long-term core memory."
}

func (t *MemoryStoreTool) ParametersSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"key": map[string]interface{}{
				"type":        "string",
				"description": "A unique identifier for this memory (e.g., 'user_name', 'favorite_color'). Use underscores.",
			},
			"value": map[string]interface{}{
				"type":        "string",
				"description": "The actual fact or information to remember.",
			},
		},
		"required": []string{"key", "value"},
	}
}

func (t *MemoryStoreTool) Execute(ctx context.Context, args map[string]interface{}) (*core.ToolResult, error) {
	if t.memory == nil {
		return core.NewErrorResult("Memory backend is not initialized"), nil
	}

	key, ok := args["key"].(string)
	if !ok || key == "" {
		return core.NewErrorResult("Missing or invalid 'key' argument"), nil
	}

	value, ok := args["value"].(string)
	if !ok || value == "" {
		return core.NewErrorResult("Missing or invalid 'value' argument"), nil
	}

	err := t.memory.Store(ctx, key, value, core.MemoryCategoryCore, t.session)
	if err != nil {
		return core.NewErrorResult(fmt.Sprintf("Failed to store memory: %v", err)), nil
	}

	return core.NewSuccessResult(fmt.Sprintf("Memory correctly saved with key: %s", key)), nil
}

func (t *MemoryStoreTool) Spec() core.ToolSpec {
	return core.ToolSpec{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters:  t.ParametersSchema(),
	}
}
