package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/zhanglvtao/cece/internal/tool"
)

// ToolResultPolicy is a legacy type kept for interface compatibility.
// Tool results are no longer truncated — the LLM manages its own context.
type ToolResultPolicy struct {
	InlineMaxLines int
	HeadLines      int
	TailLines      int
}

// NormalizeToolResultPolicy is a legacy function kept for interface compatibility.
func NormalizeToolResultPolicy(policy ToolResultPolicy) ToolResultPolicy { return policy }

// ToolExecutor runs requested tools and converts their results back to model content blocks.
type ToolExecutor struct {
	registry       *tool.Registry
	planState      *tool.PlanModeState
	taskList       *tool.TaskList
	answerProvider func() []tool.QuestionAnswer
}

func NewToolExecutor(registry *tool.Registry, planState *tool.PlanModeState, taskList *tool.TaskList, _ ToolResultPolicy, answerProvider func() []tool.QuestionAnswer) *ToolExecutor {
	return &ToolExecutor{
		registry:       registry,
		planState:      planState,
		taskList:       taskList,
		answerProvider: answerProvider,
	}
}

// ExecuteBatch runs safe tool calls in parallel, using non-concurrent tools as barriers.
// Returns tool_result content blocks to be appended to the conversation.
func (e *ToolExecutor) ExecuteBatch(ctx context.Context, calls []ApiToolUseBlock, ch chan<- Event) []ApiContentBlock {
	type execResult struct {
		index  int
		result tool.Result
	}

	execMode := tool.PermissionModeDefault
	if e.planState != nil {
		execMode = e.planState.Mode()
	}
	hasEnterPlanMode := false
	for _, call := range calls {
		if call.Name == tool.EnterPlanModeToolName {
			hasEnterPlanMode = true
			break
		}
	}

	executeOne := func(idx int, c ApiToolUseBlock) execResult {
		emitter := &chanEmitter{ch: ch, id: c.ID}
		slog.Info("tool: executing", "name", c.Name, "id", c.ID)
		emitToolEvent(ch, ToolExecStarted{ID: c.ID, Name: c.Name})
		var result tool.Result
		if c.Name == tool.AskUserQuestionToolName {
			answers := []tool.QuestionAnswer(nil)
			if e.answerProvider != nil {
				answers = e.answerProvider()
			}
			answerBytes, _ := json.Marshal(map[string]any{"answers": answers})
			result = tool.Result{Content: string(answerBytes)}
		} else {
			denied := false
			result, denied = e.permissionDeniedResult(c.Name, execMode, c.Input)
			if !denied && hasEnterPlanMode && c.Name != tool.EnterPlanModeToolName {
				result = tool.Result{Content: "EnterPlanMode must be handled before other tool calls. Continue planning with read-only tools after plan mode is active.", IsError: true}
				denied = true
			}
			if !denied {
				result = e.registry.Execute(ctx, c.Name, c.Input, emitter)
			}
		}
		slog.Info("tool: completed", "name", c.Name, "id", c.ID, "isError", result.IsError, "len", len(result.Content))
		emitToolEvent(ch, ToolExecCompleted{ID: c.ID, Name: c.Name, Result: result})
		return execResult{index: idx, result: result}
	}

	runConcurrent := func(start, end int, resultMap map[int]tool.Result) {
		results := make(chan execResult, end-start)
		for i := start; i < end; i++ {
			go func(idx int, c ApiToolUseBlock) {
				results <- executeOne(idx, c)
			}(i, calls[i])
		}
		for i := start; i < end; i++ {
			r := <-results
			resultMap[r.index] = r.result
		}
	}

	resultMap := make(map[int]tool.Result, len(calls))
	for i := 0; i < len(calls); {
		if canRunToolConcurrently(calls[i]) {
			start := i
			for i < len(calls) && canRunToolConcurrently(calls[i]) {
				i++
			}
			runConcurrent(start, i, resultMap)
			continue
		}
		r := executeOne(i, calls[i])
		resultMap[r.index] = r.result
		i++
	}

	// Check if task list was updated during execution.
	if e.taskList != nil {
		hasTaskCall := false
		for _, call := range calls {
			if call.Name == tool.TodoToolName {
				hasTaskCall = true
				break
			}
		}
		if hasTaskCall {
			emitToolEvent(ch, TaskUpdated{Tasks: e.taskList.Snapshot()})
		}
	}

	// Check if mode changed during execution (e.g. ExitPlanMode).
	if e.planState != nil && e.planState.Mode() != execMode {
		newMode := e.planState.Mode()
		var msg string
		switch newMode {
		case tool.PermissionModeAutoAccept:
			msg = "Auto-accept mode"
		case tool.PermissionModePlan:
			msg = "Entered plan mode"
		default:
			msg = "Default mode"
		}
		emitToolEvent(ch, ModeChangedDuringExec{Mode: newMode, Message: msg})
	}

	blocks := make([]ApiContentBlock, len(calls))
	for i, call := range calls {
		result := resultMap[i]
		totalLines := countLines(result.Content)
		blocks[i] = ApiContentBlock{
			Type: ApiToolResultContentType,
			ToolResult: &ApiToolResultBlock{
				ToolUseID:  call.ID,
				Content:    result.Content,
				IsError:    result.IsError,
				Truncated:  result.Truncated,
				TotalLines: totalLines,
			},
		}
	}
	return blocks
}

