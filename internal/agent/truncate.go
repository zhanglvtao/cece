package agent

// TruncateToolResults replaces all tool_result content in messages with "[truncated]".
// Returns count of truncated blocks, estimated tokens before and after.
// Mutates messages in place — irreversible.
func TruncateToolResults(messages []Message) (truncatedCount, tokensBefore, tokensAfter int) {
	tokensBefore = EstimateMessagesTokens(messages)
	for i := range messages {
		for j := range messages[i].ContentBlocks {
			cb := &messages[i].ContentBlocks[j]
			if cb.Type == ApiToolResultContentType && cb.ToolResult != nil {
				if trimToolResultPreview(cb.ToolResult, "[truncated]") {
					truncatedCount++
				}
			}
		}
	}
	tokensAfter = EstimateMessagesTokens(messages)
	return
}

func trimToolResultPreview(tr *ApiToolResultBlock, fallback string) bool {
	if tr == nil {
		return false
	}
	trimmed := fallback
	if tr.OutputPath != "" {
		trimmed = "[trimmed preview]\nFull output saved to: " + tr.OutputPath
	}
	if tr.Content == trimmed {
		return false
	}
	tr.Content = trimmed
	tr.Truncated = true
	tr.TotalLines = 0
	return true
}
