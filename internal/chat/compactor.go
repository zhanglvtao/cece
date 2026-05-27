package chat

import (
	"context"
	"fmt"
	"strings"

	"cece/internal/prompt"
)

const defaultKeepRecentTurns = 2

// CompactResult holds the outcome of a history compaction.
type CompactResult struct {
	Messages       []Message
	MessagesBefore int
	MessagesAfter  int
	TokensBefore   int
	TokensAfter    int
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

// Compact summarizes older messages and returns a new, shorter message list.
// All messages (including complete tool_result content) are sent to the LLM
// for summarization — no truncation. The LLM decides what's important.
func (c *Compactor) Compact(ctx context.Context, messages []Message) (CompactResult, error) {
	summarize, keep := splitMessagesForCompact(messages, c.keepRecentTurns)

	if len(summarize) == 0 {
		return CompactResult{
			Messages:       messages,
			MessagesBefore: len(messages),
			MessagesAfter:  len(messages),
		}, nil
	}

	tokensBefore := estimateMessagesTokens(messages)

	summary, err := c.generateSummary(ctx, summarize)
	if err != nil {
		return CompactResult{}, fmt.Errorf("generate compact summary: %w", err)
	}

	// Build new history: summary user message + preserved recent messages
	summaryMsg := Message{
		Role: UserRole,
		Content: fmt.Sprintf(
			"[Previous conversation summary]\n\n%s\n\n[End of summary. Recent messages continue below.]",
			summary,
		),
	}

	result := make([]Message, 0, 1+len(keep))
	result = append(result, summaryMsg)
	result = append(result, keep...)

	tokensAfter := estimateMessagesTokens(result)

	return CompactResult{
		Messages:       result,
		MessagesBefore: len(messages),
		MessagesAfter:  len(result),
		TokensBefore:   tokensBefore,
		TokensAfter:    tokensAfter,
	}, nil
}

// generateSummary calls the model to produce a conversation summary.
// Sends all messages with complete tool_result content — no truncation.
func (c *Compactor) generateSummary(ctx context.Context, messages []Message) (string, error) {
	systemPrompt := SystemPrompt{
		Blocks: []SystemBlock{
			{Text: buildCompactSystemPrompt()},
		},
	}

	// No tools for summary generation
	chunks, err := c.client.Stream(ctx, messages, systemPrompt, nil, 4096)
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

	return strings.TrimSpace(buf.String()), nil
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

// buildCompactSystemPrompt returns the system prompt for summary generation.
func buildCompactSystemPrompt() string {
	return `Your task is to create a detailed summary of the conversation so far, preserving all technical details that would be essential for continuing development work without losing context.

Before providing your summary, analyze the conversation chronologically and identify:
- The user's explicit requests and intents
- Key decisions, technical concepts, and code patterns
- Specific details: file names, function signatures, code snippets, file edits
- Errors encountered and how they were fixed
- User feedback, especially when told to do something differently

Your summary should include these sections:

1. Primary Request and Intent: What the user asked for
2. Key Technical Details: Files examined, modified, or created with relevant code snippets
3. Errors and Fixes: Problems encountered and solutions applied
4. User Feedback: Any specific direction changes from the user
5. Current State: What was being worked on immediately before this summary

Be thorough and specific. Include actual file paths, code snippets, and function names. This summary will be the only context the assistant has about the earlier conversation.`
}

// estimateMessagesTokens estimates total tokens for a slice of messages.
func estimateMessagesTokens(messages []Message) int {
	total := 0
	for _, m := range messages {
		if m.Content != "" {
			total += prompt.EstimateTokens(m.Content)
		}
		for _, cb := range m.ContentBlocks {
			switch cb.Type {
			case ApiTextContentType:
				total += prompt.EstimateTokens(cb.Text)
			case ApiToolUseContentType:
				if cb.ToolUse != nil {
					total += prompt.EstimateTokens(cb.ToolUse.Name)
					total += prompt.EstimateTokens(string(cb.ToolUse.Input))
				}
			case ApiToolResultContentType:
				if cb.ToolResult != nil {
					total += prompt.EstimateTokens(cb.ToolResult.Content)
				}
			case ApiThinkingContentType:
				if cb.Thinking != nil {
					total += prompt.EstimateTokens(cb.Thinking.Text)
				}
			}
		}
	}
	return total
}