func canRunToolConcurrently(call ApiToolUseBlock) bool {
	switch call.Name {
	case "Read", "Glob", "Grep", "WebFetch":
		return true
	case tool.AgentToolName:
		var params struct {
			Operation string `json:"operation"`
		}
		if err := json.Unmarshal(call.Input, &params); err != nil {
			return false
		}
		operation := strings.TrimSpace(params.Operation)
		return operation == "" || operation == "start"
	default:
		return false
	}
}

type chanEmitter struct {
	ch chan<- Event
	id string
}

func (e *chanEmitter) Emit(text string) {
	if e == nil || e.ch == nil {
		return
	}
	e.ch <- ToolExecDelta{ID: e.id, Text: text}
}

func emitToolEvent(ch chan<- Event, ev Event) {
	if ch == nil {
		return
	}
	ch <- ev
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	n := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		n++
	}
	return n
}

func (e *ToolExecutor) permissionDeniedResult(name string, mode tool.PermissionMode, input json.RawMessage) (tool.Result, bool) {
	if mode != tool.PermissionModePlan {
		return tool.Result{}, false
	}
	if name == tool.EnterPlanModeToolName || name == tool.ExitPlanModeToolName || name == tool.AskUserQuestionToolName {
		return tool.Result{}, false
	}
	t, ok := e.registry.Get(name)
	if !ok {
		return tool.Result{Content: fmt.Sprintf("unknown tool: %s", name), IsError: true}, true
	}
	if tool.EffectOf(t) == tool.EffectWrite {
		if e.isPlanModeAllowedWrite(input) {
			return tool.Result{}, false
		}
		return tool.Result{Content: fmt.Sprintf("Tool %s is not allowed in plan mode. Write-effect tools must target allowed plan-mode paths only: %s. Continue read-only exploration or call ExitPlanMode with a plan.", name, e.allowedPlanModeWriteLabels()), IsError: true}, true
	}
	return tool.Result{}, false
}

func (e *ToolExecutor) isPlanModeAllowedWrite(input json.RawMessage) bool {
	return isPlanModeAllowedWriteInput(e.planState, input)
}

func isPlanModeAllowedWriteInput(planState *tool.PlanModeState, input json.RawMessage) bool {
	var p struct {
		Path string `json:"path"`
	}
	if planState == nil {
		return false
	}
	if err := json.Unmarshal(input, &p); err != nil || p.Path == "" {
		return false
	}
	return planState.IsPlanModeWriteAllowed(p.Path)
}

func (e *ToolExecutor) allowedPlanModeWriteLabels() string {
	if e.planState == nil {
		return ""
	}
	return strings.Join(e.planState.PlanModeAllowedWriteLabels(), ", ")
}
