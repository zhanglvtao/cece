package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/zhanglvtao/cece/internal/prompt"
	"github.com/zhanglvtao/cece/internal/tool"
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
	TryAutoCompact       func(ctx context.Context) bool
	HistorySnapshot      func() []Message
	ResetQuestionAnswers func()
	IncrementAPICalls    func()
	IncrementToolCount   func(name string)
	UpdateCacheTokens    func(read, creation int)
	ContextWindow        int
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
	consecutiveEmptyResponses := 0
	const maxEmptyRetries = 3
	for {
		messages = r.applyToolUseFallback(messages, plan.System)
		r.deps.IncrementAPICalls()

		resp, err := r.streamer.Stream(ctx, ModelStreamRequest{
			Messages:    messages,
			System:      plan.System,
			Reason:      reason,
			MaxTokens:   r.maxTokens,
			ToolResults: toolResultNames,
		}, events)
		if err != nil {
			if ctx.Err() != nil {
				// Context cancelled (user interrupted): insert interrupt message
				// so the LLM can see it on the next turn.
				interruptMsg := Message{Role: UserRole, Content: "[Request interrupted by user]"}
				r.deps.AppendMessage(interruptMsg)
				// Best-effort persist; ctx is already cancelled so we use background.
				r.deps.PersistMessage(context.Background(), interruptMsg)
			}
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
			messages = r.applyToolUseFallback(messages, plan.System)
			r.deps.IncrementAPICalls()
			resp, err = r.streamer.Stream(ctx, ModelStreamRequest{
				Messages:    messages,
				System:      plan.System,
				Reason:      reason,
				MaxTokens:   escalatedMaxTokens,
				ToolResults: toolResultNames,
			}, events)
			if err != nil {
				if ctx.Err() != nil {
					interruptMsg := Message{Role: UserRole, Content: "[Request interrupted by user]"}
					r.deps.AppendMessage(interruptMsg)
					r.deps.PersistMessage(context.Background(), interruptMsg)
				}
				events <- RunFailed{Err: err}
				return
			}
		}

		// Update cache token counters from this stream response.
		if resp.cacheReadTokens > 0 || resp.cacheCreationTokens > 0 {
			r.deps.UpdateCacheTokens(resp.cacheReadTokens, resp.cacheCreationTokens)
		}

		assistant := assistantMessageFromResponse(resp)

		// If the model returned a completely empty response (no text, no
		// tool calls, no thinking), the API may have silently dropped the
		// output. Don't persist an empty assistant message — it causes
		// consecutive user messages on the next turn, which confuses the
		// model. Instead, inject a retry nudge and continue the loop.
		if resp.textContent == "" && len(resp.toolCalls) == 0 && len(resp.thinkingBlocks) == 0 {
			consecutiveEmptyResponses++
			if consecutiveEmptyResponses >= maxEmptyRetries {
				slog.Warn("model returned empty response too many times — stopping",
					"consecutive_empty", consecutiveEmptyResponses,
					"stop_reason", resp.stopReason,
				)
				events <- RunFailed{Err: fmt.Errorf("model returned empty response %d consecutive times", consecutiveEmptyResponses)}
				return
			}
			slog.Warn("model returned empty response — injecting retry nudge",
				"stop_reason", resp.stopReason,
				"input_tokens", resp.inputTokens,
				"output_tokens", resp.outputTokens,
				"attempt", consecutiveEmptyResponses,
			)
			nudge := Message{
				Role:    AssistantRole,
				Content: "[Empty response — retrying]",
			}
			r.deps.AppendMessage(nudge)
			r.deps.PersistMessage(ctx, nudge)
			messages = r.deps.HistorySnapshot()
			continue
		}
		consecutiveEmptyResponses = 0

		r.deps.AppendMessage(assistant)
		r.deps.PersistMessage(ctx, assistant)
		r.deps.UpdateSessionMeta(ctx, resp)

		// Auto-compact: if context usage is >= 90%, compact automatically.
		// After compact, refresh the message snapshot so the next API call
		// uses the compacted history.
		if r.deps.TryAutoCompact != nil && r.deps.TryAutoCompact(ctx) {
			messages = r.deps.HistorySnapshot()
		}

		// No tool calls -- either promote queued input first, or finish the turn.
		if resp.stopReason != "tool_use" || len(resp.toolCalls) == 0 {
			if r.promoteQueuedInputs(ctx, events, reason) {
				messages = r.deps.HistorySnapshot()
				reason = "user"
				toolResultNames = nil
				continue
			}
			events <- AssistantCompleted{Duration: time.Since(turnStart)}
			return
		}

		if err := r.interactionGate.WaitIfNeeded(ctx, resp.toolCalls, events); err != nil {
			if errors.Is(err, WaitRejected) {
				// User rejected: construct rejection tool_results and continue the loop.
				if hasExitPlanMode(resp.toolCalls) {
					events <- PlanRejected{}
				} else {
					events <- ToolCallsRejected{}
				}
				resultMsg := Message{
					Role:          UserRole,
					ContentBlocks: rejectToolResults(resp.toolCalls),
				}
				r.deps.AppendMessage(resultMsg)
				r.deps.PersistMessage(ctx, resultMsg)
				messages = r.deps.HistorySnapshot()
				reason = "tool_result"
				continue
			}
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
		r.promoteQueuedInputs(ctx, events, reason)

		messages = r.deps.HistorySnapshot()
		// Inject mode change reminder if pending.
		if r.deps.DrainModeReminder != nil {
			if reminder := r.deps.DrainModeReminder(); reminder != "" {
				messages = append(messages, Message{Role: UserRole, Content: reminder})
			}
		}
		// Inject context-pressure nudge if needed.
		// (removed: agentic loop no longer injects nudge; autoCompact handles high context)
		// Next model call is triggered by tool results (or user intervention).
		reason = "tool_result"
	}
}

