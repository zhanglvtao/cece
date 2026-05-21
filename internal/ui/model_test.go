package ui

import (
	"errors"
	"testing"
	"time"

	"cece/internal/chat"
	"cece/internal/ui/list"
	tea "charm.land/bubbletea/v2"
)

func TestApplyEventBuildsAssistantMessage(t *testing.T) {
	model := NewModel(nil, "claude-sonnet-4-6", "/tmp")

	model.applyEvent(chat.UIUserMessageAdded{
		Message: chat.Message{Role: chat.UserRole, Content: "Hi"},
	})
	model.applyEvent(chat.UIAssistantStarted{})
	model.applyEvent(chat.UIAssistantDelta{Text: "Hello"})
	model.applyEvent(chat.UIAssistantDelta{Text: " there"})
	model.applyEvent(chat.UIAssistantCompleted{})

	if model.chat.list.Len() != 2 {
		t.Fatalf("list len = %d, want 2 (user + assistant)", model.chat.list.Len())
	}
	if !model.chat.currentAssistant.Finished() {
		t.Fatal("assistant should be finished")
	}
	if model.busy {
		t.Fatal("busy = true, want false")
	}
}

func TestApplyEventShowsFailureWithoutDroppingPartialReply(t *testing.T) {
	model := NewModel(nil, "claude-sonnet-4-6", "/tmp")

	model.applyEvent(chat.UIAssistantStarted{})
	model.applyEvent(chat.UIAssistantDelta{Text: "partial"})
	model.applyEvent(chat.UIRunFailed{Err: errors.New("boom")})

	if model.chat.list.Len() != 2 {
		t.Fatalf("list len = %d, want 2 (partial assistant + interrupted)", model.chat.list.Len())
	}
	if model.status == "" {
		t.Fatal("expected status message")
	}
}

func TestCtrlCWhileBusyStopsStreamingAndKeepsPartialText(t *testing.T) {
	model := NewModel(nil, "claude-sonnet-4-6", "/tmp")

	model.applyEvent(chat.UIUserMessageAdded{
		Message: chat.Message{Role: chat.UserRole, Content: "Hi"},
	})
	model.applyEvent(chat.UIModelRequestStarted{Reason: "user"})
	model.applyEvent(chat.UIAssistantStarted{})
	model.applyEvent(chat.UIAssistantDelta{Text: "partial"})
	model.applyEvent(chat.UIRunFailed{Err: errors.New("cancelled")})

	if model.busy {
		t.Fatal("busy = true after cancel, want false")
	}
	if model.chat.list.Len() != 4 {
		t.Fatalf("list len = %d, want 4 (user + reqDetail + partial assistant + interrupted)", model.chat.list.Len())
	}
	if model.status != "Cancelled" {
		t.Fatalf("status = %q, want %q", model.status, "Cancelled")
	}
}

func TestApplyEventCollectsStreamDetails(t *testing.T) {
	model := NewModel(nil, "claude-sonnet-4-6", "/tmp")

	model.applyEvent(chat.UIUserMessageAdded{
		Message: chat.Message{Role: chat.UserRole, Content: "Hi"},
	})
	model.applyEvent(chat.UIAssistantStarted{})
	model.applyEvent(chat.UIStreamStarted{InputTokens: 42, CacheCreationTokens: 500, CacheReadTokens: 200})
	model.applyEvent(chat.UIAssistantDelta{Text: "Hel"})
	model.applyEvent(chat.UIStreamCompleted{OutputTokens: 7, StopReason: "end_turn", Duration: 500 * time.Millisecond, ToolCalls: []string{"Bash"}})
	model.applyEvent(chat.UIAssistantCompleted{})

	if model.chat.currentDetail == nil {
		t.Fatal("expected detail item to be created")
	}
	d := model.chat.currentDetail.detail
	if d.InputTokens != 42 {
		t.Fatalf("detail.InputTokens = %d, want 42", d.InputTokens)
	}
	if d.OutputTokens != 7 {
		t.Fatalf("detail.OutputTokens = %d, want 7", d.OutputTokens)
	}
	if d.StopReason != "end_turn" {
		t.Fatalf("detail.StopReason = %q, want %q", d.StopReason, "end_turn")
	}
	if d.Duration != 500*time.Millisecond {
		t.Fatalf("detail.Duration = %v, want %v", d.Duration, 500*time.Millisecond)
	}
	if d.CacheCreationTokens != 500 {
		t.Fatalf("detail.CacheCreationTokens = %d, want 500", d.CacheCreationTokens)
	}
	if d.CacheReadTokens != 200 {
		t.Fatalf("detail.CacheReadTokens = %d, want 200", d.CacheReadTokens)
	}
	if len(d.ToolCalls) != 1 || d.ToolCalls[0] != "Bash" {
		t.Fatalf("detail.ToolCalls = %v, want [Bash]", d.ToolCalls)
	}
}

