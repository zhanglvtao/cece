package ui

import (
	"fmt"
	"strings"

	"cece/internal/protocol"
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
	return renderTaskBar(m.tasks, m.width)
}

func renderTaskBar(tasks []protocol.TaskItem, width int) string {
	var b strings.Builder
	show := tasks
	overflow := 0
	if len(tasks) > maxTaskBarLines {
		show = tasks[:maxTaskBarLines]
		overflow = len(tasks) - maxTaskBarLines
	}
	for _, t := range show {
		icon := taskStatusIcon(t.Status)
		text := t.Content
		if t.Status == "in_progress" && t.ActiveForm != "" {
			text = t.ActiveForm
		}
		line := fmt.Sprintf("%s %s", icon, text)
		if width > 0 && len(line) > width {
			line = line[:width-3] + "..."
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if overflow > 0 {
		b.WriteString(fmt.Sprintf("  ... +%d more", overflow))
	}
	return strings.TrimRight(b.String(), "\n")
}

func taskStatusIcon(status string) string {
	switch status {
	case "pending":
		return "○"
	case "in_progress":
		return "●"
	case "completed":
		return "✓"
	default:
		return "·"
	}
}
