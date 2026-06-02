package agent

import (
	"encoding/json"
	"testing"

	"cece/internal/prompt"
)

func TestAssembleResultToSystemPrompt(t *testing.T) {
	result := prompt.AssembleResult{
		Segments: []prompt.PromptSegment{
			{Content: "stable text", Layer: prompt.ContextStable},
			{Content: "", Layer: prompt.ContextSession},
			{Content: "session text", Layer: prompt.ContextSession},
			{Content: "turn text", Layer: prompt.ContextTurn},
		},
	}

	sp := AssembleResultToSystemPrompt(result)
	if len(sp.Blocks) != 3 {
		t.Fatalf("AssembleResultToSystemPrompt() returned %d blocks, want 3", len(sp.Blocks))
	}

	if sp.Blocks[0].Text != "stable text" {
		t.Errorf("block[0].Text = %q, want %q", sp.Blocks[0].Text, "stable text")
	}
	if sp.Blocks[0].CacheControl == nil || sp.Blocks[0].CacheControl["type"] != "ephemeral" {
		t.Errorf("block[0].CacheControl = %v, want ephemeral", sp.Blocks[0].CacheControl)
	}

	if sp.Blocks[1].Text != "session text" {
		t.Errorf("block[1].Text = %q, want %q", sp.Blocks[1].Text, "session text")
	}
	if sp.Blocks[1].CacheControl == nil || sp.Blocks[1].CacheControl["type"] != "ephemeral" {
		t.Errorf("block[1].CacheControl = %v, want ephemeral", sp.Blocks[1].CacheControl)
	}

	if sp.Blocks[2].Text != "turn text" {
		t.Errorf("block[2].Text = %q, want %q", sp.Blocks[2].Text, "turn text")
	}
	if sp.Blocks[2].CacheControl != nil {
		t.Errorf("block[2].CacheControl = %v, want nil", sp.Blocks[2].CacheControl)
	}
}

func TestAssembleResultToSystemPromptEmpty(t *testing.T) {
	result := prompt.AssembleResult{}
	sp := AssembleResultToSystemPrompt(result)
	if len(sp.Blocks) != 0 {
		t.Errorf("empty AssembleResult should produce 0 blocks, got %d", len(sp.Blocks))
	}
}

func TestProjectMessagesForRequestStripsAssistantThinkingBlocks(t *testing.T) {
	messages := []Message{
		{
			Role:    AssistantRole,
			Content: "Visible answer.",
			ContentBlocks: []ApiContentBlock{
				{
					Type: ApiThinkingContentType,
					Thinking: &ApiThinkingBlock{
						Text:      "let me think",
						Signature: "sig_visible",
					},
				},
				{
					Type: ApiRedactedThinkingContentType,
					Thinking: &ApiThinkingBlock{
						Signature: "sig_redacted",
					},
				},
				{Type: ApiTextContentType, Text: "Visible answer."},
				{
					Type: ApiToolUseContentType,
					ToolUse: &ApiToolUseBlock{
						ID:    "call_1",
						Name:  "Read",
						Input: json.RawMessage(`{"file_path":"/tmp/x"}`),
					},
				},
			},
		},
	}

	projected := ProjectMessagesForRequest(messages)
	if len(projected) != 1 {
		t.Fatalf("projected len = %d, want 1", len(projected))
	}
	if projected[0].Content != "Visible answer." {
		t.Fatalf("projected content = %q, want visible fallback", projected[0].Content)
	}
	if len(projected[0].ContentBlocks) != 2 {
		t.Fatalf("projected content blocks = %d, want 2", len(projected[0].ContentBlocks))
	}
	if projected[0].ContentBlocks[0].Type != ApiTextContentType {
		t.Fatalf("first block type = %q, want %q", projected[0].ContentBlocks[0].Type, ApiTextContentType)
	}
	if projected[0].ContentBlocks[1].Type != ApiToolUseContentType {
		t.Fatalf("second block type = %q, want %q", projected[0].ContentBlocks[1].Type, ApiToolUseContentType)
	}
	if got := len(messages[0].ContentBlocks); got != 4 {
		t.Fatalf("original content blocks mutated to %d, want 4", got)
	}
}

func TestEnsureToolResultCoverage_NoOrphans(t *testing.T) {
	msgs := []Message{
		{Role: UserRole, Content: "hello"},
		{Role: AssistantRole, Content: "hi"},
	}
	result := EnsureToolResultCoverage(msgs)
	if len(result) != len(msgs) {
		t.Fatalf("expected %d messages, got %d", len(msgs), len(result))
	}
}

