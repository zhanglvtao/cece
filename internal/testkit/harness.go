package testkit

import (
	"context"
	"sync"
	"testing"
	"time"

	"cece/internal/agent"
	"cece/internal/engine"
	"cece/internal/mcp"
	"cece/internal/protocol"
	"cece/internal/runtime"
	"cece/internal/skill"
	"cece/internal/tool"
	"cece/internal/ui"

	tea "charm.land/bubbletea/v2"
)

// Harness wires a fake LLM, an Engine, an EngineMediator, an in-memory
// session store, the real UI Model and a Driver into a single
// end-to-end fixture suitable for `go test`. All public fields are
// safe to read in tests.
type Harness struct {
	t *testing.T

	LLM   *ScriptedClient
	Eng   *engine.Engine
	Med   *engine.EngineMediator
	Store *MemStore
	UI    *ui.Model
	Drv   *Driver

	// events records every protocol.Event emitted by the engine after
	// the harness started. Reads are guarded by mu.
	mu     sync.Mutex
	events []protocol.Event

	closeOnce sync.Once
	closeWG   sync.WaitGroup
}

// HarnessOption customises harness construction.
type HarnessOption func(*harnessConfig)

type harnessConfig struct {
	model         string
	projectDir    string
	contextWindow int
	yolo          bool
	maxTokens     int
	defaultMode   string
	width, height int

	store      *MemStore
	skills     *skill.Store
	mcpManager *mcp.Manager
	extraTools []tool.Tool

	lightClient agent.ModelClient

	providerResolver runtime.ProviderResolverFn
	createClientFn   runtime.CreateClientFn
	listAllModelsFn  runtime.ListAllModelsFn
}

// WithModel overrides the default model name.
func WithModel(name string) HarnessOption {
	return func(c *harnessConfig) { c.model = name }
}

// WithProjectDir overrides the default project directory.
func WithProjectDir(dir string) HarnessOption {
	return func(c *harnessConfig) { c.projectDir = dir }
}

// WithContextWindow overrides the default context window size.
func WithContextWindow(n int) HarnessOption {
	return func(c *harnessConfig) { c.contextWindow = n }
}

// WithYolo enables auto-approval of tool execution.
func WithYolo(yolo bool) HarnessOption {
	return func(c *harnessConfig) { c.yolo = yolo }
}

// WithMaxTokens overrides the default max output tokens.
func WithMaxTokens(n int) HarnessOption {
	return func(c *harnessConfig) { c.maxTokens = n }
}

// WithWindowSize overrides the initial terminal size sent to the UI.
func WithWindowSize(w, h int) HarnessOption {
	return func(c *harnessConfig) { c.width = w; c.height = h }
}

// WithDefaultMode sets the initial permission mode.
func WithDefaultMode(mode string) HarnessOption {
	return func(c *harnessConfig) { c.defaultMode = mode }
}

// WithStore replaces the default in-memory store. Useful for tests
// that pre-seed conversations.
func WithStore(s *MemStore) HarnessOption {
	return func(c *harnessConfig) { c.store = s }
}

// WithSkillStore injects a skill store into the harness.
func WithSkillStore(s *skill.Store) HarnessOption {
	return func(c *harnessConfig) { c.skills = s }
}

// WithMCPManager injects an MCP manager.
func WithMCPManager(m *mcp.Manager) HarnessOption {
	return func(c *harnessConfig) { c.mcpManager = m }
}

// WithExtraTools registers additional / replacement tools.
func WithExtraTools(tools ...tool.Tool) HarnessOption {
	return func(c *harnessConfig) { c.extraTools = append(c.extraTools, tools...) }
}

// WithLightClient injects an alternate model client used by AutoTitle.
func WithLightClient(client agent.ModelClient) HarnessOption {
	return func(c *harnessConfig) { c.lightClient = client }
}

// WithProviderResolver overrides the default empty provider resolver.
func WithProviderResolver(fn runtime.ProviderResolverFn) HarnessOption {
	return func(c *harnessConfig) { c.providerResolver = fn }
}

// WithCreateClientFn overrides the default factory that always returns
// the harness's primary LLM.
func WithCreateClientFn(fn runtime.CreateClientFn) HarnessOption {
	return func(c *harnessConfig) { c.createClientFn = fn }
}

// WithListAllModelsFn overrides the default single-model list.
func WithListAllModelsFn(fn runtime.ListAllModelsFn) HarnessOption {
	return func(c *harnessConfig) { c.listAllModelsFn = fn }
}

