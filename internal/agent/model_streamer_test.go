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

func TestModelStreamerPreservesReasoningBlockWithFunctionCall(t *testing.T) {
	// Simulate a Responses API response: reasoning -> function_call
	streamer := NewModelStreamer(staticStreamClient{events: []ApiStreamEvent{
		{EventType: "message_start"},
		// Reasoning block
		{EventType: "content_block_start", Index: 0, IsThinking: true, ThinkingProviderID: "rs_abc123", ThinkingSummaryText: "Let me think", ThinkingEncryptedContent: "ENC_BLOB"},
		{EventType: "content_block_delta", Detail: "thinking_delta", Index: 0, ThinkingDelta: "I should read the file..."},
		{EventType: "content_block_stop", Index: 0, IsThinking: true, ThinkingProviderID: "rs_abc123", ThinkingSummaryText: "Let me think"},
		// Function call block
		{EventType: "content_block_start", Index: 1, ToolCallID: "call_1", ToolCallProviderID: "fc_1", ToolCallName: "Read"},
		{EventType: "content_block_delta", Detail: "input_json_delta", Index: 1, ToolCallInput: `{"file_path":"/tmp/x"}`},
		{EventType: "content_block_stop", Index: 1},
		{EventType: "message_delta", StopReason: "tool_use"},
		{Done: true},
	}}, nil, nil)

	resp, err := streamer.Stream(context.Background(), ModelStreamRequest{
		Messages: []Message{{Role: UserRole, Content: "read file"}},
	}, nil)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	// Must have 1 thinking block
	if len(resp.thinkingBlocks) != 1 {
		t.Fatalf("thinkingBlocks len = %d, want 1", len(resp.thinkingBlocks))
	}
	tb := resp.thinkingBlocks[0]
	if tb.Type != ApiThinkingContentType {
		t.Fatalf("thinking block type = %q, want thinking", tb.Type)
	}
	if tb.Thinking == nil {
		t.Fatal("thinking block Thinking is nil")
	}
	if tb.Thinking.ID != "rs_abc123" {
		t.Errorf("thinking ID = %q, want rs_abc123", tb.Thinking.ID)
	}
	if tb.Thinking.EncryptedContent != "ENC_BLOB" {
		t.Errorf("thinking EncryptedContent = %q, want ENC_BLOB", tb.Thinking.EncryptedContent)
	}
	if tb.Thinking.Text != "I should read the file..." {
		t.Errorf("thinking Text = %q, want 'I should read the file...'", tb.Thinking.Text)
	}

	// Must have 1 tool call
	if len(resp.toolCalls) != 1 {
		t.Fatalf("toolCalls len = %d, want 1", len(resp.toolCalls))
	}
	if resp.toolCalls[0].ProviderID != "fc_1" {
		t.Errorf("tool call ProviderID = %q, want fc_1", resp.toolCalls[0].ProviderID)
	}
}

// Test: reasoning_text.done closes the block before output_item.done.
// The stop event from reasoning_text.done must still save the thinking block.
func TestModelStreamerReasoningTextDoneClosesBeforeOutputItemDone(t *testing.T) {
	streamer := NewModelStreamer(staticStreamClient{events: []ApiStreamEvent{
		{EventType: "message_start"},
		// Reasoning starts via output_item.added
		{EventType: "content_block_start", Index: 0, IsThinking: true, ThinkingProviderID: "rs_xyz", ThinkingSummaryText: "thinking...", ThinkingEncryptedContent: "ENC"},
		// Reasoning text delta
		{EventType: "content_block_delta", Detail: "thinking_delta", Index: 0, ThinkingDelta: "hmm"},
		// reasoning_text.done closes the reasoning block
		{EventType: "content_block_stop", Index: 0, IsThinking: true, ThinkingProviderID: "rs_xyz", ThinkingSummaryText: "thinking..."},
		// Function call
		{EventType: "content_block_start", Index: 1, ToolCallID: "call_1", ToolCallProviderID: "fc_1", ToolCallName: "Read"},
		{EventType: "content_block_delta", Detail: "input_json_delta", Index: 1, ToolCallInput: `{}`},
		{EventType: "content_block_stop", Index: 1},
		// output_item.done for reasoning (arrives after reasoning_text.done)
		// This is a no-op in model_streamer since thinkingIndex is -1 now
		{EventType: "content_block_stop", Index: 0, IsThinking: true, ThinkingProviderID: "rs_xyz"},
		{EventType: "message_delta", StopReason: "tool_use"},
		{Done: true},
	}}, nil, nil)

	resp, err := streamer.Stream(context.Background(), ModelStreamRequest{
		Messages: []Message{{Role: UserRole, Content: "read"}},
	}, nil)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if len(resp.thinkingBlocks) != 1 {
		t.Fatalf("thinkingBlocks len = %d, want 1; thinking block was not saved", len(resp.thinkingBlocks))
	}
	if resp.thinkingBlocks[0].Thinking.ID != "rs_xyz" {
		t.Errorf("thinking ID = %q, want rs_xyz", resp.thinkingBlocks[0].Thinking.ID)
	}
}

// Test: reasoning block without any text delta (only summary).
// This can happen when the model only emits summary, not reasoning_text.
func TestModelStreamerReasoningWithOnlySummary(t *testing.T) {
	streamer := NewModelStreamer(staticStreamClient{events: []ApiStreamEvent{
		{EventType: "message_start"},
		// Reasoning with summary but no text delta
		{EventType: "content_block_start", Index: 0, IsThinking: true, ThinkingProviderID: "rs_summary_only", ThinkingSummaryText: "Brief thought", ThinkingEncryptedContent: "ENC_SUMMARY"},
		// No reasoning_text.delta events
		// Closed by output_item.done
		{EventType: "content_block_stop", Index: 0, IsThinking: true, ThinkingProviderID: "rs_summary_only", ThinkingSummaryText: "Brief thought"},
		// Function call
		{EventType: "content_block_start", Index: 1, ToolCallID: "call_1", ToolCallProviderID: "fc_1", ToolCallName: "Bash"},
		{EventType: "content_block_delta", Detail: "input_json_delta", Index: 1, ToolCallInput: `{}`},
		{EventType: "content_block_stop", Index: 1},
		{EventType: "message_delta", StopReason: "tool_use"},
		{Done: true},
	}}, nil, nil)

	resp, err := streamer.Stream(context.Background(), ModelStreamRequest{
		Messages: []Message{{Role: UserRole, Content: "run"}},
	}, nil)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if len(resp.thinkingBlocks) != 1 {
		t.Fatalf("thinkingBlocks len = %d, want 1", len(resp.thinkingBlocks))
	}
	tb := resp.thinkingBlocks[0].Thinking
	if tb.ID != "rs_summary_only" {
		t.Errorf("thinking ID = %q, want rs_summary_only", tb.ID)
	}
	if tb.EncryptedContent != "ENC_SUMMARY" {
		t.Errorf("thinking EncryptedContent = %q, want ENC_SUMMARY", tb.EncryptedContent)
	}
	if tb.SummaryText != "Brief thought" {
		t.Errorf("thinking SummaryText = %q, want 'Brief thought'", tb.SummaryText)
	}
}
