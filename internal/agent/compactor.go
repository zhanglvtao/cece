package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/zhanglvtao/cece/internal/prompt"
)

const defaultKeepRecentTurns = 2

// CompactResult holds the outcome of a history compaction.
type CompactResult struct {
	// Boundary is the compact boundary + summary message to insert into history.
	// It has CompactBoundary=true and contains the conversation summary.
	Boundary Message
	// SummarizeCount is the number of older messages that were summarized.
	SummarizeCount int
	// KeepCount is the number of recent messages preserved verbatim.
	KeepCount    int
	TokensBefore int
	TokensAfter  int
}

// Compactor compresses conversation history by summarizing older messages.
type Compactor struct {
	client          ModelClient
	keepRecentTurns int
}

// NewCompactor creates a Compactor with the given model client.
func NewCompactor(client ModelClient, keepRecentTurns int) *Compactor {
	if keepRecentTurns < 1 {
		keepRecentTurns = defaultKeepRecentTurns
	}
	return &Compactor{
		client:          client,
		keepRecentTurns: keepRecentTurns,
	}
}

// Compact summarizes older messages and returns a boundary message to insert
// into history. All messages (including complete tool_result content) are sent
// to the LLM for summarization — no truncation. The LLM decides what's important.
//
// The caller should append the Boundary message to history. When building API
// requests, MessagesAfterCompactBoundary will skip everything before the boundary.
func (c *Compactor) Compact(ctx context.Context, messages []Message) (CompactResult, error) {
	summarize, keep := splitMessagesForCompact(messages, c.keepRecentTurns)

	if len(summarize) == 0 {
		return CompactResult{SummarizeCount: 0, KeepCount: len(messages)}, nil
	}

	tokensBefore := EstimateMessagesTokens(messages)

	summary, err := c.GenerateSummary(ctx, summarize)
	if err != nil {
		return CompactResult{}, fmt.Errorf("generate compact summary: %w", err)
	}

	// Build boundary message with summary
	boundary := Message{
		Role: UserRole,
		Content: fmt.Sprintf(
			"This session is being continued from a previous conversation that ran out of context. The summary below covers the earlier portion of the conversation.\n\n%s\n\nRecent messages are preserved verbatim.",
			summary,
		),
		CompactBoundary: true,
	}

	// Estimate tokens after: boundary + keep messages
	tokensAfter := EstimateMessagesTokens(append([]Message{boundary}, keep...))

	return CompactResult{
		Boundary:       boundary,
		SummarizeCount: len(summarize),
		KeepCount:      len(keep),
		TokensBefore:   tokensBefore,
		TokensAfter:    tokensAfter,
	}, nil
}

// GenerateSummary calls the model to produce a conversation summary.
// Sends all messages with complete tool_result content — no truncation.
// The compact prompt is appended as a final user message (like claude-code),
// and the system prompt is a short role instruction.
func (c *Compactor) GenerateSummary(ctx context.Context, messages []Message) (string, error) {
	messages = ValidateToolResultCoverage(EnsureToolResultCoverage(messages))

	systemPrompt := SystemPrompt{
		Blocks: []SystemBlock{
			{Text: "You are a helpful AI assistant tasked with summarizing conversations."},
		},
	}

	// Append the compact instruction as a user message at the end
	compactRequest := Message{
		Role:    UserRole,
		Content: buildCompactUserPrompt(),
	}
	requestMessages := make([]Message, 0, len(messages)+1)
	requestMessages = append(requestMessages, messages...)
	requestMessages = append(requestMessages, compactRequest)

	// No tools for summary generation
	chunks, err := c.client.Stream(ctx, requestMessages, systemPrompt, nil, 4096)
	if err != nil {
		return "", err
	}

	var buf strings.Builder
	for chunk := range chunks {
		if chunk.Err != nil {
			return "", chunk.Err
		}
		if chunk.Delta != "" && chunk.Detail != "thinking_delta" {
			buf.WriteString(chunk.Delta)
		}
		if chunk.Done {
			break
		}
	}

	return formatCompactSummary(strings.TrimSpace(buf.String())), nil
}

// formatCompactSummary strips the <analysis> drafting scratchpad from the
// summary. The analysis block improves summary quality by letting the model
// think first, but has no informational value once the summary is written.
func formatCompactSummary(summary string) string {
	// Strip analysis section
	summary = stripTag(summary, "analysis")

	// Clean up extra whitespace between sections
	summary = strings.ReplaceAll(summary, "\n\n\n", "\n\n")

	return strings.TrimSpace(summary)
}

// stripTag removes the first occurrence of <tag>...</tag> from s.
func stripTag(s string, tag string) string {
	open := "<" + tag + ">"
	close := "</" + tag + ">"
	start := strings.Index(s, open)
	if start == -1 {
		return s
	}
	end := strings.Index(s, close)
	if end == -1 || end < start {
		return s
	}
	before := strings.TrimRight(s[:start], "\n")
	after := strings.TrimLeft(s[end+len(close):], "\n")
	if before == "" {
		return after
	}
	if after == "" {
		return before
	}
	return before + "\n\n" + after
}