func TestEnsureToolResultCoverage_WithOrphans(t *testing.T) {
	msgs := []Message{
		{Role: UserRole, Content: "run ls"},
		{
			Role: AssistantRole,
			ContentBlocks: []ApiContentBlock{
				{Type: ApiToolUseContentType, ToolUse: &ApiToolUseBlock{ID: "call_1", Name: "Bash", Input: json.RawMessage(`{"cmd":"ls"}`)}},
			},
		},
	}

	result := EnsureToolResultCoverage(msgs)
	if len(result) != 3 {
		t.Fatalf("expected 3 messages (original + synthetic), got %d", len(result))
	}
	// Synthetic tool_result must be at index 1 (immediately after the assistant message at index 1),
	// NOT at the end.
	synthetic := result[2]
	if synthetic.Role != UserRole {
		t.Fatalf("synthetic message role = %q, want user", synthetic.Role)
	}
	if len(synthetic.ContentBlocks) != 1 {
		t.Fatalf("synthetic message content blocks = %d, want 1", len(synthetic.ContentBlocks))
	}
	tr, ok := synthetic.ContentBlocks[0].AsToolResult()
	if !ok {
		t.Fatal("expected tool_result content block")
	}
	if tr.ToolUseID != "call_1" {
		t.Errorf("tool_use_id = %q, want call_1", tr.ToolUseID)
	}
	if !tr.IsError {
		t.Error("synthetic tool result should have IsError=true")
	}
	// Verify position: synthetic must immediately follow the assistant message that contains the tool_use.
	if result[1].Role != AssistantRole {
		t.Fatalf("message at index 1 role = %q, want assistant", result[1].Role)
	}
	if result[2].Role != UserRole {
		t.Fatalf("message at index 2 role = %q, want user (synthetic)", result[2].Role)
	}
}

func TestEnsureToolResultCoverage_InsertPosition(t *testing.T) {
	// Orphaned tool_use in the middle of a conversation.
	// The synthetic tool_result must be inserted right after the assistant message,
	// not at the end.
	msgs := []Message{
		{Role: UserRole, Content: "run ls"},
		{
			Role: AssistantRole,
			ContentBlocks: []ApiContentBlock{
				{Type: ApiToolUseContentType, ToolUse: &ApiToolUseBlock{ID: "call_1", Name: "Bash", Input: json.RawMessage(`{"cmd":"ls"}`)}},
			},
		},
		{Role: UserRole, Content: "continue"},
		{
			Role: AssistantRole,
			ContentBlocks: []ApiContentBlock{
				{Type: ApiToolUseContentType, ToolUse: &ApiToolUseBlock{ID: "call_2", Name: "Read", Input: json.RawMessage(`{"path":"/tmp"}`)}},
			},
		},
		{
			Role: UserRole,
			ContentBlocks: []ApiContentBlock{
				{Type: ApiToolResultContentType, ToolResult: &ApiToolResultBlock{ToolUseID: "call_2", Content: "file contents"}},
			},
		},
	}

	result := EnsureToolResultCoverage(msgs)
	// Expected: 5 original + 1 synthetic for call_1 = 6
	if len(result) != 6 {
		t.Fatalf("expected 6 messages, got %d", len(result))
	}

	// result[0] = user "run ls"
	// result[1] = assistant (tool_use: call_1)
	// result[2] = user (synthetic tool_result for call_1)  ← INSERTED HERE
	// result[3] = user "continue"
	// result[4] = assistant (tool_use: call_2)
	// result[5] = user (tool_result for call_2)

	if result[2].Role != UserRole {
		t.Fatalf("inserted message at index 2 role = %q, want user", result[2].Role)
	}
	tr, ok := result[2].ContentBlocks[0].AsToolResult()
	if !ok {
		t.Fatal("expected tool_result at index 2")
	}
	if tr.ToolUseID != "call_1" {
		t.Errorf("inserted tool_use_id = %q, want call_1", tr.ToolUseID)
	}

	// Verify the "continue" message shifted to index 3
	if result[3].Content != "continue" {
		t.Errorf("message at index 3 content = %q, want 'continue'", result[3].Content)
	}

	// Verify call_2's tool_result is still at the end
	tr2, ok := result[5].ContentBlocks[0].AsToolResult()
	if !ok {
		t.Fatal("expected tool_result at index 5")
	}
	if tr2.ToolUseID != "call_2" {
		t.Errorf("tool_use_id at index 5 = %q, want call_2", tr2.ToolUseID)
	}
}