func TestTokenInfoTracksCumulativeTokens(t *testing.T) {
	model := NewModel(nil, "claude-sonnet-4-6", "/tmp")

	model.applyEvent(chat.UIStreamStarted{InputTokens: 10})
	model.applyEvent(chat.UIStreamCompleted{OutputTokens: 5})

	in, out := model.chat.TokenInfo()
	if in != 10 {
		t.Fatalf("input tokens = %d, want 10", in)
	}
	if out != 5 {
		t.Fatalf("output tokens = %d, want 5", out)
	}
}

func TestRequestDetailBackfilledOnStreamStarted(t *testing.T) {
	model := NewModel(nil, "claude-sonnet-4-6", "/tmp")

	model.applyEvent(chat.UIUserMessageAdded{
		Message: chat.Message{Role: chat.UserRole, Content: "Hi"},
	})
	if model.chat.currentRequest != nil {
		t.Fatal("currentRequest should be nil before UIModelRequestStarted")
	}

	model.applyEvent(chat.UIModelRequestStarted{Reason: "user"})
	if model.chat.currentRequest == nil {
		t.Fatal("currentRequest should be set after UIModelRequestStarted")
	}
	if model.chat.currentRequest.inputTokens != 0 {
		t.Fatalf("requestDetail.inputTokens = %d before UIStreamStarted, want 0", model.chat.currentRequest.inputTokens)
	}

	model.applyEvent(chat.UIStreamStarted{InputTokens: 100})
	if model.chat.currentRequest.inputTokens != 100 {
		t.Fatalf("requestDetail.inputTokens = %d after UIStreamStarted, want 100", model.chat.currentRequest.inputTokens)
	}
}

func TestLoadingItemAddedAndRemoved(t *testing.T) {
	model := NewModel(nil, "claude-sonnet-4-6", "/tmp")

	model.chat.ApplyEvent(chat.UIUserMessageAdded{
		Message: chat.Message{Role: chat.UserRole, Content: "Hi"},
	})
	loading := &loadingItem{
		Versioned: list.NewVersioned(),
		styles:    model.styles,
	}
	model.chat.SetLoading(loading)
	model.busy = true

	if model.chat.list.Len() != 2 {
		t.Fatalf("list len = %d after adding loading, want 2", model.chat.list.Len())
	}

	model.applyEvent(chat.UIAssistantStarted{})
	if model.chat.loading == nil {
		t.Fatal("loading should still be present after UIAssistantStarted")
	}
	if model.chat.loading.label != "Generating" {
		t.Fatalf("loading label = %q, want 'Generating'", model.chat.loading.label)
	}
	if model.chat.list.Len() != 3 {
		t.Fatalf("list len = %d after UIAssistantStarted, want 3", model.chat.list.Len())
	}
}

func TestLoadingItemRemovedOnRunFailed(t *testing.T) {
	model := NewModel(nil, "claude-sonnet-4-6", "/tmp")

	model.chat.ApplyEvent(chat.UIUserMessageAdded{
		Message: chat.Message{Role: chat.UserRole, Content: "Hi"},
	})
	loading := &loadingItem{
		Versioned: list.NewVersioned(),
		styles:    model.styles,
	}
	model.chat.SetLoading(loading)
	model.busy = true

	model.applyEvent(chat.UIRunFailed{Err: errors.New("boom")})
	if model.chat.loading != nil {
		t.Fatal("loading should be nil after UIRunFailed")
	}
}