// splitMessagesForCompact splits messages into two groups:
//   - summarize: older messages that will be replaced by a summary
//   - keep: recent messages preserved verbatim (with full tool_result content)
//
// A "turn" is a user message followed by its responses. We count from the end
// and keep `keepRecentTurns` turns.
func splitMessagesForCompact(messages []Message, keepRecentTurns int) (summarize, keep []Message) {
	if len(messages) == 0 || keepRecentTurns <= 0 {
		return nil, messages
	}

	// Count user-role messages to determine turn boundaries from the end
	userMsgIndices := []int{}
	for i, m := range messages {
		if m.Role == UserRole {
			userMsgIndices = append(userMsgIndices, i)
		}
	}

	// If fewer turns than keepRecentTurns, nothing to summarize
	if len(userMsgIndices) <= keepRecentTurns {
		return nil, messages
	}

	// The split point: the index of the N-th-from-last user message
	splitIdx := userMsgIndices[len(userMsgIndices)-keepRecentTurns]

	return messages[:splitIdx], messages[splitIdx:]
}

// CanCompactMessages returns true if there are enough messages to summarize
// (i.e. more user turns than keepRecentTurns).
func CanCompactMessages(messages []Message, keepRecentTurns int) bool {
	summarize, _ := splitMessagesForCompact(messages, keepRecentTurns)
	return len(summarize) > 0
}

// buildCompactUserPrompt returns the user message prompt for summary generation.
func buildCompactUserPrompt() string {
	return `CRITICAL: Respond with TEXT ONLY. Do NOT call any tools.

Your task is to create a detailed summary of the conversation so far, paying close attention to the user's explicit requests and your previous actions.
This summary should be thorough in capturing technical details, code patterns, and architectural decisions that would be essential for continuing development work without losing context.

Before providing your final summary, wrap your analysis in <analysis> tags to organize your thoughts and ensure you've covered all necessary points. In your analysis process:

1. Chronologically analyze each message and section of the conversation. For each section thoroughly identify:
   - The user's explicit requests and intents
   - Your approach to addressing the user's requests
   - Key decisions, technical concepts and code patterns
   - Specific details like:
     - file names
     - full code snippets
     - function signatures
     - file edits
   - Errors that you ran into and how you fixed them
   - Pay special attention to specific user feedback that you received, especially if the user told you to do something differently.
2. Double-check for technical accuracy and completeness, addressing each required element thoroughly.

Your summary should include the following sections:

1. Primary Request and Intent: Capture all of the user's explicit requests and intents in detail
2. Key Technical Concepts: List all important technical concepts, technologies, and frameworks discussed.
3. Files and Code Sections: Enumerate specific files and code sections examined, modified, or created. Pay special attention to the most recent messages and include full code snippets where applicable and include a summary of why this file read or edit is important.
4. Errors and fixes: List all errors that you ran into, and how you fixed them. Pay special attention to specific user feedback that you received, especially if the user told you to do something differently.
5. Problem Solving: Document problems solved and any ongoing troubleshooting efforts.
6. All user messages: List ALL user messages that are not tool results. These are critical for understanding the users' feedback and changing intent.
7. Pending Tasks: Outline any pending tasks that you have explicitly been asked to work on.
8. Current Work: Describe in detail precisely what was being worked on immediately before this summary request, paying special attention to the most recent messages from both user and assistant. Include file names and code snippets where applicable.
9. Optional Next Step: List the next step that you will take that is related to the most recent work you were doing. IMPORTANT: ensure that this step is DIRECTLY in line with the user's most recent explicit requests, and the task you were working on immediately before this summary request. If your last task was concluded, then only list next steps if they are explicitly in line with the users request. Do not start on tangential requests or really old requests that were already completed without confirming with the user first.
                       If there is a next step, include direct quotes from the most recent conversation showing exactly what task you were working on and where you left off. This should be verbatim to ensure there's no drift in task interpretation.

Please provide your summary based on the conversation so far, following this structure and ensuring precision and thoroughness in your response.`
}

// EstimateMessagesTokens estimates total tokens for a slice of messages.
func EstimateMessagesTokens(messages []Message) int {
	total := 0
	for _, m := range messages {
		total += overheadPerMessage
		if m.Content != "" {
			total += prompt.PreciseEstimateTokens(m.Content)
		}
		for _, cb := range m.ContentBlocks {
			switch cb.Type {
			case ApiTextContentType:
				total += overheadTextBlock
				total += prompt.PreciseEstimateTokens(cb.Text)
			case ApiToolUseContentType:
				total += overheadToolUseBlock
				if cb.ToolUse != nil {
					total += prompt.PreciseEstimateTokens(cb.ToolUse.Name)
					total += prompt.PreciseEstimateTokens(string(cb.ToolUse.Input))
				}
			case ApiToolResultContentType:
				total += overheadToolResultBlock
				if cb.ToolResult != nil {
					total += prompt.PreciseEstimateTokens(cb.ToolResult.Content)
				}
			case ApiThinkingContentType:
				total += overheadThinkingBlock
				if cb.Thinking != nil {
					total += prompt.PreciseEstimateTokens(cb.Thinking.Text)
				}
			}
		}
	}
	return total
}
