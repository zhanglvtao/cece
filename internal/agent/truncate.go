package agent

// TruncateToolResults replaces all tool_result content in messages with "[truncated]".
// Returns count of truncated blocks, estimated tokens before and after.
// Mutates messages in place — irreversible.
func TruncateToolResults(messages []Message) (truncatedCount, tokensBefore, tokensAfter int) {
	tokensBefore = estimateMessagesTokens(messages)
	for i := range messages {
		for j := range messages[i].ContentBlocks {
			cb := &messages[i].ContentBlocks[j]
			if cb.Type == ApiToolResultContentType && cb.ToolResult != nil {
				if cb.ToolResult.Content != "[truncated]" {
					cb.ToolResult.Content = "[truncated]"
					cb.ToolResult.Truncated = true
					cb.ToolResult.TotalLines = 0
					truncatedCount++
				}
			}
		}
	}
	tokensAfter = estimateMessagesTokens(messages)
	return
}
