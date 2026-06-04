package ui

import (
	"cece/internal/protocol"
)

// This file exposes read-only accessors that testkit and other tests
// use to assert on Model state without touching unexported fields.
// All names end in `ForTest` to discourage production callers.

// ModalKindForTest returns a stable string identifier for the active
// modal, or "" if none is active.
func (m *Model) ModalKindForTest() string {
	switch m.modal.kind {
	case modalNone:
		return ""
	case modalConfirmTools:
		return "confirm_tools"
	case modalApprovePlan:
		return "approve_plan"
	case modalQuestion:
		return "question"
	case modalModelPicker:
		return "model_picker"
	case modalSessionPicker:
		return "session_picker"
	case modalMCPPicker:
		return "mcp_picker"
	case modalRenameSession:
		return "rename_session"
	}
	return ""
}

// ModalActiveForTest reports whether any modal is open.
func (m *Model) ModalActiveForTest() bool { return m.modal.active() }

// BusyForTest reports whether the agent loop is currently active.
func (m *Model) BusyForTest() bool { return m.busy }

// StatusForTest returns the headline status text.
func (m *Model) StatusForTest() string { return m.status }

// ModeForTest returns the current permission mode.
func (m *Model) ModeForTest() protocol.PermissionMode { return m.mode }

// QueuedForTest returns a copy of the queued user inputs.
func (m *Model) QueuedForTest() []string {
	out := make([]string, len(m.queued))
	copy(out, m.queued)
	return out
}

// HistoryForTest returns a copy of the input history (most recent first).
func (m *Model) HistoryForTest() []string {
	out := make([]string, len(m.history))
	copy(out, m.history)
	return out
}

// TasksForTest returns a copy of the current todo list.
func (m *Model) TasksForTest() []protocol.TodoItem {
	out := make([]protocol.TodoItem, len(m.tasks))
	copy(out, m.tasks)
	return out
}

// RunningAgentsForTest returns a snapshot of active sub-agents
// (ID + Description + Activity history).
func (m *Model) RunningAgentsForTest() []RunningAgentSnapshot {
	out := make([]RunningAgentSnapshot, len(m.runningAgents))
	for i, a := range m.runningAgents {
		out[i] = RunningAgentSnapshot{
			ID:          a.ID,
			Description: a.Description,
			Model:       a.Model,
			ToolCall:    a.ToolCall,
			LastMsg:     a.LastMsg,
		}
	}
	return out
}

// RunningAgentSnapshot mirrors the unexported runningAgent struct.
type RunningAgentSnapshot struct {
	ID          string
	Description string
	Model       string
	ToolCall    string
	LastMsg     string
}

// SlashPopupActiveForTest reports whether the slash command popup is open.
func (m *Model) SlashPopupActiveForTest() bool { return m.slashPopup.Active() }

// FilePopupActiveForTest reports whether the @-file popup is open.
func (m *Model) FilePopupActiveForTest() bool { return m.filePopup.Active() }

// CurrentSessionIDForTest returns the active session ID, or "" if none.
func (m *Model) CurrentSessionIDForTest() string { return m.currentSessionID }

// InputValueForTest returns the current textarea value.
func (m *Model) InputValueForTest() string { return m.input.Value() }

// PendingQuitForTest reports whether a quit-after-title is pending.
func (m *Model) PendingQuitForTest() bool { return m.pendingQuit }

// OpenSessionsDialogForTest opens the session picker modal directly. For testing only.
func (m *Model) OpenSessionsDialogForTest() { m.openSessionsDialog() }