func TestEnsureToolResultCoverage_PartialOrphans(t *testing.T) {
	msgs := []Message{
		{Role: UserRole, Content: "run both"},
		{
			Role: AssistantRole,
			ContentBlocks: []ApiContentBlock{
				{Type: ApiToolUseContentType, ToolUse: &ApiToolUseBlock{ID: "call_1", Name: "Bash", Input: json.RawMessage(`{"cmd":"ls"}`)}},
				{Type: ApiToolUseContentType, ToolUse: &ApiToolUseBlock{ID: "call_2", Name: "Read", Input: json.RawMessage(`{"path":"/tmp"}`)}},
			},
		},
		{
			Role: UserRole,
			ContentBlocks: []ApiContentBlock{
				{Type: ApiToolResultContentType, ToolResult: &ApiToolResultBlock{ToolUseID: "call_1", Content: "file1.txt"}},
			},
		},
	}

	result := EnsureToolResultCoverage(msgs)
	// Expected: 3 original + 1 synthetic for call_2 = 4
	if len(result) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(result))
	}
	// Synthetic for call_2 is inserted after the assistant message (index 1),
	// so it should be at index 2, pushing the existing tool_result to index 3.
	tr, ok := result[2].ContentBlocks[0].AsToolResult()
	if !ok {
		t.Fatal("expected tool_result content block at index 2")
	}
	if tr.ToolUseID != "call_2" {
		t.Errorf("synthetic tool_use_id = %q, want call_2", tr.ToolUseID)
	}
	// The original partial tool_result (call_1) should have shifted to index 3
	tr1, ok := result[3].ContentBlocks[0].AsToolResult()
	if !ok {
		t.Fatal("expected tool_result content block at index 3")
	}
	if tr1.ToolUseID != "call_1" {
		t.Errorf("shifted tool_use_id = %q, want call_1", tr1.ToolUseID)
	}
}

func TestValidateToolResultCoverage_NoOrphans(t *testing.T) {
	msgs := []Message{
		{Role: UserRole, Content: "hello"},
		{Role: AssistantRole, Content: "hi"},
	}
	result := ValidateToolResultCoverage(msgs)
	if len(result) != len(msgs) {
		t.Fatalf("expected %d messages, got %d", len(msgs), len(result))
	}
}

func TestValidateToolResultCoverage_PatchesOrphans(t *testing.T) {
	msgs := []Message{
		{Role: UserRole, Content: "run ls"},
		{
			Role: AssistantRole,
			ContentBlocks: []ApiContentBlock{
				{Type: ApiToolUseContentType, ToolUse: &ApiToolUseBlock{ID: "call_1", Name: "Bash", Input: json.RawMessage(`{"cmd":"ls"}`)}},
			},
		},
	}

	result := ValidateToolResultCoverage(msgs)
	if len(result) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result))
	}
	// Synthetic must immediately follow the assistant message
	tr, ok := result[2].ContentBlocks[0].AsToolResult()
	if !ok {
		t.Fatal("expected tool_result content block")
	}
	if tr.ToolUseID != "call_1" {
		t.Errorf("tool_use_id = %q, want call_1", tr.ToolUseID)
	}
}

func TestProjectMessagesForRequestKeepsUserToolResults(t *testing.T) {
	messages := []Message{
		{
			Role: UserRole,
			ContentBlocks: []ApiContentBlock{
				{
					Type: ApiToolResultContentType,
					ToolResult: &ApiToolResultBlock{
						ToolUseID: "call_1",
						Content:   "file1\nfile2",
					},
				},
			},
		},
	}

	projected := ProjectMessagesForRequest(messages)
	if len(projected) != 1 {
		t.Fatalf("projected len = %d, want 1", len(projected))
	}
	if len(projected[0].ContentBlocks) != 1 {
		t.Fatalf("projected content blocks = %d, want 1", len(projected[0].ContentBlocks))
	}
	tr, ok := projected[0].ContentBlocks[0].AsToolResult()
	if !ok {
		t.Fatal("expected tool_result block to be preserved")
	}
	if tr.ToolUseID != "call_1" || tr.Content != "file1\nfile2" {
		t.Fatalf("tool result = %+v, want call_1/file1\\nfile2", tr)
	}
}
