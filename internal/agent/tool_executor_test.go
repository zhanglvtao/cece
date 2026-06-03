package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"cece/internal/tool"
)

type staticTestTool struct{}

func (staticTestTool) Info() tool.Definition {
	return tool.Definition{
		Name:        "Static",
		Description: "static test tool",
		InputSchema: map[string]any{"type": "object"},
	}
}

func (staticTestTool) Run(ctx context.Context, input json.RawMessage, emitter tool.Emitter) tool.Result {
	if emitter != nil {
		emitter.Emit("progress\n")
	}
	return tool.Result{Content: "ok"}
}

func TestToolExecutorExecuteBatchAllowsNilEventChannel(t *testing.T) {
	registry := tool.NewRegistry()
	registry.Register(staticTestTool{})
	executor := NewToolExecutor(registry, nil, nil, ToolResultPolicy{}, nil)

	done := make(chan []ApiContentBlock, 1)
	go func() {
		done <- executor.ExecuteBatch(context.Background(), []ApiToolUseBlock{{
			ID:    "tool-1",
			Name:  "Static",
			Input: json.RawMessage(`{}`),
		}}, nil)
	}()

	select {
	case blocks := <-done:
		if len(blocks) != 1 {
			t.Fatalf("len(blocks) = %d, want 1", len(blocks))
		}
		if blocks[0].ToolResult == nil || blocks[0].ToolResult.Content != "ok" {
			t.Fatalf("tool result = %#v, want ok", blocks[0].ToolResult)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("ExecuteBatch blocked with nil event channel")
	}
}
