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
	input, _ := json.Marshal(map[string]any{"operation": "start", "prompt": "inspect code", "description": "inspect code", "agent_type": "research"})
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

func TestAgentToolModelSchemaExposesChoices(t *testing.T) {
	agentTool := NewAgent(&AgentHandler{}, WithAgentModels([]string{"glm-5.1", "", "gpt-5.5", "glm-5.1"}))
	info := agentTool.Info()
	props := info.InputSchema["properties"].(map[string]any)
	model := props["model"].(map[string]any)
	enum := model["enum"].([]string)
	if len(enum) != 2 || enum[0] != "glm-5.1" || enum[1] != "gpt-5.5" {
		t.Fatalf("model enum = %#v", enum)
	}
	if !strings.Contains(model["description"].(string), "Optional") {
		t.Fatalf("model description = %q, want optional hint", model["description"])
	}
}

func TestAgentToolModelSchemaUsesProvider(t *testing.T) {
	agentTool := NewAgent(&AgentHandler{}, WithAgentModelProvider(func() []string {
		return []string{"deepseek-v4-pro"}
	}))
	props := agentTool.Info().InputSchema["properties"].(map[string]any)
	model := props["model"].(map[string]any)
	enum := model["enum"].([]string)
	if len(enum) != 1 || enum[0] != "deepseek-v4-pro" {
		t.Fatalf("model enum = %#v", enum)
	}
	agentType := props["agent_type"].(map[string]any)
	types := agentType["enum"].([]string)
	want := []string{"research", "coding", "review", "execution"}
	if len(types) != len(want) {
		t.Fatalf("agent_type enum = %#v", types)
	}
	for i, v := range want {
		if types[i] != v {
			t.Fatalf("agent_type enum[%d] = %q, want %q", i, types[i], v)
		}
	}
}

func TestAgentToolRequiresAgentTypeForStart(t *testing.T) {
	agentTool := NewAgent(&AgentHandler{
		RunSubAgent: func(ctx context.Context, config AgentSubAgentConfig, emitter Emitter) (AgentSubAgentResult, error) {
			return AgentSubAgentResult{}, nil
		},
	})
	input, _ := json.Marshal(map[string]any{"operation": "start", "prompt": "inspect code"})
	result := agentTool.Run(context.Background(), input, nil)
	if !result.IsError {
		t.Fatalf("IsError = false, content = %q", result.Content)
	}
	if !strings.Contains(result.Content, "agent_type is required") {
		t.Fatalf("content = %q, want agent_type required error", result.Content)
	}
}

func TestAgentToolPassesWaitTimeout(t *testing.T) {
	var got AgentSubAgentConfig
	agentTool := NewAgent(&AgentHandler{
		RunSubAgent: func(ctx context.Context, config AgentSubAgentConfig, emitter Emitter) (AgentSubAgentResult, error) {
			got = config
			return AgentSubAgentResult{AgentID: config.AgentID, Status: "running", Content: "waiting"}, nil
		},
	})
	input, _ := json.Marshal(map[string]any{"operation": "wait", "agent_id": "agent-1", "timeout_ms": 1234})
	result := agentTool.Run(context.Background(), input, nil)
	if result.IsError {
		t.Fatalf("IsError = true, content = %q", result.Content)
	}
	if got.Operation != "wait" || got.AgentID != "agent-1" || got.TimeoutMS != 1234 {
		t.Fatalf("config = %+v, want wait agent-1 timeout 1234", got)
	}
}

func TestAgentToolDescriptionMentionsAsyncControlPlane(t *testing.T) {
	agentTool := NewAgent(&AgentHandler{})
	desc := agentTool.Info().Description
	for _, want := range []string{
		"independent subtasks",
		"parallelizable",
		"asynchronously",
		"spawned agent",
		"spawning agent's inbox",
		"Do not proactively poll",
		"status",
		"wait",
		"send",
		"answer",
		"confirm",
		"reject",
		"cancel",
	} {
		if !strings.Contains(desc, want) {
			t.Fatalf("description missing %q: %q", want, desc)
		}
	}
}
