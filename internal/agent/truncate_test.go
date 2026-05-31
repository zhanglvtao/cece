package agent

import "testing"

func TestTruncateToolResults(t *testing.T) {
	msgs := []Message{
		{Role: UserRole, Content: "u1"},
		{Role: AssistantRole, Content: "a1", ContentBlocks: []ApiContentBlock{
			{Type: ApiToolUseContentType, ToolUse: &ApiToolUseBlock{ID: "t1", Name: "Bash", Input: nil}},
		}},
		{Role: UserRole, ContentBlocks: []ApiContentBlock{
			{Type: ApiToolResultContentType, ToolResult: &ApiToolResultBlock{ToolUseID: "t1", Content: "line1\nline2\nline3", TotalLines: 3}},
			{Type: ApiTextContentType, Text: "some text"},
		}},
		{Role: UserRole, Content: "u2"},
	}

	count, before, after := TruncateToolResults(msgs)
	if count != 1 {
		t.Fatalf("truncated count = %d, want 1", count)
	}
	if before <= after {
		t.Fatalf("tokens before=%d should be > after=%d", before, after)
	}
	tr := msgs[2].ContentBlocks[0].ToolResult
	if tr.Content != "[truncated]" {
		t.Fatalf("tool_result content = %q, want [truncated]", tr.Content)
	}
	if !tr.Truncated {
		t.Fatal("tool_result should be marked truncated")
	}
	if msgs[2].ContentBlocks[1].Text != "some text" {
		t.Fatal("non-tool-result content should be preserved")
	}
	if msgs[0].Content != "u1" {
		t.Fatal("user message should be preserved")
	}
}

func TestTruncateToolResultsAlreadyTruncated(t *testing.T) {
	msgs := []Message{
		{Role: UserRole, ContentBlocks: []ApiContentBlock{
			{Type: ApiToolResultContentType, ToolResult: &ApiToolResultBlock{ToolUseID: "t1", Content: "[truncated]"}},
		}},
	}
	count, _, _ := TruncateToolResults(msgs)
	if count != 0 {
		t.Fatalf("already truncated: count = %d, want 0", count)
	}
}

func TestTruncateToolResultsNoToolResults(t *testing.T) {
	msgs := []Message{
		{Role: UserRole, Content: "u1"},
		{Role: AssistantRole, Content: "a1"},
	}
	count, before, after := TruncateToolResults(msgs)
	if count != 0 {
		t.Fatalf("count = %d, want 0", count)
	}
	if before != after {
		t.Fatalf("tokens should be unchanged: before=%d after=%d", before, after)
	}
}
