package agent

import (
		"encoding/json"

	"cece/internal/protocol"
	"cece/internal/tool"
)

// ToDTO converts an internal agent.Event to a protocol.Event.
// Returns nil for unrecognized event types.
func ToDTO(e Event) protocol.Event {
	switch v := e.(type) {
	case SessionCreated:
		return protocol.SessionCreated{ID: v.ID, Title: v.Title}

	case UserMessageAdded:
		return protocol.UserMessageAdded{Message: MessageToDTO(v.Message)}

	case SystemReminderAdded:
		return protocol.SystemReminderAdded{Content: v.Content}

	case ModelRequestStarted:
		return protocol.ModelRequestStarted{
			Reason:               v.Reason,
			ToolResults:          v.ToolResults,
			EstimatedInputTokens: v.EstimatedInputTokens,
		}

	case AssistantStarted:
		return protocol.AssistantStarted{}

	case AssistantDelta:
		return protocol.AssistantDelta{Text: v.Text}

	case AssistantCompleted:
		return protocol.AssistantCompleted{Duration: v.Duration}

	case RunFailed:
		errMsg := ""
		if v.Err != nil {
			errMsg = v.Err.Error()
		}
		return protocol.RunFailed{Err: errMsg}

	case StreamStarted:
		return protocol.StreamStarted{
			Model:               v.Model,
			InputTokens:         v.InputTokens,
			Tools:               v.Tools,
			CacheCreationTokens: v.CacheCreationTokens,
			CacheReadTokens:     v.CacheReadTokens,
		}

	case StreamEventDetail:
		return protocol.StreamEventDetail{
			EventType: v.EventType,
			Detail:    v.Detail,
			Text:      v.Text,
		}

	case StreamCompleted:
		return protocol.StreamCompleted{
			OutputTokens: v.OutputTokens,
			StopReason:   v.StopReason,
			Duration:     v.Duration,
			ToolCalls:    v.ToolCalls,
		}

	case TruncationRetry:
		return protocol.TruncationRetry{
			Attempt:       v.Attempt,
			PrevMaxTokens: v.PrevMaxTokens,
			NewMaxTokens:  v.NewMaxTokens,
		}

	case ToolCallStarted:
		return protocol.ToolCallStarted{ID: v.ID, Name: v.Name}

	case ToolCallDelta:
		return protocol.ToolCallDelta{ID: v.ID, Delta: v.Input}

	case ToolCallCompleted:
		return protocol.ToolCallCompleted{ID: v.ID, Name: v.Name, Input: v.Input}

	case ToolCallsReady:
		calls := make([]protocol.ToolUseBlock, len(v.Calls))
		for i, c := range v.Calls {
			calls[i] = toolUseBlockToDTO(c)
		}
		return protocol.ToolCallsReady{Calls: calls}

	case ToolExecStarted:
		return protocol.ToolExecStarted{ID: v.ID, Name: v.Name}

	case ToolExecDelta:
		return protocol.ToolExecDelta{ID: v.ID, Text: v.Text}

	case ToolExecCompleted:
		return protocol.ToolExecCompleted{
			ID:   v.ID,
			Name: v.Name,
			Result: protocol.ToolResult{
				Content: v.Result.Content,
				IsError: v.Result.IsError,
			},
		}

	case ThinkingStarted:
		return protocol.ThinkingStarted{Index: v.Index}

	case ThinkingDelta:
		return protocol.ThinkingDelta{Text: v.Text}

	case ThinkingCompleted:
		return protocol.ThinkingCompleted{Text: v.Text, Signature: v.Signature}

	case PlanApprovalRequested:
		return protocol.PlanApprovalRequested{PlanContent: v.PlanContent, PlanFile: v.PlanFile}

	case QuestionAsked:
		return protocol.QuestionAsked{CallID: v.CallID, Questions: questionsToDTO(v.Questions)}

	case QueuedInputPromoted:
		return protocol.QueuedInputPromoted{}

	case Compacting:
		return protocol.CompactingEvent{}

	case Compacted:
		return protocol.CompactedEvent{
			TokensBefore:   v.TokensBefore,
			TokensAfter:    v.TokensAfter,
			MessagesBefore: v.MessagesBefore,
			MessagesAfter:  v.MessagesAfter,
			Summary:        v.Summary,
		}

	case TurnCompleted:
		return protocol.TurnCompleted{}

	case TaskUpdated:
		return protocol.TaskUpdatedEvent{Tasks: taskItemsToDTO(v.Tasks)}
	}
	return nil
}

func MessageToDTO(m Message) protocol.Message {
	return protocol.Message{
		Role:          string(m.Role),
		Content:       m.Content,
		ContentBlocks: contentBlocksToDTO(m.ContentBlocks),
	}
}