func TestTruncationRetryUpdatesStatusAndChat(t *testing.T) {
	model := NewModel(nil, "claude-sonnet-4-6", "/tmp")

	model.applyEvent(chat.UITruncationRetry{
		Attempt:       1,
		PrevMaxTokens: 16384,
		NewMaxTokens:  64000,
	})

	if model.status != "Retrying (64K)..." {
		t.Fatalf("status = %q, want %q", model.status, "Retrying (64K)...")
	}

	// Chat should have a truncationRetryItem
	found := false
	for i := 0; i < model.chat.list.Len(); i++ {
		if _, ok := model.chat.list.ItemAt(i).(*truncationRetryItem); ok {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected truncationRetryItem in chat list")
	}
}

func TestUserMessageDeduplicatedWhenBusy(t *testing.T) {
	model := NewModel(nil, "claude-sonnet-4-6", "/tmp")

	model.chat.ApplyEvent(chat.UIUserMessageAdded{
		Message: chat.Message{Role: chat.UserRole, Content: "Hi"},
	})
	loading := &loadingItem{
		Versioned: list.NewVersioned(),
		styles:    model.styles,
	}
	model.chat.SetLoading(loading)
	model.busy = true

	model.applyEvent(chat.UIUserMessageAdded{
		Message: chat.Message{Role: chat.UserRole, Content: "Hi"},
	})

	if model.chat.list.Len() != 2 {
		t.Fatalf("list len = %d after duplicate UIUserMessageAdded, want 2", model.chat.list.Len())
	}
}

// scrollState captures the chat list scroll position for equality assertions.
// Uses public APIs only.
func scrollState(m *Model) (firstVisibleIdx int, atBottom bool) {
	start, _ := m.chat.list.VisibleItemIndices()
	return start, m.chat.list.AtBottom()
}

// seedScrollableChat populates the chat with enough content to require
// scrolling and resizes the model to a known viewport.
func seedScrollableChat(t *testing.T, m *Model) {
	t.Helper()
	for i := 0; i < 20; i++ {
		m.applyEvent(chat.UIUserMessageAdded{
			Message: chat.Message{Role: chat.UserRole, Content: "msg"},
		})
	}
	m.width, m.height = 80, 24
	m.handleResize(m.width, m.height)
	chatRect, _, _ := m.generateLayout(m.width, m.height)
	if chatRect.Dy() <= 0 {
		t.Fatalf("chatRect height = %d, want > 0", chatRect.Dy())
	}
	m.chat.list.ScrollToTop()
}

// TestMouseWheelScrollsChat verifies that wheel events scroll the chat
// history, regardless of cursor position. The terminal protocol does not
// support wheel-only capture, so we run in cell-motion mode and selectively
// honour wheel events at the application layer.
func TestMouseWheelScrollsChat(t *testing.T) {
	m := NewModel(nil, "claude-sonnet-4-6", "/tmp")
	model := &m
	seedScrollableChat(t, model)

	beforeIdx, beforeBottom := scrollState(model)
	updated, _ := model.update(tea.MouseWheelMsg{X: 5, Y: 2, Button: tea.MouseWheelDown})
	model = updated.(*Model)
	afterIdx, afterBottom := scrollState(model)
	if beforeIdx == afterIdx && beforeBottom == afterBottom {
		t.Fatalf("wheel down did not scroll chat: idx=%d bottom=%v unchanged",
			beforeIdx, beforeBottom)
	}
}

// TestMouseWheelInInputAreaDoesNotScrollChat verifies that wheel events
// landing in the input box are routed to the textarea, not the chat list.
func TestMouseWheelInInputAreaDoesNotScrollChat(t *testing.T) {
	m := NewModel(nil, "claude-sonnet-4-6", "/tmp")
	model := &m
	seedScrollableChat(t, model)

	_, inputRect, _ := model.generateLayout(model.width, model.height)
	beforeIdx, beforeBottom := scrollState(model)

	updated, _ := model.update(tea.MouseWheelMsg{
		X:      5,
		Y:      inputRect.Min.Y,
		Button: tea.MouseWheelDown,
	})
	model = updated.(*Model)

	afterIdx, afterBottom := scrollState(model)
	if beforeIdx != afterIdx || beforeBottom != afterBottom {
		t.Fatalf("wheel in input area scrolled chat: idx %d->%d bottom %v->%v",
			beforeIdx, afterIdx, beforeBottom, afterBottom)
	}
}

// TestClickInChatSuppressesWheel verifies the click-priority rule: a click
// landing in the chat area starts a selection gesture that suppresses
// subsequent wheel scrolling, until the corresponding release.
func TestClickInChatSuppressesWheel(t *testing.T) {
	m := NewModel(nil, "claude-sonnet-4-6", "/tmp")
	model := &m
	seedScrollableChat(t, model)

	// Start selection.
	updated, _ := model.update(tea.MouseClickMsg{X: 5, Y: 2, Button: tea.MouseLeft})
	model = updated.(*Model)
	if !model.chatSelecting {
		t.Fatal("click in chat area should arm chatSelecting")
	}

	// Wheel during selection: should be suppressed.
	beforeIdx, beforeBottom := scrollState(model)
	updated, _ = model.update(tea.MouseWheelMsg{X: 5, Y: 2, Button: tea.MouseWheelDown})
	model = updated.(*Model)
	afterIdx, afterBottom := scrollState(model)
	if beforeIdx != afterIdx || beforeBottom != afterBottom {
		t.Fatalf("wheel during selection should not scroll chat: idx %d->%d bottom %v->%v",
			beforeIdx, afterIdx, beforeBottom, afterBottom)
	}

	// Release ends selection.
	updated, _ = model.update(tea.MouseReleaseMsg{X: 5, Y: 2, Button: tea.MouseLeft})
	model = updated.(*Model)
	if model.chatSelecting {
		t.Fatal("release should clear chatSelecting")
	}

	// Wheel after release scrolls again.
	beforeIdx, beforeBottom = scrollState(model)
	updated, _ = model.update(tea.MouseWheelMsg{X: 5, Y: 2, Button: tea.MouseWheelDown})
	model = updated.(*Model)
	afterIdx, afterBottom = scrollState(model)
	if beforeIdx == afterIdx && beforeBottom == afterBottom {
		t.Fatalf("wheel after release should scroll chat again: idx=%d bottom=%v unchanged",
			beforeIdx, beforeBottom)
	}
}