// NewHarness assembles the full pipeline and starts the driver.
// Cleanup is registered with t.Cleanup so callers don't have to.
func NewHarness(t *testing.T, llm *ScriptedClient, opts ...HarnessOption) *Harness {
	t.Helper()

	cfg := harnessConfig{
		model:         "test-model",
		projectDir:    t.TempDir(),
		contextWindow: 200000,
		yolo:          true,
		maxTokens:     4096,
		width:         120,
		height:        40,
	}
	for _, o := range opts {
		o(&cfg)
	}
	if llm == nil {
		llm = NewScriptedClient()
	}
	if cfg.store == nil {
		cfg.store = NewMemStore()
	}

	var lightFn runtime.LightModelClientFn
	if cfg.lightClient != nil {
		client := cfg.lightClient
		lightFn = func() agent.ModelClient { return client }
	}

	bundle, err := runtime.Build(runtime.Options{
		ProjectDir:       cfg.projectDir,
		Model:            cfg.model,
		ContextWindow:    cfg.contextWindow,
		MaxTokens:        cfg.maxTokens,
		Yolo:             cfg.yolo,
		DefaultMode:      cfg.defaultMode,
		StablePrompt:     "test stable prompt",
		ModelClient:      llm,
		Store:            cfg.store,
		Skills:           cfg.skills,
		ExtraTools:       cfg.extraTools,
		MCPManager:       cfg.mcpManager,
		ProviderResolver: cfg.providerResolver,
		CreateClientFn:   cfg.createClientFn,
		ListAllModelsFn:  cfg.listAllModelsFn,
		LightClientFn:    lightFn,
	})
	if err != nil {
		t.Fatalf("testkit harness build failed: %v", err)
	}

	h := &Harness{
		t:     t,
		LLM:   llm,
		Eng:   bundle.Engine,
		Med:   bundle.Mediator,
		Store: cfg.store,
	}

	// Tap the engine's event stream so the harness can observe every
	// event while the UI also receives a copy via Eventer.Events().
	tap := newEventTap(bundle.Engine.Events(), &h.events, &h.mu)
	h.closeWG.Add(1)
	go func() { defer h.closeWG.Done(); tap.run() }()

	wrapped := &tappedSender{EngineMediator: bundle.Mediator, events: tap.out}
	uiModel := ui.NewModel(wrapped, cfg.model, cfg.projectDir, cfg.contextWindow)
	if cfg.skills != nil {
		uiModel.SetSkillStore(cfg.skills)
	}
	uiModel.SetSessions(cfg.store)
	if cfg.defaultMode != "" {
		uiModel.SetDefaultMode(cfg.defaultMode)
	}
	h.UI = &uiModel
	h.Drv = NewDriver(&uiModel)

	// Initial WindowSizeMsg so layout is finite.
	h.Drv.Send(tea.WindowSizeMsg{Width: cfg.width, Height: cfg.height})

	t.Cleanup(h.Close)
	return h
}

// ── ergonomic action helpers ───────────────────────────────────────────────

// Send types text and presses enter (submits user input).
func (h *Harness) Send(text string) {
	h.Drv.Type(text)
	h.Drv.Press("enter")
}

// Do dispatches an action directly to the mediator, bypassing the UI.
// Useful for tests that exercise mediator-side flows without simulating
// a full keyboard sequence (e.g. /resume picker).
func (h *Harness) Do(action protocol.Action) {
	h.Med.Do(action)
}

// SendSlash types "/<cmd> <args>" and submits.
func (h *Harness) SendSlash(cmd, args string) {
	if args == "" {
		h.Send("/" + cmd)
		return
	}
	h.Send("/" + cmd + " " + args)
}

// Press forwards to the driver.
func (h *Harness) Press(keys ...string) { h.Drv.Press(keys...) }

// Type forwards to the driver.
func (h *Harness) Type(text string) { h.Drv.Type(text) }

// Confirm answers "y" to a tool-confirmation modal.
func (h *Harness) Confirm() { h.Drv.Press("y") }

// Reject answers "n" to a tool-confirmation modal.
func (h *Harness) Reject() { h.Drv.Press("n") }

// AcceptAuto switches to auto-accept and confirms the current modal.
func (h *Harness) AcceptAuto() { h.Drv.Press("shift+tab") }

// ApprovePlan answers "y" to a plan-approval modal.
func (h *Harness) ApprovePlan() { h.Drv.Press("y") }

// RejectPlan answers "n" to a plan-approval modal.
func (h *Harness) RejectPlan() { h.Drv.Press("n") }

// ApprovePlanAuto switches exit-target to auto-accept and approves plan.
func (h *Harness) ApprovePlanAuto() { h.Drv.Press("shift+tab") }

// Cancel sends esc.
func (h *Harness) Cancel() { h.Drv.Press("esc") }

// QuitOnce sends a single ctrl+c.
func (h *Harness) QuitOnce() { h.Drv.Press("ctrl+c") }

// QuitTwice sends two ctrl+c presses (used for force-delete-and-quit).
func (h *Harness) QuitTwice() {
	h.Drv.Press("ctrl+c")
	h.Drv.Press("ctrl+c")
}

// CycleMode cycles permission mode via shift+tab.
func (h *Harness) CycleMode() { h.Drv.Press("shift+tab") }