func (r *TurnRunner) applyToolUseFallback(messages []Message, system SystemPrompt) []Message {
	if r.deps.ContextWindow <= 0 {
		return messages
	}
	tools := []tool.Definition(nil)
	if r.streamer != nil && r.streamer.registry != nil {
		tools = r.streamer.registry.Definitions()
	}
	estimated := EstimateRequestTokens(system, messages, tools)
	if estimated <= r.deps.ContextWindow {
		return messages
	}

	turns := len(TurnBoundaries(messages))
	if turns <= 1 {
		return messages
	}

	truncated := cloneMessagesForRequestFallback(messages)
	totalTruncated := 0
	before := estimated
	for upToTurn := 1; upToTurn <= turns && estimated > r.deps.ContextWindow; upToTurn++ {
		count, _, _ := TruncateToolUseInputs(truncated, upToTurn)
		if count == 0 {
			continue
		}
		totalTruncated += count
		estimated = EstimateRequestTokens(system, truncated, tools)
	}
	if totalTruncated > 0 {
		slog.Warn("tool_use input fallback truncation applied", "truncated", totalTruncated, "tokens_before", before, "tokens_after", estimated, "context_window", r.deps.ContextWindow)
		return truncated
	}
	return messages
}

func cloneMessagesForRequestFallback(messages []Message) []Message {
	out := make([]Message, len(messages))
	for i, msg := range messages {
		out[i] = msg
		if len(msg.ContentBlocks) == 0 {
			continue
		}
		out[i].ContentBlocks = make([]ApiContentBlock, len(msg.ContentBlocks))
		for j, block := range msg.ContentBlocks {
			out[i].ContentBlocks[j] = block
			if block.ToolUse != nil {
				toolUse := *block.ToolUse
				if toolUse.Input != nil {
					toolUse.Input = append([]byte(nil), toolUse.Input...)
				}
				out[i].ContentBlocks[j].ToolUse = &toolUse
			}
			if block.ToolResult != nil {
				toolResult := *block.ToolResult
				out[i].ContentBlocks[j].ToolResult = &toolResult
			}
			if block.Thinking != nil {
				thinking := *block.Thinking
				out[i].ContentBlocks[j].Thinking = &thinking
			}
		}
	}
	return out
}

func (r *TurnRunner) promoteQueuedInputs(ctx context.Context, events chan<- Event, reason string) bool {
	inputs := r.deps.DrainQueuedInputs()
	if len(inputs) == 0 {
		return false
	}
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
	return true
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

// rejectToolResults builds tool_result content blocks that tell the LLM
// the user rejected the tool calls. The rejection message varies by tool type.
func rejectToolResults(calls []ApiToolUseBlock) []ApiContentBlock {
	blocks := make([]ApiContentBlock, len(calls))
	for i, call := range calls {
		var msg string
		switch call.Name {
		case tool.ExitPlanModeToolName:
			msg = "The user rejected the plan. Stay in plan mode and continue revising the plan based on the feedback."
		case tool.AskUserQuestionToolName:
			msg = "The user cancelled the question. Continue with your best judgment."
		default:
			msg = "The user rejected this tool call. Consider an alternative approach."
		}
		blocks[i] = ApiContentBlock{
			Type: ApiToolResultContentType,
			ToolResult: &ApiToolResultBlock{
				ToolUseID: call.ID,
				Content:   msg,
				IsError:   true,
			},
		}
	}
	return blocks
}
