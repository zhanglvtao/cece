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

func TestModelStreamerPreservesToolCallProviderID(t *testing.T) {
	streamer := NewModelStreamer(staticStreamClient{events: []ApiStreamEvent{
		{EventType: "message_start"},
		{EventType: "content_block_start", Index: 0, ToolCallID: "call_1", ToolCallProviderID: "fc_1", ToolCallName: "Read"},
		{EventType: "content_block_delta", Detail: "input_json_delta", Index: 0, ToolCallInput: `{"file_path":"/tmp/x"}`},
		{EventType: "content_block_stop", Index: 0},
		{EventType: "message_delta", StopReason: "tool_use"},
		{Done: true},
	}}, nil, nil)

	resp, err := streamer.Stream(context.Background(), ModelStreamRequest{
		Messages: []Message{{Role: UserRole, Content: "read file"}},
	}, nil)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if len(resp.toolCalls) != 1 {
		t.Fatalf("toolCalls len = %d, want 1", len(resp.toolCalls))
	}
	if resp.toolCalls[0].ProviderID != "fc_1" {
		t.Fatalf("provider_id = %q, want fc_1", resp.toolCalls[0].ProviderID)
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
