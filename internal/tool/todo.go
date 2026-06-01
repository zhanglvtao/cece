package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

const TodoToolName = "Todo"

// TodoStatus represents the status of a todo item.
type TodoStatus string

const (
	TodoPending    TodoStatus = "pending"
	TodoInProgress TodoStatus = "in_progress"
	TodoCompleted  TodoStatus = "completed"
)

// TodoItem represents a single item in the todo list.
type TodoItem struct {
	Content    string     `json:"content"`
	ActiveForm string     `json:"activeForm"`
	Status     TodoStatus `json:"status"`
}

// TaskList holds the current set of todo items with version tracking for change detection.
type TaskList struct {
	mu      sync.Mutex
	items   []TodoItem
	version int
}

// NewTaskList creates an empty TaskList.
func NewTaskList() *TaskList {
	return &TaskList{}
}

// Replace replaces the todo list with new items and increments the version.
func (t *TaskList) Replace(items []TodoItem) (int, int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	oldVersion := t.version
	t.items = items
	t.version++
	return oldVersion, t.version
}

// Snapshot returns a copy of the current todo items.
func (t *TaskList) Snapshot() []TodoItem {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]TodoItem, len(t.items))
	copy(out, t.items)
	return out
}

// Version returns the current version number.
func (t *TaskList) Version() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.version
}

// ── Todo Tool ──────────────────────────────────────────────────────────────

type todoTool struct {
	taskList *TaskList
}

// NewTodo creates a Todo tool backed by the given TaskList.
func NewTodo(taskList *TaskList) Tool {
	return todoTool{taskList: taskList}
}

func (todoTool) Effect() Effect { return EffectMode }

func (todoTool) Info() Definition {
	return Definition{
		Name:        TodoToolName,
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

func (t todoTool) Run(ctx context.Context, input json.RawMessage, emitter Emitter) Result {
	if t.taskList == nil {
		return Result{Content: "task list is not configured", IsError: true}
	}

	var parsed struct {
		Todos []TodoItem `json:"todos"`
	}
	if err := json.Unmarshal(input, &parsed); err != nil {
		return Result{Content: fmt.Sprintf("invalid input: %v", err), IsError: true}
	}

	// Validate statuses
	for i, item := range parsed.Todos {
		switch item.Status {
		case TodoPending, TodoInProgress, TodoCompleted:
		default:
			return Result{Content: fmt.Sprintf("invalid status %q at index %d, must be pending/in_progress/completed", item.Status, i), IsError: true}
		}
	}

	// If all completed, clear the list (auto-dismiss)
	allDone := len(parsed.Todos) > 0
	for _, item := range parsed.Todos {
		if item.Status != TodoCompleted {
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
		case TodoPending:
			pending++
		case TodoInProgress:
			inProgress++
		case TodoCompleted:
			completed++
		}
	}
	return Result{Content: fmt.Sprintf("Tasks updated: %d pending, %d in_progress, %d completed.", pending, inProgress, completed)}
}
