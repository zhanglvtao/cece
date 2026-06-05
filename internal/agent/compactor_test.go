package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/zhanglvtao/cece/internal/tool"
)

// mockStreamClient implements ModelClient for testing.
type mockStreamClient struct {
	streamFn func(ctx context.Context, messages []Message, system SystemPrompt, tools []tool.Definition, maxTokens int) (<-chan ApiStreamEvent, error)
}

func (m *mockStreamClient) Stream(ctx context.Context, messages []Message, system SystemPrompt, tools []tool.Definition, maxTokens int) (<-chan ApiStreamEvent, error) {
	return m.streamFn(ctx, messages, system, tools, maxTokens)
}

func makeMockStreamClient(summaryText string) *mockStreamClient {
	return &mockStreamClient{
		streamFn: func(ctx context.Context, messages []Message, system SystemPrompt, tools []tool.Definition, maxTokens int) (<-chan ApiStreamEvent, error) {
			ch := make(chan ApiStreamEvent, 10)
			go func() {
				ch <- ApiStreamEvent{EventType: "message_start", InputTokens: 100}
				ch <- ApiStreamEvent{EventType: "content_block_start", Index: 0, Detail: "text"}
				ch <- ApiStreamEvent{Delta: summaryText, Detail: "text_delta"}
				ch <- ApiStreamEvent{EventType: "content_block_stop", Index: 0}
				ch <- ApiStreamEvent{Done: true, EventType: "message_stop"}
				close(ch)
			}()
			return ch, nil
		},
	}
}

func TestSplitMessagesForCompact_BasicGrouping(t *testing.T) {
	msgs := []Message{
		{Role: UserRole, Content: "u1"},
		{Role: AssistantRole, Content: "a1"},
		{Role: UserRole, Content: "u2"},
		{Role: AssistantRole, Content: "a2"},
		{Role: UserRole, Content: "u3"},
		{Role: AssistantRole, Content: "a3"},
		{Role: UserRole, Content: "u4"},
		{Role: AssistantRole, Content: "a4"},
	}

	summarize, keep := splitMessagesForCompact(msgs, 2)
	if len(summarize) != 4 {
		t.Errorf("expected 4 messages to summarize, got %d", len(summarize))
	}
	if len(keep) != 4 {
		t.Errorf("expected 4 messages to keep, got %d", len(keep))
	}
	if keep[0].Content != "u3" {
		t.Errorf("expected first kept message to be u3, got %s", keep[0].Content)
	}
}

func TestSplitMessagesForCompact_TooFewToCompact(t *testing.T) {
	msgs := []Message{
		{Role: UserRole, Content: "u1"},
		{Role: AssistantRole, Content: "a1"},
	}
	summarize, keep := splitMessagesForCompact(msgs, 2)
	if len(summarize) != 0 {
		t.Errorf("expected 0 messages to summarize, got %d", len(summarize))
	}
	if len(keep) != 2 {
		t.Errorf("expected 2 messages to keep, got %d", len(keep))
	}
}

func TestSplitMessagesForCompact_WithToolResults(t *testing.T) {
	msgs := []Message{
		{Role: UserRole, Content: "u1"},
		{Role: AssistantRole, Content: "a1"},
		{Role: UserRole, ContentBlocks: []ApiContentBlock{
			{Type: ApiToolResultContentType, ToolResult: &ApiToolResultBlock{ToolUseID: "t1", Content: "result1"}},
		}},
		{Role: AssistantRole, Content: "a2"},
		{Role: UserRole, Content: "u3"},
		{Role: AssistantRole, Content: "a3"},
	}
	summarize, keep := splitMessagesForCompact(msgs, 1)
	if len(summarize) != 4 {
		t.Errorf("expected 4 messages to summarize, got %d", len(summarize))
	}
	if len(keep) != 2 {
		t.Errorf("expected 2 messages to keep, got %d", len(keep))
	}
}

func TestCompact_WithMockStream(t *testing.T) {
	msgs := []Message{
		{Role: UserRole, Content: "u1"},
		{Role: AssistantRole, Content: "a1"},
		{Role: UserRole, Content: "u2"},
		{Role: AssistantRole, Content: "a2"},
		{Role: UserRole, Content: "u3"},
		{Role: AssistantRole, Content: "a3"},
	}

	mc := makeMockStreamClient("The user asked about u1 and u2.")
	c := NewCompactor(mc, 2)
	result, err := c.Compact(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Compact failed: %v", err)
	}
	// 3 turns, keepRecentTurns=2: summarize turn1(u1,a1), keep turns2+3(u2,a2,u3,a3)
	if result.SummarizeCount != 2 {
		t.Errorf("expected SummarizeCount=2, got %d", result.SummarizeCount)
	}
	if result.KeepCount != 4 {
		t.Errorf("expected KeepCount=4, got %d", result.KeepCount)
	}
	if result.Boundary.Role != UserRole {
		t.Errorf("expected boundary role to be user, got %s", result.Boundary.Role)
	}
	if !result.Boundary.CompactBoundary {
		t.Error("expected boundary message to have CompactBoundary=true")
	}
	if !strings.Contains(result.Boundary.Content, "continued from a previous conversation") {
		t.Errorf("expected boundary content to contain 'continued from a previous conversation', got %s", result.Boundary.Content)
	}
}

