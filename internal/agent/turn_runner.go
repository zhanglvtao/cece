package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"time"

	"github.com/zhanglvtao/cece/internal/diag"

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
	AppendMessage           func(Message)
	PersistMessage          func(context.Context, Message)
	UpdateSessionMeta       func(context.Context, modelResponse)
	DrainQueuedInputs       func() []string
	DrainModeReminder       func() string
	TryAutoCompact          func(ctx context.Context) bool
	EnsureContextBudget     func(ctx context.Context, targetTokens int) bool
	MaybeInjectContextNudge func(snapshot []Message) ([]Message, bool, int, int, int, int)
	HistorySnapshot         func() []Message
	ResetQuestionAnswers    func()
	IncrementAPICalls       func()
	RecordToolExecution     func(name string, isError bool)
	UpdateCacheTokens       func(read, creation int)
	ContextWindow           int
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
	loopIter := 0
	for {
		loopIter++
		messages = r.maybeInjectContextNudge(messages, events)
		prepared, err := r.prepareModelStreamRequest(ctx, messages, plan.System, r.maxTokens)
		if err != nil {
			events <- RunFailed{Err: err}
			return
		}
		messages = prepared.messages
		r.deps.IncrementAPICalls()

		diag.Log("turn_runner: calling streamer.Stream() loop_iter=%d reason=%q messages=%d", loopIter, reason, len(messages))
		resp, err := r.streamer.Stream(ctx, ModelStreamRequest{
			Messages:             messages,
			System:               plan.System,
			Reason:               reason,
			MaxTokens:            prepared.maxTokens,
			ContextWindow:        r.deps.ContextWindow,
			ToolResults:          toolResultNames,
			EstimatedInputTokens: prepared.estimatedInputTokens,
			ReserveTokens:        prepared.reserveTokens,
			UnderestimateP95:     prepared.underestimateP95,
			AvailableMaxTokens:   prepared.availableMaxTokens,
			BudgetFits:           prepared.budgetFits,
			ManagementTriggered:  prepared.managementTriggered,
		}, events)
		diag.Log("turn_runner: streamer.Stream() returned err=%v resp_text=%q tool_calls=%d", err, resp.textContent, len(resp.toolCalls))
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
			prepared, err := r.prepareModelStreamRequest(ctx, messages, plan.System, escalatedMaxTokens)
			if err != nil {
				events <- RunFailed{Err: err}
				return
			}
			messages = prepared.messages
			events <- TruncationRetry{
				Attempt:       1,
				PrevMaxTokens: r.maxTokens,
				NewMaxTokens:  prepared.maxTokens,
			}
			slog.Info("output truncated, escalating max_tokens", "from", r.maxTokens, "to", prepared.maxTokens)
			r.deps.IncrementAPICalls()
			resp, err = r.streamer.Stream(ctx, ModelStreamRequest{
				Messages:             messages,
				System:               plan.System,
				Reason:               reason,
				MaxTokens:            prepared.maxTokens,
				ContextWindow:        r.deps.ContextWindow,
				ToolResults:          toolResultNames,
				EstimatedInputTokens: prepared.estimatedInputTokens,
				ReserveTokens:        prepared.reserveTokens,
				UnderestimateP95:     prepared.underestimateP95,
				AvailableMaxTokens:   prepared.availableMaxTokens,
				BudgetFits:           prepared.budgetFits,
				ManagementTriggered:  prepared.managementTriggered,
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
					"input_tokens", resp.inputTokens,
					"output_tokens", resp.outputTokens,
				)
				events <- RunFailed{Err: fmt.Errorf("model returned empty response %d consecutive times (input_tokens=%d, output_tokens=%d, stop_reason=%q)", consecutiveEmptyResponses, resp.inputTokens, resp.outputTokens, resp.stopReason)}
				return
			}
			slog.Warn("model returned empty response — injecting retry nudge",
				"stop_reason", resp.stopReason,
				"input_tokens", resp.inputTokens,
				"output_tokens", resp.outputTokens,
				"attempt", consecutiveEmptyResponses,
			)
			// Don't persist the nudge — it creates consecutive assistant
			// messages that break aiden proxy's Responses API conversion.
			messages = append(messages, Message{
				Role:    AssistantRole,
				Content: "[Empty response — retrying]",
			})
			continue
		}
		consecutiveEmptyResponses = 0

		r.deps.AppendMessage(assistant)
		r.deps.PersistMessage(ctx, assistant)
		r.deps.UpdateSessionMeta(ctx, resp)

		// No tool calls -- either promote queued input first, or finish the turn.
		if resp.stopReason != "tool_use" || len(resp.toolCalls) == 0 {
			if r.promoteQueuedInputs(ctx, events, reason) {
				r.tryAutoCompact(ctx)
				messages = r.deps.HistorySnapshot()
				reason = "user"
				toolResultNames = nil
				continue
			}
			// Run end-of-turn context management even when the turn ends with a
			// plain-text answer (no tool calls). Without this, a turn that finishes
			// via text skips the compaction that every tool-execution path performs.
			r.tryAutoCompact(ctx)
			events <- AssistantCompleted{Duration: time.Since(turnStart)}
			return
		}

		if err := r.interactionGate.WaitIfNeeded(ctx, resp.toolCalls, events); err != nil {
			if errors.Is(err, WaitRejected) {
				if hasExitPlanMode(resp.toolCalls) {
					resultMsg := Message{
						Role:          ToolRole,
						ContentBlocks: rejectToolResults(resp.toolCalls),
					}
					r.deps.AppendMessage(resultMsg)
					r.deps.PersistMessage(context.Background(), resultMsg)
					r.tryAutoCompact(ctx)
					events <- PlanRejected{}
					events <- AssistantCompleted{Duration: time.Since(turnStart)}
					return
				}
				// User rejected: construct rejection tool_results and continue the loop.
				events <- ToolCallsRejected{}
				resultMsg := Message{
					Role:          ToolRole,
					ContentBlocks: rejectToolResults(resp.toolCalls),
				}
				r.deps.AppendMessage(resultMsg)
				r.deps.PersistMessage(ctx, resultMsg)
				r.tryAutoCompact(ctx)
				messages = r.deps.HistorySnapshot()
				reason = "tool_result"
				continue
			}
			events <- RunFailed{Err: err}
			return
		}

		toolResults := r.toolExecutor.ExecuteBatch(ctx, resp.toolCalls, events)
		for i, tc := range resp.toolCalls {
			isErr := i < len(toolResults) && toolResults[i].ToolResult != nil && toolResults[i].ToolResult.IsError
			r.deps.RecordToolExecution(tc.Name, isErr)
		}
		toolResultNames = make([]string, len(resp.toolCalls))
		for i, tc := range resp.toolCalls {
			toolResultNames[i] = tc.Name
		}

		// Append tool_result as a tool message.
		resultMsg := Message{
			Role:          ToolRole,
			ContentBlocks: toolResults,
		}
		r.deps.AppendMessage(resultMsg)
		r.deps.PersistMessage(ctx, resultMsg)

		approvedPlanContinuation := ""
		if approvedPlan, ok := successfulExitPlanModePlan(resp.toolCalls, toolResults); ok {
			approvedPlanContinuation = tool.BuildApprovedPlanContinuation(approvedPlan)
		}

		// Check for queued user inputs between tool calls.
		r.promoteQueuedInputs(ctx, events, reason)
		r.tryAutoCompact(ctx)

		messages = r.deps.HistorySnapshot()
		// Inject mode change reminder if pending.
		if r.deps.DrainModeReminder != nil {
			if reminder := r.deps.DrainModeReminder(); reminder != "" {
				messages = append(messages, Message{Role: UserRole, Content: reminder})
			}
		}
		if approvedPlanContinuation != "" {
			messages = append(messages, Message{Role: UserRole, Content: approvedPlanContinuation})
		}
		// Inject context-pressure nudge if needed.
		// (removed: agentic loop no longer injects nudge; autoCompact handles high context)
		// Next model call is triggered by tool results (or user intervention).
		reason = "tool_result"
	}
}

