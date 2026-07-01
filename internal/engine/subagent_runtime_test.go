package engine

import (
	"context"
	"testing"

	"github.com/zhanglvtao/cece/internal/protocol"
)

func TestAgentRuntimeAssistantDeltaEmitsThrottledProgress(t *testing.T) {
	rt := NewAgentRuntime("agent-1", "A", "worker-model", "parent-session", nil, nil, context.Background(), func() {}, 8)

	msg, ok := rt.handleEvent(protocol.AssistantDelta{Text: "hello"})
	if !ok {
		t.Fatal("first assistant delta should emit progress")
	}
	if msg.Kind != AgentMessageProgress || msg.Status != AgentStatusRunning {
		t.Fatalf("message = %+v, want running progress", msg)
	}
	if snap, ok := msg.Payload.(AgentRuntimeSnapshot); !ok || snap.LastMessage != "hello" {
		t.Fatalf("payload = %+v, want snapshot with LastMessage=hello", msg.Payload)
	}

	if _, ok := rt.handleEvent(protocol.AssistantDelta{Text: " world"}); ok {
		t.Fatal("immediate assistant delta should be throttled")
	}
}
