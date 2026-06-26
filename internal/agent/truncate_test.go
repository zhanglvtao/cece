package agent

import "testing"

func TestTruncateToolResults(t *testing.T) {
	msgs := []Message{
		{Role: UserRole, Content: "u1"},
		{Role: AssistantRole, Content: "a1", ContentBlocks: []ApiContentBlock{
			{Type: ApiToolUseContentType, ToolUse: &ApiToolUseBlock{ID: "t1", Name: "Bash", Input: nil}},
		}},
		{Role: ToolRole, ContentBlocks: []ApiContentBlock{
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
		{Role: ToolRole, ContentBlocks: []ApiContentBlock{
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

func TestTruncateToolResultsPreservesArtifactMetadata(t *testing.T) {
	msgs := []Message{{Role: ToolRole, ContentBlocks: []ApiContentBlock{{Type: ApiToolResultContentType, ToolResult: &ApiToolResultBlock{
		ToolUseID:     "t1",
		Content:       "Output too large. Full output saved to: .cece/tool-results/bash.txt\nPreview...",
		Truncated:     true,
		TotalLines:    10,
		OutputPath:    ".cece/tool-results/bash.txt",
		OriginalBytes: 9000,
		PreviewBytes:  2000,
	}}}}}

	count, _, _ := TruncateToolResults(msgs)
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
	tr := msgs[0].ContentBlocks[0].ToolResult
	if tr.OutputPath != ".cece/tool-results/bash.txt" || tr.OriginalBytes != 9000 || tr.PreviewBytes != 2000 {
		t.Fatalf("artifact metadata lost: %+v", tr)
	}
	if tr.Content != "[trimmed preview]\nFull output saved to: .cece/tool-results/bash.txt" {
		t.Fatalf("content = %q, want recoverable artifact hint", tr.Content)
	}
	if tr.TotalLines != 0 {
		t.Fatalf("TotalLines = %d, want 0 after preview trim", tr.TotalLines)
	}
}