const contextBudgetSafetyMargin = 1024
const minContextBudgetMaxTokens = 1024

type preparedModelRequest struct {
	messages             []Message
	maxTokens            int
	estimatedInputTokens int
	reserveTokens        int
	underestimateP95     int
	availableMaxTokens   int
	budgetFits           bool
	managementTriggered  bool
}

func (r *TurnRunner) prepareModelStreamRequest(ctx context.Context, messages []Message, system SystemPrompt, requestedMaxTokens int) (preparedModelRequest, error) {
	if requestedMaxTokens <= 0 || r.deps.ContextWindow <= 0 {
		prepared := r.applyToolUseFallback(messages, system)
		estimated := EstimateRequestTokens(system, prepared, r.toolDefinitions())
		return preparedModelRequest{messages: prepared, maxTokens: requestedMaxTokens, estimatedInputTokens: estimated}, nil
	}

	tools := r.toolDefinitions()
	preparedMessages := messages
	managementTriggered := false
	budget := r.computeReserveBudget(system, preparedMessages, requestedMaxTokens, tools)
	if !budget.Fits {
		slog.Warn("context reserve preflight exceeded", "estimated_input", budget.EstimatedInputTokens, "reserve", budget.ReserveTokens, "context_window", r.deps.ContextWindow)
		managementTriggered = r.ensureContextBudget(ctx, budget.ReserveTokens)
		if refreshed, ok := r.refreshedHistorySnapshot(messages); ok {
			preparedMessages = refreshed
			budget = r.computeReserveBudget(system, preparedMessages, requestedMaxTokens, tools)
		}
	}

	preparedMessages = r.applyToolUseFallback(preparedMessages, system)
	budget = r.computeReserveBudget(system, preparedMessages, requestedMaxTokens, tools)
	if budget.Fits {
		return preparedModelRequest{messages: preparedMessages, maxTokens: requestedMaxTokens, estimatedInputTokens: budget.EstimatedInputTokens, reserveTokens: budget.ReserveTokens, underestimateP95: budget.UnderestimateP95, availableMaxTokens: budget.AvailableMaxTokens, budgetFits: true, managementTriggered: managementTriggered}, nil
	}

	available := budget.AvailableMaxTokens
	if available < minContextBudgetMaxTokens {
		slog.Warn("context budget extremely tight — using minimum max_tokens", "estimated_input", budget.EstimatedInputTokens, "reserve", budget.ReserveTokens, "context_window", r.deps.ContextWindow)
		requestedMaxTokens = minContextBudgetMaxTokens
	} else if available < requestedMaxTokens {
		slog.Warn("shrinking max_tokens to fit reserve budget", "from", requestedMaxTokens, "to", available, "estimated_input", budget.EstimatedInputTokens, "reserve", budget.ReserveTokens, "context_window", r.deps.ContextWindow)
		requestedMaxTokens = available
	}
	return preparedModelRequest{messages: preparedMessages, maxTokens: requestedMaxTokens, estimatedInputTokens: budget.EstimatedInputTokens, reserveTokens: budget.ReserveTokens, underestimateP95: budget.UnderestimateP95, availableMaxTokens: budget.AvailableMaxTokens, budgetFits: false, managementTriggered: managementTriggered}, nil
}

