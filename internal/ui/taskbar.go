package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/zhanglvtao/cece/internal/protocol"
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
		return 1 + maxTaskBarLines + 1 // label + visible items + overflow
	}
	return 1 + n // label + items
}

// taskBarView renders the task progress panel.
func (m *Model) taskBarView() string {
	if len(m.tasks) == 0 {
		return ""
	}
	return renderTaskBar(m.tasks, m.width, m.statusFrame, m.styles)
}

func renderTaskBar(tasks []protocol.TodoItem, width int, frame int, styles Styles) string {
	sorted := make([]protocol.TodoItem, len(tasks))
	copy(sorted, tasks)
	sort.SliceStable(sorted, func(i, j int) bool {
		return taskStatusOrder(sorted[i].Status) < taskStatusOrder(sorted[j].Status)
	})

	var b strings.Builder
	b.WriteString(styles.Task.Label.Render("[Todo List]"))
	b.WriteByte('\n')
	show := sorted
	overflow := 0
	if len(sorted) > maxTaskBarLines {
		show = sorted[:maxTaskBarLines]
		overflow = len(sorted) - maxTaskBarLines
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

func taskStatusOrder(status string) int {
	switch status {
	case "in_progress":
		return 0
	case "pending":
		return 1
	default: // completed etc.
		return 2
	}
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
