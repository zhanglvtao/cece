package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestAgentToolStartReturnsRunningStatusWithoutError(t *testing.T) {
	agentTool := NewAgent(&AgentHandler{
		RunSubAgent: func(ctx context.Context, config AgentSubAgentConfig, emitter Emitter) (AgentSubAgentResult, error) {
			return AgentSubAgentResult{AgentID: "agent-1", SessionID: "session-1", Status: "running", Content: "Agent agent-1 started asynchronously."}, nil
		},
	})
	input, _ := json.Marshal(map[string]any{"operation": "start", "prompt": "inspect code", "description": "inspect code"})
	result := agentTool.Run(context.Background(), input, nil)
	if result.IsError {
		t.Fatalf("IsError = true, content = %q", result.Content)
	}
	for _, want := range []string{"agent-1", "Status: running", "Session: session-1", "asynchronously"} {
		if !strings.Contains(result.Content, want) {
			t.Fatalf("content = %q, want substring %q", result.Content, want)
		}
	}
	if strings.Contains(result.Content, "Tokens:") {
		t.Fatalf("async start response should not include completion token summary: %q", result.Content)
	}
}

func TestAgentToolDescriptionMentionsAsyncControlPlane(t *testing.T) {
	agentTool := NewAgent(&AgentHandler{})
	desc := agentTool.Info().Description
	for _, want := range []string{"asynchronously", "status", "send", "answer", "confirm", "reject", "cancel"} {
		if !strings.Contains(desc, want) {
			t.Fatalf("description missing %q: %q", want, desc)
		}
	}
}
