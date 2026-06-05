package agent

import (
	"context"
	"log/slog"
	"time"

	"cece/internal/prompt"
	"cece/internal/tool"
)

type TurnPlan struct {
	Messages       []Message
	System         SystemPrompt          // 给 API 的 system blocks
	AssembleResult prompt.AssembleResult // 原始组装结果，供 dryrun 使用
	Tools          []tool.Definition     // 工具定义（含 InputSchema）
}

// TurnDeps contains the Runtime-owned operations a turn needs while keeping
// the agent loop outside the Runtime facade.
type TurnDeps struct {
	AppendMessage        func(Message)
	PersistMessage       func(context.Context, Message)
	UpdateSessionMeta    func(context.Context, modelResponse)
	DrainQueuedInputs    func() []string
	DrainModeReminder    func() string
	DrainNudgeReminder   func() string
	HistorySnapshot      func() []Message
	ResetQuestionAnswers func()
	IncrementAPICalls    func()
	IncrementToolCount   func(name string)
	UpdateCacheTokens    func(read, creation int)
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

func (r *TurnRunner) Run(ctx context.Context, plan TurnPlan, events chan<- Event) {
	// Agent loop: keep calling the model until it stops requesting tools.
	messages := plan.Messages
	turnStart := time.Now()
	reason := "user"
	var toolResultNames []string
	for {
		r.deps.IncrementAPICalls()

		resp, err := r.streamer.Stream(ctx, ModelStreamRequest{
			Messages:    messages,
			System:      plan.System,
			Reason:      reason,
			MaxTokens:   r.maxTokens,
			ToolResults: toolResultNames,
		}, events)
		if err != nil {
			events <- RunFailed{Err: err}
			return
		}

		// Silent escalation: if output was truncated, retry once with 64K.
		if resp.stopReason == "max_tokens" {
			events <- TruncationRetry{
				Attempt:       1,
				PrevMaxTokens: r.maxTokens,
				NewMaxTokens:  escalatedMaxTokens,
			}
			slog.Info("output truncated, escalating max_tokens", "from", r.maxTokens, "to", escalatedMaxTokens)
			r.deps.IncrementAPICalls()
			resp, err = r.streamer.Stream(ctx, ModelStreamRequest{
				Messages:    messages,
				System:      plan.System,
				Reason:      reason,
				MaxTokens:   escalatedMaxTokens,
				ToolResults: toolResultNames,
			}, events)
			if err != nil {
				events <- RunFailed{Err: err}
				return
			}
		}

		// Update cache token counters from this stream response.
		if resp.cacheReadTokens > 0 || resp.cacheCreationTokens > 0 {
			r.deps.UpdateCacheTokens(resp.cacheReadTokens, resp.cacheCreationTokens)
		}

		assistant := assistantMessageFromResponse(resp)
		r.deps.AppendMessage(assistant)
		r.deps.PersistMessage(ctx, assistant)
		r.deps.UpdateSessionMeta(ctx, resp)

		// No tool calls -- conversation turn is done.
		if resp.stopReason != "tool_use" || len(resp.toolCalls) == 0 {
			events <- AssistantCompleted{Duration: time.Since(turnStart)}
			return
		}

		if err := r.interactionGate.WaitIfNeeded(ctx, resp.toolCalls, events); err != nil {
			events <- RunFailed{Err: err}
			return
		}

		toolResults := r.toolExecutor.ExecuteBatch(ctx, resp.toolCalls, events)
		for _, tc := range resp.toolCalls {
			r.deps.IncrementToolCount(tc.Name)
		}
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
			// Insert a standalone reminder before the first queued input
			// so the LLM can decide whether to interrupt its current task.
			if reason == "tool_result" {
				reminder := Message{Role: UserRole, Content: QueuedInputReminder}
				r.deps.AppendMessage(reminder)
				r.deps.PersistMessage(ctx, reminder)
			}
			for _, input := range inputs {
				user := Message{Role: UserRole, Content: input}
				r.deps.AppendMessage(user)
				r.deps.PersistMessage(ctx, user)
				events <- UserMessageAdded{Message: user}
				events <- QueuedInputPromoted{}
			}
		}

		messages = r.deps.HistorySnapshot()
		// Inject mode change reminder if pending.
		if r.deps.DrainModeReminder != nil {
			if reminder := r.deps.DrainModeReminder(); reminder != "" {
				messages = append(messages, Message{Role: UserRole, Content: reminder})
			}
		}
		// Inject context-pressure nudge if needed.
		if r.deps.DrainNudgeReminder != nil {
			if nudge := r.deps.DrainNudgeReminder(); nudge != "" {
				messages = append(messages, Message{Role: UserRole, Content: nudge})
			}
		}
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
