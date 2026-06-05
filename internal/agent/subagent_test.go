package agent

import (
	"context"
	"testing"

	"github.com/zhanglvtao/cece/internal/tool"
)

func TestSubAgentRunReportsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	sa := NewSubAgent(nil, tool.NewRegistry(), SubAgentConfig{Prompt: "do work"})
	result := sa.Run(ctx)

	if !result.Cancelled {
		t.Fatalf("Cancelled = false, want true; result=%#v", result)
	}
	if result.Err == "" {
		t.Fatalf("Err is empty, want cancellation error")
	}
}
