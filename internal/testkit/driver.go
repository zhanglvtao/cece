package testkit

import (
	"image/color"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Driver runs a bubbletea Model in-process without a terminal. It owns
// the Init/Update/cmd loop and exposes ergonomic helpers for sending
// messages and reading the rendered View. It is intentionally minimal:
// no rendering, no input parsing, no signal handling.
type Driver struct {
	mu      sync.Mutex
	model   tea.Model
	msgs    chan tea.Msg
	done    chan struct{}
	quitOnce sync.Once
	closed  bool
}

// NewDriver wraps a Model and starts the main loop in a goroutine.
// The caller must invoke Close() to stop the driver and reclaim
// goroutines, typically with t.Cleanup.
func NewDriver(m tea.Model) *Driver {
	d := &Driver{
		model: m,
		msgs:  make(chan tea.Msg, 256),
		done:  make(chan struct{}),
	}
	go d.loop()
	return d
}

// Send pushes a single message into the driver loop. It is safe to call
// concurrently. Send is non-blocking when the driver is closed.
func (d *Driver) Send(msg tea.Msg) {
	if msg == nil {
		return
	}
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	// Hold the lock while sending so Close() cannot interleave.
	defer d.mu.Unlock()
	select {
	case d.msgs <- msg:
	case <-d.done:
	default:
		// Buffer full and not closed: spawn a goroutine to deliver
		// without blocking under the lock. This should be rare (256
		// slot buffer) and only happens when a cmd produces messages
		// faster than the loop can consume them.
		go func() {
			select {
			case d.msgs <- msg:
			case <-d.done:
			}
		}()
	}
}

// Press sends one or more key presses described in the short form
// understood by KeyMsg ("enter", "ctrl+c", "a", ...).
func (d *Driver) Press(keys ...string) {
	for _, k := range keys {
		d.Send(KeyMsg(k))
	}
}

// Type splits text into single-rune key presses.
func (d *Driver) Type(text string) {
	for _, r := range text {
		d.Send(KeyMsg(string(r)))
	}
}

// View returns the current rendered content (with ANSI sequences).
// Call ViewPlain for a stripped version.
func (d *Driver) View() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.model.View().Content
}

// ViewPlain returns the current view with ANSI escape codes removed.
func (d *Driver) ViewPlain() string {
	return stripANSI(d.View())
}

// Model returns the current model. Tests should treat it as read-only;
// the driver loop may replace it on the next Update.
func (d *Driver) Model() tea.Model {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.model
}

// WithModel calls fn with the current model under the driver's lock,
// preventing the loop from running an Update concurrently. Use this
// when reading bubble component state (textarea, viewport) that may
// be mutated mid-Update.
func (d *Driver) WithModel(fn func(tea.Model)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	fn(d.model)
}

// Close stops the driver loop. Safe to call multiple times.
func (d *Driver) Close() {
	d.quitOnce.Do(func() {
		d.mu.Lock()
		if d.closed {
			d.mu.Unlock()
			return
		}
		d.closed = true
		close(d.msgs)
		d.mu.Unlock()
	})
	// Wait for loop to exit, with a safety timeout so a stuck command
	// goroutine never deadlocks the test runner.
	select {
	case <-d.done:
	case <-time.After(2 * time.Second):
	}
}

// loop runs the Init → Update → Cmd cycle. Commands execute on
// goroutines and their resulting messages are fed back into msgs.
func (d *Driver) loop() {
	defer close(d.done)

	d.mu.Lock()
	initCmd := d.model.Init()
	d.mu.Unlock()
	d.runCmd(initCmd)

	for msg := range d.msgs {
		if _, ok := msg.(tea.QuitMsg); ok {
			return
		}
		d.mu.Lock()
		newModel, cmd := d.model.Update(msg)
		if newModel != nil {
			d.model = newModel
		}
		d.mu.Unlock()
		d.runCmd(cmd)
	}
}

// runCmd schedules a Cmd on a fresh goroutine and routes its produced
// Msg back into the loop. tea.Batch / tea.Sequence are flattened
// recursively so all child commands are executed.
func (d *Driver) runCmd(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	go func() {
		msg := cmd()
		d.handleProducedMsg(msg)
	}()
}

func (d *Driver) handleProducedMsg(msg tea.Msg) {
	if msg == nil {
		return
	}
	switch v := msg.(type) {
	case tea.BatchMsg:
		for _, c := range v {
			d.runCmd(c)
		}
	default:
		d.Send(msg)
	}
}

// Resize sends a tea.WindowSizeMsg.
func (d *Driver) Resize(w, h int) {
	d.Send(tea.WindowSizeMsg{Width: w, Height: h})
}

// Paste sends bracketed-paste content as a single tea.PasteMsg.
func (d *Driver) Paste(text string) {
	d.Send(tea.PasteMsg{Content: text})
}

// Wheel scrolls the chat viewport. dy < 0 → wheel up; dy > 0 → wheel down.
func (d *Driver) Wheel(dy int) {
	btn := tea.MouseWheelDown
	if dy < 0 {
		btn = tea.MouseWheelUp
	}
	d.Send(tea.MouseWheelMsg(tea.Mouse{Button: btn}))
}

// SetBackgroundColor injects a tea.BackgroundColorMsg with the given color.
func (d *Driver) SetBackgroundColor(c color.Color) {
	d.Send(tea.BackgroundColorMsg{Color: c})
}

// WaitForView blocks until the rendered view contains substr or
// timeout elapses. Returns true on match.
func (d *Driver) WaitForView(substr string, timeout time.Duration) bool {
	if timeout <= 0 {
		timeout = DefaultEventTimeout
	}
	deadline := time.Now().Add(timeout)
	for {
		if strings.Contains(d.ViewPlain(), substr) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// ModalKindFn is the predicate type returned by harness/UI getters that
// require knowing the modal kind. Avoids importing internal/ui here.
type ModalKindFn func() string

// WaitForModalKind blocks until kindFn returns the expected kind, or timeout.
func (d *Driver) WaitForModalKind(kindFn ModalKindFn, want string, timeout time.Duration) bool {
	if timeout <= 0 {
		timeout = DefaultEventTimeout
	}
	deadline := time.Now().Add(timeout)
	for {
		if kindFn() == want {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// WaitForBoolFn is a generic boolean wait helper. Returns true once
// fn() returns the desired value, or false on timeout.
func (d *Driver) WaitForBoolFn(fn func() bool, want bool, timeout time.Duration) bool {
	if timeout <= 0 {
		timeout = DefaultEventTimeout
	}
	deadline := time.Now().Add(timeout)
	for {
		if fn() == want {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// stripANSI removes ANSI escape sequences from s.
func stripANSI(s string) string {
	var out strings.Builder
	out.Grow(len(s))
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' {
			for i < len(s) && !((s[i] >= 'A' && s[i] <= 'Z') || (s[i] >= 'a' && s[i] <= 'z')) {
				i++
			}
			i++
			continue
		}
		out.WriteByte(s[i])
		i++
	}
	return out.String()
}