// SetMode dispatches a SetPermissionModeAction directly.
func (h *Harness) SetMode(mode protocol.PermissionMode) {
	h.Med.Do(protocol.SetPermissionModeAction{Mode: mode})
}

// LoadSession dispatches a LoadSessionAction.
func (h *Harness) LoadSession(id string) {
	h.Med.Do(protocol.LoadSessionAction{SessionID: id})
}

// CurrentUI returns the most recent *ui.Model held by the driver.
// Bubble Tea's value-receiver Update creates a fresh copy each call,
// so reading state directly from the field initialised at construction
// time would be stale. Always go through CurrentUI() in tests that
// inspect post-Update state.
//
// IMPORTANT: methods on *ui.Model that read mutable component state
// (textarea, viewport, etc.) race with the driver's Update goroutine.
// Use Read() for those reads instead.
func (h *Harness) CurrentUI() *ui.Model {
	if m, ok := h.Drv.Model().(*ui.Model); ok {
		return m
	}
	return h.UI
}

// Read invokes fn with the current *ui.Model while holding the driver's
// lock, ensuring no concurrent Update mutates component state during
// the read. Returns whatever fn returned.
func (h *Harness) Read(fn func(*ui.Model)) {
	h.Drv.WithModel(func(m tea.Model) {
		if um, ok := m.(*ui.Model); ok {
			fn(um)
		}
	})
}

// ReadString is a typed convenience for fn that returns a string.
func (h *Harness) ReadString(fn func(*ui.Model) string) string {
	var out string
	h.Read(func(m *ui.Model) { out = fn(m) })
	return out
}

// ReadBool is a typed convenience for fn that returns a bool.
func (h *Harness) ReadBool(fn func(*ui.Model) bool) bool {
	var out bool
	h.Read(func(m *ui.Model) { out = fn(m) })
	return out
}

// ReadStrings is a typed convenience for fn that returns a []string.
func (h *Harness) ReadStrings(fn func(*ui.Model) []string) []string {
	var out []string
	h.Read(func(m *ui.Model) { out = fn(m) })
	return out
}

// EventsSnapshot returns a copy of every event seen so far.
func (h *Harness) EventsSnapshot() []protocol.Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]protocol.Event, len(h.events))
	copy(out, h.events)
	return out
}

// ── ergonomic wait helpers ─────────────────────────────────────────────────

// WaitForModal blocks until a modal of the given kind opens, or timeout.
// Use kind values as documented on Model.ModalKindForTest.
func (h *Harness) WaitForModal(kind string, timeout time.Duration) bool {
	return h.Drv.WaitForModalKind(func() string {
		return h.ReadString(func(m *ui.Model) string { return m.ModalKindForTest() })
	}, kind, timeout)
}

// WaitForBusy blocks until BusyForTest matches want, or timeout.
func (h *Harness) WaitForBusy(want bool, timeout time.Duration) bool {
	return h.Drv.WaitForBoolFn(func() bool {
		return h.ReadBool(func(m *ui.Model) bool { return m.BusyForTest() })
	}, want, timeout)
}

// WaitForReady blocks until status text becomes "Ready" (i.e. idle).
func (h *Harness) WaitForReady(timeout time.Duration) bool {
	return h.Drv.WaitForBoolFn(func() bool {
		return h.ReadString(func(m *ui.Model) string { return m.StatusForTest() }) == "Ready"
	}, true, timeout)
}

// WaitForView is a convenience wrapper around Driver.WaitForView.
func (h *Harness) WaitForView(substr string, timeout time.Duration) bool {
	return h.Drv.WaitForView(substr, timeout)
}

// Close shuts down the driver and waits for the mediator's background
// goroutines. Idempotent.
func (h *Harness) Close() {
	h.closeOnce.Do(func() {
		if h.Drv != nil {
			h.Drv.Close()
		}
		h.Med.Wait()
	})
}

// ── internal plumbing ───────────────────────────────────────────────────────

// eventTap forwards events from the engine to (a) the harness's
// recorded list and (b) a fresh channel consumed by the UI.
type eventTap struct {
	in  <-chan protocol.Event
	out chan protocol.Event
	rec *[]protocol.Event
	mu  *sync.Mutex
}

func newEventTap(in <-chan protocol.Event, rec *[]protocol.Event, mu *sync.Mutex) *eventTap {
	return &eventTap{
		in:  in,
		out: make(chan protocol.Event, 256),
		rec: rec,
		mu:  mu,
	}
}

func (t *eventTap) run() {
	defer close(t.out)
	for ev := range t.in {
		t.mu.Lock()
		*t.rec = append(*t.rec, ev)
		t.mu.Unlock()
		t.out <- ev
	}
}

// tappedSender adapts an EngineMediator so its Events() returns the
// tap's downstream channel rather than the raw engine channel.
type tappedSender struct {
	*engine.EngineMediator
	events <-chan protocol.Event
}

func (t *tappedSender) Events() <-chan protocol.Event { return t.events }

// Silence "imported and not used" if context becomes unused after refactor.
var _ = context.Background
