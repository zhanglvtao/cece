package chat

import (
	"context"
	"log/slog"
	"time"
)

type TurnRequest struct {
	Messages []Message
	System   SystemPrompt
}

// TurnDeps contains the Runtime-owned operations a turn needs while keeping
// the agent loop outside the Runtime facade.
type TurnDeps struct {
	AppendMessage        func(Message)
	PersistMessage       func(context.Context, Message)
	UpdateSessionMeta    func(context.Context, modelResponse)
	DrainQueuedInputs    func() []string
	HistorySnapshot      func() []Message
	ResetQuestionAnswers func()
}

// TurnRunner owns the agent loop for one user turn.
type TurnRunner struct {
	streamer        *ModelStreamer
	interactionGate *InteractionGate
	toolExecutor    *ToolExecutor
	maxTokens       int
	deps            TurnDeps
}

func NewTurnRunner(streamer *ModelStreamer, interactionGate *InteractionGate, toolExecutor *ToolExecutor, maxTokens int, deps TurnDeps) *TurnRunner {
	return &TurnRunner{
		streamer:        streamer,
		interactionGate: interactionGate,
		toolExecutor:    toolExecutor,
		maxTokens:       maxTokens,
		deps:            deps,
	}
}

func (r *TurnRunner) Run(ctx context.Context, req TurnRequest, events chan<- Event) {
	// Agent loop: keep calling the model until it stops requesting tools.
	messages := req.Messages
	turnStart := time.Now()
	reason := "user"
	var toolResultNames []string
	for {
		resp, err := r.streamer.Stream(ctx, ModelStreamRequest{
			Messages:    messages,
			System:      req.System,
			Reason:      reason,
			MaxTokens:   r.maxTokens,
			ToolResults: toolResultNames,
		}, events)
		if err != nil {
			events <- UIRunFailed{Err: err}
			return
		}

		// Silent escalation: if output was truncated, retry once with 64K.
		if resp.stopReason == "max_tokens" {
			events <- UITruncationRetry{
				Attempt:       1,
				PrevMaxTokens: r.maxTokens,
				NewMaxTokens:  escalatedMaxTokens,
			}
			slog.Info("output truncated, escalating max_tokens", "from", r.maxTokens, "to", escalatedMaxTokens)
			resp, err = r.streamer.Stream(ctx, ModelStreamRequest{
				Messages:    messages,
				System:      req.System,
				Reason:      reason,
				MaxTokens:   escalatedMaxTokens,
				ToolResults: toolResultNames,
			}, events)
			if err != nil {
				events <- UIRunFailed{Err: err}
				return
			}
		}

		assistant := assistantMessageFromResponse(resp)
		r.deps.AppendMessage(assistant)
		r.deps.PersistMessage(ctx, assistant)
		r.deps.UpdateSessionMeta(ctx, resp)

		// No tool calls -- conversation turn is done.
		if resp.stopReason != "tool_use" || len(resp.toolCalls) == 0 {
			events <- UIAssistantCompleted{Duration: time.Since(turnStart)}
			return
		}

		if err := r.interactionGate.WaitIfNeeded(ctx, resp.toolCalls, events); err != nil {
			events <- UIRunFailed{Err: err}
			return
		}

		toolResults := r.toolExecutor.ExecuteBatch(ctx, resp.toolCalls, events)
		toolResultNames = make([]string, len(resp.toolCalls))
		for i, tc := range resp.toolCalls {
			toolResultNames[i] = tc.Name
		}

		// Append tool_result as a user message.
		resultMsg := Message{
			Role:          UserRole,
			ContentBlocks: toolResults,
		}
		r.deps.AppendMessage(resultMsg)
		r.deps.PersistMessage(ctx, resultMsg)

		// Check for queued user inputs between tool calls.
		if inputs := r.deps.DrainQueuedInputs(); len(inputs) > 0 {
			for _, input := range inputs {
				user := Message{Role: UserRole, Content: input}
				r.deps.AppendMessage(user)
				r.deps.PersistMessage(ctx, user)
				events <- UIUserMessageAdded{Message: user}
				events <- UIQueuedInputPromoted{}
			}
		}

		messages = r.deps.HistorySnapshot()
		// Next model call is triggered by tool results (or user intervention).
		reason = "tool_result"
	}
}

func assistantMessageFromResponse(resp modelResponse) Message {
	var contentBlocks []ApiContentBlock
	contentBlocks = append(contentBlocks, resp.thinkingBlocks...)
	if resp.textContent != "" {
		contentBlocks = append(contentBlocks, ApiContentBlock{
			Type: ApiTextContentType,
			Text: resp.textContent,
		})
	}
	for _, tc := range resp.toolCalls {
		contentBlocks = append(contentBlocks, ApiContentBlock{
			Type:    ApiToolUseContentType,
			ToolUse: &tc,
		})
	}

	return Message{
		Role:          AssistantRole,
		Content:       resp.textContent,
		ContentBlocks: contentBlocks,
	}
}
