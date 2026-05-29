package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"cece/internal/tool"
)

type ToolResultPolicy struct {
	InlineMaxLines int
	HeadLines      int
	TailLines      int
}

// ToolExecutor runs requested tools and converts their results back to model content blocks.
type ToolExecutor struct {
	registry       *tool.Registry
	planState      *tool.PlanModeState
	taskList       *tool.TaskList
	resultPolicy   ToolResultPolicy
	answerProvider func() []tool.QuestionAnswer
}

func NewToolExecutor(registry *tool.Registry, planState *tool.PlanModeState, taskList *tool.TaskList, policy ToolResultPolicy, answerProvider func() []tool.QuestionAnswer) *ToolExecutor {
	return &ToolExecutor{
		registry:       registry,
		planState:      planState,
		taskList:       taskList,
		resultPolicy:   NormalizeToolResultPolicy(policy),
		answerProvider: answerProvider,
	}
}

// ExecuteBatch runs tool calls in parallel, emitting progress events to ch.
// Returns tool_result content blocks to be appended to the conversation.
func (e *ToolExecutor) ExecuteBatch(ctx context.Context, calls []ApiToolUseBlock, ch chan<- Event) []ApiContentBlock {
	type execResult struct {
		index  int
		result tool.Result
	}

	results := make(chan execResult, len(calls))
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

	for i, call := range calls {
		go func(idx int, c ApiToolUseBlock) {
			emitter := &chanEmitter{ch: ch, id: c.ID}
			ch <- ToolExecStarted{ID: c.ID, Name: c.Name}
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
			ch <- ToolExecCompleted{ID: c.ID, Name: c.Name, Result: result}
			results <- execResult{index: idx, result: result}
		}(i, call)
	}

	resultMap := make(map[int]tool.Result, len(calls))
	for range calls {
		r := <-results
		resultMap[r.index] = r.result
	}

	// Check if task list was updated during execution.
	if e.taskList != nil {
		hasTaskCall := false
		for _, call := range calls {
			if call.Name == tool.TaskToolName {
				hasTaskCall = true
				break
			}
		}
		if hasTaskCall {
			ch <- TaskUpdated{Tasks: e.taskList.Snapshot()}
		}
	}

	blocks := make([]ApiContentBlock, len(calls))
	for i, call := range calls {
		result := resultMap[i]
		content, truncated, totalLines := truncateToolResult(result.Content, e.resultPolicy)
		blocks[i] = ApiContentBlock{
			Type: ApiToolResultContentType,
			ToolResult: &ApiToolResultBlock{
				ToolUseID:  call.ID,
				Content:    content,
				IsError:    result.IsError,
				Truncated:  truncated,
				TotalLines: totalLines,
			},
		}
	}
	return blocks
}

func NormalizeToolResultPolicy(policy ToolResultPolicy) ToolResultPolicy {
	if policy.InlineMaxLines <= 0 {
		policy.InlineMaxLines = 200
	}
	if policy.HeadLines <= 0 {
		policy.HeadLines = 80
	}
	if policy.TailLines <= 0 {
		policy.TailLines = 80
	}
	return policy
}

// chanEmitter adapts an event channel to the tool.Emitter interface.
type chanEmitter struct {
	ch chan<- Event
	id string
}

func (e *chanEmitter) Emit(text string) {
	e.ch <- ToolExecDelta{ID: e.id, Text: text}
}

func truncateToolResult(content string, policy ToolResultPolicy) (string, bool, int) {
	if content == "" {
		return "", false, 0
	}
	lines := strings.Split(content, "\n")
	totalLines := len(lines)
	if totalLines <= policy.InlineMaxLines || policy.HeadLines+policy.TailLines >= totalLines {
		return content, false, totalLines
	}

	head := lines[:policy.HeadLines]
	tail := lines[totalLines-policy.TailLines:]
	omitted := totalLines - policy.HeadLines - policy.TailLines
	parts := make([]string, 0, len(head)+len(tail)+1)
	parts = append(parts, head...)
	parts = append(parts, fmt.Sprintf("... [%d lines omitted] ...", omitted))
	parts = append(parts, tail...)
	return strings.Join(parts, "\n"), true, totalLines
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
		// Allow writes that target the plans directory.
		if e.isPlansDirWrite(name, input) {
			return tool.Result{}, false
		}
		plansDir := e.plansDir()
		return tool.Result{Content: fmt.Sprintf("Tool %s is not allowed in plan mode. Write-effect tools must target %s/ only. Continue read-only exploration or call ExitPlanMode with a plan.", name, plansDir), IsError: true}, true
	}
	return tool.Result{}, false
}

// isPlansDirWrite checks whether a write-effect tool targets a path under the plans directory.
func (e *ToolExecutor) isPlansDirWrite(name string, input json.RawMessage) bool {
	return isPlansDirWriteInput(e.plansDir(), input)
}

func isPlansDirWriteInput(plansDir string, input json.RawMessage) bool {
	var p struct {
		Path string `json:"path"`
	}
	if plansDir == "" {
		return false
	}
	if err := json.Unmarshal(input, &p); err != nil || p.Path == "" {
		return false
	}
	abs, err := filepath.Abs(p.Path)
	if err != nil {
		return false
	}
	return strings.HasPrefix(abs+string(os.PathSeparator), plansDir+string(os.PathSeparator)) || abs == plansDir
}

func (e *ToolExecutor) plansDir() string {
	if e.planState == nil {
		return ""
	}
	return e.planState.PlansDir()
}
