package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/zhanglvtao/cece/internal/tool"
)

func TestIsRecoverableProviderError_CodebaseInvalidMessageIsNotRecoverable(t *testing.T) {
	err := errors.New("codebase api error: trae_permanent_error(invalid params): We're sorry, the param is invalid.; biz error: rpc error: code = ErrParamInvalid desc = invalid message, origin err = invalid message (code=4001)")

	if isRecoverableProviderError(err) {
		t.Fatal("expected codebase invalid message error to be non-recoverable")
	}
}

func TestIsRecoverableProviderError_CodebaseOrdinaryParamErrorIsRecoverable(t *testing.T) {
	err := errors.New("codebase api error: missing required field repo_name (code=4001)")

	if !isRecoverableProviderError(err) {
		t.Fatal("expected ordinary codebase parameter error to remain recoverable")
	}
}

type staticStreamClient struct {
	events []ApiStreamEvent
}

func (c staticStreamClient) Stream(context.Context, []Message, SystemPrompt, []tool.Definition, int) (<-chan ApiStreamEvent, error) {
	ch := make(chan ApiStreamEvent, len(c.events))
	for _, event := range c.events {
		ch <- event
	}
	close(ch)
	return ch, nil
}

func (c staticStreamClient) SetReasoningEffort(_ string) {}

func TestIsContextBudgetProviderError_AidenGeneric400(t *testing.T) {
	errMsg := `aiden api returned 400 Bad Request: {"error":{"message":"1210:API 调用参数有误，请检查文档。","code":"-4316"}}`
	if !isContextBudgetProviderError(errMsg) {
		t.Fatal("expected Aiden 1210/-4316 to be recognized as context budget provider error")
	}
}

func TestReserveBudgetUsesP95Underestimate(t *testing.T) {
	stats := NewUnderestimateStats(5)
	for _, sample := range []int{100, 200, 300, 400, 500} {
		stats.Record(sample)
	}

	budget := ComputeReserveBudget(ReserveBudgetInput{
		EstimatedInputTokens: 1000,
		RequestedMaxTokens:   4096,
		ModelMaxOutput:       8192,
		ContextWindow:        50000,
		ReserveRatio:         0.1,
		UnderestimateP95:     stats.P95(),
	})

	if budget.UnderestimateP95 != 500 {
		t.Fatalf("UnderestimateP95 = %d, want 500", budget.UnderestimateP95)
	}
	if budget.ReserveTokens != 20000 {
		t.Fatalf("ReserveTokens = %d, want 20000", budget.ReserveTokens)
	}
	if !budget.Fits {
		t.Fatal("expected budget to fit")
	}
}

func TestModelStreamerRecordsUnderestimateFromActualInputTokens(t *testing.T) {
	client := staticStreamClient{events: []ApiStreamEvent{
		{EventType: "message_start", InputTokens: 1800},
		{EventType: "message_delta", StopReason: "end_turn", OutputTokens: 1},
		{Done: true, EventType: "message_stop"},
	}}
	stats := NewUnderestimateStats(8)
	streamer := NewModelStreamer(client, tool.NewRegistry(), nil)
	streamer.SetUnderestimateStats(stats)

	_, err := streamer.Stream(context.Background(), ModelStreamRequest{
		Messages:      []Message{{Role: UserRole, Content: "hello"}},
		System:        SystemPrompt{},
		Reason:        "user",
		MaxTokens:     1024,
		ContextWindow: 32000,
		EstimatedInputTokens: 1200,
	}, make(chan Event, 8))
	if err != nil {
		t.Fatalf("Stream error = %v", err)
	}

	if got := stats.P95(); got != 600 {
		t.Fatalf("P95 = %d, want 600", got)
	}
}