func contentBlocksToDTO(blocks []ApiContentBlock) []protocol.ContentBlock {
	if len(blocks) == 0 {
		return nil
	}
	out := make([]protocol.ContentBlock, len(blocks))
	for i, b := range blocks {
		out[i] = contentBlockToDTO(b)
	}
	return out
}

func contentBlockToDTO(b ApiContentBlock) protocol.ContentBlock {
	cb := protocol.ContentBlock{Type: protocol.ContentBlockType(b.Type)}
	switch b.Type {
	case ApiTextContentType:
		cb.Text = b.Text
	case ApiThinkingContentType:
		if b.Thinking != nil {
			cb.Text = b.Thinking.Text
		}
	case ApiToolUseContentType:
		if b.ToolUse != nil {
			cb.ToolUse = &protocol.ToolUseBlock{
				ID:    b.ToolUse.ID,
				Name:  b.ToolUse.Name,
				Input: b.ToolUse.Input,
			}
		}
	case ApiToolResultContentType:
		if b.ToolResult != nil {
			cb.ToolResult = &protocol.ToolResultBlock{
					ToolUseID:  b.ToolResult.ToolUseID,
					Content:    b.ToolResult.Content,
					IsError:    b.ToolResult.IsError,
					Truncated:  b.ToolResult.Truncated,
					TotalLines: b.ToolResult.TotalLines,
				}
		}
	}
	return cb
}

func toolUseBlockToDTO(b ApiToolUseBlock) protocol.ToolUseBlock {
	return protocol.ToolUseBlock{
		ID:    b.ID,
		Name:  b.Name,
		Input: b.Input,
	}
}

func questionsToDTO(qs []tool.Question) []protocol.Question {
	if len(qs) == 0 {
		return nil
	}
	out := make([]protocol.Question, len(qs))
	for i, q := range qs {
		out[i] = protocol.Question{
			Question:    q.Question,
			Header:      q.Header,
			MultiSelect: q.MultiSelect,
			Preview:     q.Preview,
			Options:     questionOptionsToDTO(q.Options),
		}
	}
	return out
}

func questionOptionsToDTO(opts []tool.QuestionOption) []protocol.QuestionOption {
	if len(opts) == 0 {
		return nil
	}
	out := make([]protocol.QuestionOption, len(opts))
	for i, o := range opts {
		out[i] = protocol.QuestionOption{Label: o.Label, Description: o.Description}
	}
	return out
}

func modelInfoToDTO(m ModelInfo) protocol.ModelInfo {
	return protocol.ModelInfo{
		ID:               m.ID,
		DisplayName:      m.DisplayName,
		MaxContextWindow: m.MaxContextWindow,
		Provider:         m.Provider,
		APIKey:           m.APIKey,
		BaseURL:          m.BaseURL,
		AuthMode:         m.AuthMode,
		AuthHelper:       m.AuthHelper,
		Protocol:         m.Protocol,
		ConfigName:       m.ConfigName,
	}
}

func modelInfosToDTO(ms []ModelInfo) []protocol.ModelInfo {
	if len(ms) == 0 {
		return nil
	}
	out := make([]protocol.ModelInfo, len(ms))
	for i, m := range ms {
		out[i] = modelInfoToDTO(m)
	}
	return out
}

// DtoAnswersToInternal converts protocol.QuestionAnswer to internal tool.QuestionAnswer.
func DtoAnswersToInternal(answers []protocol.QuestionAnswer) []tool.QuestionAnswer {
	if len(answers) == 0 {
		return nil
	}
	out := make([]tool.QuestionAnswer, len(answers))
	for i, a := range answers {
		out[i] = tool.QuestionAnswer{
			Question: a.Question,
			Selected: a.Selected,
			Custom:   a.Custom,
		}
	}
	return out
}

// parseAskUserQuestions extracts questions from AskUserQuestion tool input.
func parseAskUserQuestions(input json.RawMessage) ([]tool.Question, error) {
	var wrapper struct {
		Questions []tool.Question `json:"questions"`
	}
	if err := json.Unmarshal(input, &wrapper); err != nil {
		return nil, err
	}
	return wrapper.Questions, nil
}

func taskItemsToDTO(items []tool.TaskItem) []protocol.TaskItem {
	if len(items) == 0 {
		return nil
	}
	out := make([]protocol.TaskItem, len(items))
	for i, item := range items {
		out[i] = protocol.TaskItem{
			Content:    item.Content,
			ActiveForm: item.ActiveForm,
			Status:     string(item.Status),
		}
	}
	return out
}

// escalatedMaxTokens is the output token limit used when a response
// hits max_tokens and we silently retry with a larger budget.
const escalatedMaxTokens = 64000
