package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

const TaskToolName = "Task"

// TaskStatus represents the status of a task item.
type TaskStatus string

const (
	TaskPending    TaskStatus = "pending"
	TaskInProgress TaskStatus = "in_progress"
	TaskCompleted  TaskStatus = "completed"
)

// TaskItem represents a single task in the task list.
type TaskItem struct {
	Content    string     `json:"content"`
	ActiveForm string     `json:"activeForm"`
	Status     TaskStatus `json:"status"`
}

// TaskList holds the current set of tasks with version tracking for change detection.
type TaskList struct {
	mu      sync.Mutex
	items   []TaskItem
	version int
}

// NewTaskList creates an empty TaskList.
func NewTaskList() *TaskList {
	return &TaskList{}
}

// Replace replaces the task list with new items and increments the version.
func (t *TaskList) Replace(items []TaskItem) (int, int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	oldVersion := t.version
	t.items = items
	t.version++
	return oldVersion, t.version
}

// Snapshot returns a copy of the current task items.
func (t *TaskList) Snapshot() []TaskItem {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]TaskItem, len(t.items))
	copy(out, t.items)
	return out
}

// Version returns the current version number.
func (t *TaskList) Version() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.version
}

// ── Task Tool ──────────────────────────────────────────────────────────────

type taskTool struct {
	taskList *TaskList
}

// NewTask creates a Task tool backed by the given TaskList.
func NewTask(taskList *TaskList) Tool {
	return taskTool{taskList: taskList}
}

func (taskTool) Effect() Effect { return EffectMode }

func (taskTool) Info() Definition {
	return Definition{
		Name:        TaskToolName,
		Description: "Update the task list for the current session. Use this tool proactively to track progress on complex multi-step tasks (3+ steps). Each task has content (imperative form, e.g. 'Fix auth bug') and activeForm (present continuous, e.g. 'Fixing auth bug'). Only one task should be in_progress at a time. Mark tasks completed immediately after finishing. Do NOT use this for simple single-step tasks.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"todos"},
			"properties": map[string]any{
				"todos": map[string]any{
					"type":        "array",
					"description": "The updated task list. Pass the FULL list every time (not just changes). An empty list clears all tasks.",
					"items": map[string]any{
						"type":     "object",
						"required": []string{"content", "activeForm", "status"},
						"properties": map[string]any{
							"content": map[string]any{
								"type":        "string",
								"description": "Imperative form describing what needs to be done (e.g., 'Run tests', 'Build the project')",
							},
							"activeForm": map[string]any{
								"type":        "string",
								"description": "Present continuous form shown during execution (e.g., 'Running tests', 'Building the project')",
							},
							"status": map[string]any{
								"type":        "string",
								"enum":        []string{"pending", "in_progress", "completed"},
								"description": "Task status: pending (not started), in_progress (currently working), completed (done)",
							},
						},
					},
				},
			},
		},
	}
}

func (t taskTool) Run(ctx context.Context, input json.RawMessage, emitter Emitter) Result {
	if t.taskList == nil {
		return Result{Content: "task list is not configured", IsError: true}
	}

	var parsed struct {
		Todos []TaskItem `json:"todos"`
	}
	if err := json.Unmarshal(input, &parsed); err != nil {
		return Result{Content: fmt.Sprintf("invalid input: %v", err), IsError: true}
	}

	// Validate statuses
	for i, item := range parsed.Todos {
		switch item.Status {
		case TaskPending, TaskInProgress, TaskCompleted:
		default:
			return Result{Content: fmt.Sprintf("invalid status %q at index %d, must be pending/in_progress/completed", item.Status, i), IsError: true}
		}
	}

	// If all completed, clear the list (auto-dismiss)
	allDone := len(parsed.Todos) > 0
	for _, item := range parsed.Todos {
		if item.Status != TaskCompleted {
			allDone = false
			break
		}
	}
	if allDone {
		parsed.Todos = nil
	}

	t.taskList.Replace(parsed.Todos)

	if len(parsed.Todos) == 0 {
		return Result{Content: "Tasks updated. All tasks completed — list cleared."}
	}

	// Summarize current state
	var pending, inProgress, completed int
	for _, item := range parsed.Todos {
		switch item.Status {
		case TaskPending:
			pending++
		case TaskInProgress:
			inProgress++
		case TaskCompleted:
			completed++
		}
	}
	return Result{Content: fmt.Sprintf("Tasks updated: %d pending, %d in_progress, %d completed.", pending, inProgress, completed)}
}
