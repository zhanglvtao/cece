package ui

import (
	"fmt"
	"strings"

	"cece/internal/protocol"
	"charm.land/lipgloss/v2"
)

const maxTaskBarLines = 6

// taskBarHeight returns the number of terminal lines the task bar occupies.
// Returns 0 when there are no tasks.
func (m *Model) taskBarHeight() int {
	if len(m.tasks) == 0 {
		return 0
	}
	n := len(m.tasks)
	if n > maxTaskBarLines {
		n = maxTaskBarLines
	}
	return n
}

// taskBarView renders the task progress panel.
func (m *Model) taskBarView() string {
	if len(m.tasks) == 0 {
		return ""
	}
	return renderTaskBar(m.tasks, m.width, m.statusFrame, m.styles)
}

func renderTaskBar(tasks []protocol.TodoItem, width int, frame int, styles Styles) string {
	var b strings.Builder
	show := tasks
	overflow := 0
	if len(tasks) > maxTaskBarLines {
		show = tasks[:maxTaskBarLines]
		overflow = len(tasks) - maxTaskBarLines
	}
	for _, t := range show {
		icon := taskStatusIcon(t.Status, frame)
		text := t.Content
		if t.Status == "in_progress" && t.ActiveForm != "" {
			text = t.ActiveForm
		}
		line := fmt.Sprintf("%s %s", icon, text)
		if width > 0 && len(line) > width {
			line = line[:width-3] + "..."
		}
		line = taskStyleFromStatus(t.Status, styles).Render(line)
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if overflow > 0 {
		b.WriteString(fmt.Sprintf("  ... +%d more", overflow))
	}
	return strings.TrimRight(b.String(), "\n")
}

func taskStyleFromStatus(status string, s Styles) lipgloss.Style {
	switch status {
	case "pending":
		return s.Task.Pending
	case "in_progress":
		return s.Task.InProgress
	case "completed":
		return s.Task.Completed
	default:
		return s.Task.Pending
	}
}

func taskStatusIcon(status string, frame int) string {
	switch status {
	case "pending":
		return "□"
	case "in_progress":
		if frame%4 < 2 {
			return "■"
		}
		return "□"
	case "completed":
		return "✓"
	default:
		return "·"
	}
}