func TestCompact_NothingToSummarize(t *testing.T) {
	msgs := []Message{
		{Role: UserRole, Content: "u1"},
		{Role: AssistantRole, Content: "a1"},
	}
	mc := makeMockStreamClient("should not be called")
	c := NewCompactor(mc, 2)
	result, err := c.Compact(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Compact failed: %v", err)
	}
	if result.SummarizeCount != 0 {
		t.Errorf("expected SummarizeCount=0 when nothing to summarize, got %d", result.SummarizeCount)
	}
}

func TestBuildCompactSystemPrompt(t *testing.T) {
	prompt := buildCompactUserPrompt()
	if !strings.Contains(prompt, "summary") && !strings.Contains(prompt, "Summary") {
		t.Error("compact prompt should mention summary")
	}
	if !strings.Contains(prompt, "<analysis>") {
		t.Error("compact prompt should instruct <analysis> block")
	}
	if !strings.Contains(prompt, "All user messages") {
		t.Error("compact prompt should include 'All user messages' section")
	}
}

func TestFormatCompactSummary(t *testing.T) {
	input := `<analysis>
Let me think about what happened...
The user asked for X and I did Y.
</analysis>

1. Primary Request and Intent: The user asked for X
2. Key Technical Concepts: Go, HTTP
3. Files and Code Sections: main.go - added handler`

	expected := `1. Primary Request and Intent: The user asked for X
2. Key Technical Concepts: Go, HTTP
3. Files and Code Sections: main.go - added handler`

	result := formatCompactSummary(input)
	if result != expected {
		t.Errorf("expected:\n%s\n\ngot:\n%s", expected, result)
	}
}

func TestFormatCompactSummary_NoAnalysisBlock(t *testing.T) {
	input := "1. Primary Request: something\n2. Key Details: none"
	result := formatCompactSummary(input)
	if result != input {
		t.Errorf("expected unchanged when no analysis block, got: %s", result)
	}
}

func TestStripTag(t *testing.T) {
	tests := []struct {
		name string
		tag  string
		in   string
		want string
	}{
		{"simple", "analysis", "<analysis>think</analysis>\nresult", "result"},
		{"with surrounding", "analysis", "before\n<analysis>think</analysis>\nafter", "before\n\nafter"},
		{"no tag", "analysis", "no tag here", "no tag here"},
		{"summary tag", "summary", "<summary>text</summary>", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripTag(tt.in, tt.tag)
			if got != tt.want {
				t.Errorf("stripTag(%q, %q) = %q, want %q", tt.in, tt.tag, got, tt.want)
			}
		})
	}
}

func TestCanCompactMessages(t *testing.T) {
	msgs := []Message{
		{Role: UserRole, Content: "u1"},
		{Role: AssistantRole, Content: "a1"},
		{Role: UserRole, Content: "u2"},
		{Role: AssistantRole, Content: "a2"},
		{Role: UserRole, Content: "u3"},
		{Role: AssistantRole, Content: "a3"},
	}
	if !CanCompactMessages(msgs, 2) {
		t.Fatal("expected messages to be compactable")
	}
}

func TestCanCompactMessagesTooFewTurns(t *testing.T) {
	msgs := []Message{
		{Role: UserRole, Content: "u1"},
		{Role: AssistantRole, Content: "a1"},
		{Role: UserRole, Content: "u2"},
	}
	if CanCompactMessages(msgs, 2) {
		t.Fatal("expected messages to be not compactable")
	}
}

func TestMessagesAfterCompactBoundary(t *testing.T) {
	msgs := []Message{
		{Role: UserRole, Content: "u1"},
		{Role: AssistantRole, Content: "a1"},
		{Role: UserRole, Content: "u2", CompactBoundary: true},
		{Role: AssistantRole, Content: "a2"},
		{Role: UserRole, Content: "u3"},
	}
	result := MessagesAfterCompactBoundary(msgs)
	if len(result) != 3 {
		t.Errorf("expected 3 messages after boundary (boundary + a2 + u3), got %d", len(result))
	}
	if result[0].Content != "u2" {
		t.Errorf("expected first message to be boundary u2, got %s", result[0].Content)
	}
	if !result[0].CompactBoundary {
		t.Error("expected first message to be compact boundary")
	}
}

func TestMessagesAfterCompactBoundary_NoBoundary(t *testing.T) {
	msgs := []Message{
		{Role: UserRole, Content: "u1"},
		{Role: AssistantRole, Content: "a1"},
	}
	result := MessagesAfterCompactBoundary(msgs)
	if len(result) != 2 {
		t.Errorf("expected all 2 messages when no boundary, got %d", len(result))
	}
}

func TestMessagesAfterCompactBoundary_MultipleBoundaries(t *testing.T) {
	msgs := []Message{
		{Role: UserRole, Content: "u1", CompactBoundary: true},
		{Role: AssistantRole, Content: "a1"},
		{Role: UserRole, Content: "u2", CompactBoundary: true},
		{Role: AssistantRole, Content: "a2"},
		{Role: UserRole, Content: "u3"},
	}
	result := MessagesAfterCompactBoundary(msgs)
	if len(result) != 3 {
		t.Errorf("expected 3 messages after last boundary, got %d", len(result))
	}
	if result[0].Content != "u2" {
		t.Errorf("expected first message after last boundary to be u2, got %s", result[0].Content)
	}
}