func fitsContextBudget(estimatedInput, maxTokens, contextWindow int) bool {
	if contextWindow <= 0 || maxTokens <= 0 {
		return true
	}
	return estimatedInput+maxTokens+contextBudgetSafetyMargin <= contextWindow
}

func (r *TurnRunner) computeReserveBudget(system SystemPrompt, messages []Message, requestedMaxTokens int, tools []tool.Definition) ReserveBudget {
	estimated := EstimateRequestTokens(system, messages, tools)
	underestimateP95 := 0
	if r.streamer != nil && r.streamer.underestimateStats != nil {
		underestimateP95 = r.streamer.underestimateStats.P95()
	}
	return ComputeReserveBudget(ReserveBudgetInput{
		EstimatedInputTokens: estimated,
		RequestedMaxTokens:   requestedMaxTokens,
		ModelMaxOutput:       requestedMaxTokens,
		ContextWindow:        r.deps.ContextWindow,
		ReserveRatio:         defaultReserveRatio,
		UnderestimateP95:     underestimateP95,
	})
}

func (r *TurnRunner) ensureContextBudget(ctx context.Context, targetTokens int) bool {
	if r.deps.EnsureContextBudget != nil {
		return r.deps.EnsureContextBudget(ctx, targetTokens)
	}
	return r.tryAutoCompact(ctx)
}

func (r *TurnRunner) toolDefinitions() []tool.Definition {
	if r.streamer == nil || r.streamer.registry == nil {
		return nil
	}
	return r.streamer.registry.Definitions()
}

func (r *TurnRunner) refreshedHistorySnapshot(current []Message) ([]Message, bool) {
	if r.deps.HistorySnapshot == nil {
		return nil, false
	}
	refreshed := r.deps.HistorySnapshot()
	if len(refreshed) == 0 || reflect.DeepEqual(refreshed, current) {
		return nil, false
	}
	return refreshed, true
}

func (r *TurnRunner) maybeInjectContextNudge(messages []Message, events chan<- Event) []Message {
	if r.deps.MaybeInjectContextNudge == nil {
		return messages
	}
	updated, nudged, turns, pct, used, cw := r.deps.MaybeInjectContextNudge(messages)
	if !nudged {
		return updated
	}
	events <- ContextNudged{
		TurnsSinceCompact: turns,
		ContextPct:        pct,
		ContextUsed:       used,
		ContextWindow:     cw,
	}
	return updated
}

func (r *TurnRunner) tryAutoCompact(ctx context.Context) bool {
	if r.deps.TryAutoCompact == nil {
		return false
	}
	return r.deps.TryAutoCompact(ctx)
}

func (r *TurnRunner) applyToolUseFallback(messages []Message, system SystemPrompt) []Message {
	if r.deps.ContextWindow <= 0 {
		return messages
	}
	tools := r.toolDefinitions()
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

func successfulExitPlanModePlan(calls []ApiToolUseBlock, results []ApiContentBlock) (string, bool) {
	for i, call := range calls {
		if call.Name != tool.ExitPlanModeToolName || i >= len(results) {
			continue
		}
		tr, ok := results[i].AsToolResult()
		if !ok || tr.IsError {
			continue
		}
		plan := approvedPlanFromExitResult(tr.Content)
		if strings.TrimSpace(plan) == "" {
			continue
		}
		return plan, true
	}
	return "", false
}

func approvedPlanFromExitResult(content string) string {
	idx := strings.Index(content, tool.ApprovedPlanResultHeading)
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(content[idx+len(tool.ApprovedPlanResultHeading):])
}

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
