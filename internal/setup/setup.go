package setup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/zhanglvtao/cece/internal/ui/picker"
)

var csiResidueRe = regexp.MustCompile(`^\[\d+(;\d+)*[~A-Za-z]$`)

const settingsRelPath = ".cece/settings.json"

// step identifies the current wizard step.
type step int

const (
	stepWelcome step = iota
	stepProtocol
	stepAPIKey
	stepBaseURL
	stepModel
	stepMode
	stepDone
)

var stepOrder = []step{stepProtocol, stepAPIKey, stepBaseURL, stepModel, stepMode}

// selectMsg carries a picker selection back to Update.
type selectMsg struct{ value string }

// backMsg signals the user wants to go to the previous step.
type backMsg struct{}

// protocolOption is a picker item for protocol selection.
type protocolOption struct{ id string }

// modelOption is a picker item for model selection.
type modelOption struct {
	id   string
	name string
}

// modeOption is a picker item for mode selection.
type modeOption struct {
	id   string
	desc string
}

var protocols = []protocolOption{
	{id: "anthropic"},
	{id: "codebase"},
	{id: "aiden"},
}

var anthropicModels = []modelOption{
	{id: "claude-sonnet-4-6", name: "claude-sonnet-4-6"},
	{id: "claude-opus-4", name: "claude-opus-4"},
	{id: "claude-haiku-3-5", name: "claude-haiku-3-5"},
}

var modes = []modeOption{
	{id: "default", desc: "Confirm before writes and shell commands"},
	{id: "auto-accept", desc: "Auto-approve all tool calls"},
	{id: "plan", desc: "LLM writes plan first, you review before execution"},
}

// collected stores the user's choices.
type collected struct {
	protocol string
	apiKey   string
	baseURL  string
	model    string
	mode     string
}

// SetupModel is a standalone bubbletea model for the setup wizard.
type SetupModel struct {
	step      step
	col       collected
	picker    *picker.Picker
	textInput string
	customInput bool // true when user chose "Custom input..." for model
	width     int
	height    int
	err       string
	existing  bool
	projectDir string
	configPath string
}

// NewSetupModel creates the setup wizard model.
func NewSetupModel(projectDir string) SetupModel {
	configPath := filepath.Join(projectDir, settingsRelPath)
	exists := false
	if _, err := os.Stat(configPath); err == nil {
		exists = true
	}
	return SetupModel{
		step:       stepWelcome,
		existing:   exists,
		projectDir: projectDir,
		configPath: configPath,
	}
}

func (m SetupModel) Init() tea.Cmd { return nil }

func (m SetupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	case selectMsg:
		return m.handleSelect(msg.value)
	case backMsg:
		return m.goBack()
	}

	switch m.step {
	case stepWelcome:
		return m.updateWelcome(msg)
	case stepProtocol, stepModel, stepMode:
		return m.updatePicker(msg)
	case stepAPIKey, stepBaseURL:
		return m.updateTextInput(msg)
	case stepDone:
		return m.updateDone(msg)
	}
	return m, nil
}

func (m SetupModel) updateWelcome(msg tea.Msg) (tea.Model, tea.Cmd) {
	kp, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch kp.String() {
	case "enter":
		m.step = stepProtocol
		m.openPicker()
		return m, nil
	case "esc":
		return m, tea.Quit
	}
	return m, nil
}

func (m SetupModel) updatePicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	kp, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}

	// Custom model input mode
	if m.step == stepModel && m.customInput {
		return m.updateCustomModelInput(kp)
	}

	if m.picker == nil {
		return m, nil
	}
	result, cmd := m.picker.HandleKey(kp)
	if result == picker.ResultClose {
		m.picker = nil
		if m.step == stepProtocol {
			return m, tea.Quit
		}
		return m, func() tea.Msg { return backMsg{} }
	}
	return m, cmd
}

func (m SetupModel) updateCustomModelInput(kp tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch kp.String() {
	case "enter":
		input := strings.TrimSpace(m.textInput)
		if input != "" {
			m.col.model = input
			m.customInput = false
			m.textInput = ""
			m.step = stepMode
			m.openPicker()
			return m, nil
		}
	case "esc":
		m.customInput = false
		m.textInput = ""
		m.openPicker() // reopen model picker
		return m, nil
	case "backspace":
		if m.textInput != "" {
			_, size := utf8.DecodeLastRuneInString(m.textInput)
			m.textInput = m.textInput[:len(m.textInput)-size]
		}
	default:
		if text := kp.Key().Text; text != "" && !csiResidueRe.MatchString(text) {
			m.textInput += text
		}
	}
	return m, nil
}

func (m SetupModel) updateTextInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	kp, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	m.err = ""
	switch kp.String() {
	case "enter":
		input := strings.TrimSpace(m.textInput)
		if input == "" {
			field := "API key"
			if m.step == stepBaseURL {
				field = "Base URL"
			}
			m.err = field + " is required"
			return m, nil
		}
		switch m.step {
		case stepAPIKey:
			m.col.apiKey = input
			m.step = stepBaseURL
			m.textInput = ""
			return m, nil
		case stepBaseURL:
			m.col.baseURL = input
			m.step = stepModel
			m.textInput = ""
			m.openPicker()
			return m, nil
		}
	case "esc":
		return m, func() tea.Msg { return backMsg{} }
	case "backspace":
		if m.textInput != "" {
			_, size := utf8.DecodeLastRuneInString(m.textInput)
			m.textInput = m.textInput[:len(m.textInput)-size]
		}
	default:
		if text := kp.Key().Text; text != "" && !csiResidueRe.MatchString(text) {
			m.textInput += text
		}
	}
	return m, nil
}

func (m SetupModel) updateDone(msg tea.Msg) (tea.Model, tea.Cmd) {
	kp, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch kp.String() {
	case "enter", "esc":
		return m, tea.Quit
	}
	return m, nil
}

// handleSelect processes a picker selection message.
func (m SetupModel) handleSelect(value string) (tea.Model, tea.Cmd) {
	switch m.step {
	case stepProtocol:
		m.col.protocol = value
		m.step = stepAPIKey
		m.textInput = ""
	case stepModel:
		if value == "__custom__" {
			m.customInput = true
			m.textInput = ""
			return m, nil
		}
		m.col.model = value
		m.step = stepMode
		m.openPicker()
		return m, nil
	case stepMode:
		m.col.mode = value
		if err := m.save(); err != nil {
			m.err = err.Error()
			return m, nil
		}
		m.step = stepDone
	}
	return m, nil
}

// goBack navigates to the previous step.
func (m SetupModel) goBack() (tea.Model, tea.Cmd) {
	idx := -1
	for i, s := range stepOrder {
		if s == m.step {
			idx = i
			break
		}
	}
	if idx > 0 {
		m.step = stepOrder[idx-1]
	} else {
		m.step = stepWelcome
	}
	// Restore text for text input steps
	switch m.step {
	case stepAPIKey:
		m.textInput = m.col.apiKey
	case stepBaseURL:
		m.textInput = m.col.baseURL
	default:
		m.textInput = ""
		m.openPicker()
	}
	return m, nil
}

// openPicker initializes the picker for the current step.
func (m *SetupModel) openPicker() {
	m.picker = nil
	switch m.step {
	case stepProtocol:
		items := make([]any, len(protocols))
		for i, p := range protocols {
			items[i] = p
		}
		p := picker.New("[1/5] Provider protocol", items, 8, func(item any, selected bool) string {
			return picker.FormatItem(item.(protocolOption).id, selected)
		})
		p.SetOnSelect(func(item any) tea.Cmd {
			return func() tea.Msg { return selectMsg{value: item.(protocolOption).id} }
		})
		p.SetHelpText("[up/down] move  [enter] select  [esc] quit")
		m.picker = p

	case stepModel:
		var models []modelOption
		if m.col.protocol == "anthropic" {
			models = anthropicModels
		}
		models = append(models, modelOption{id: "__custom__", name: "Custom input..."})
		items := make([]any, len(models))
		for i, mo := range models {
			items[i] = mo
		}
		p := picker.New("[4/5] Default model", items, 8, func(item any, selected bool) string {
			opt := item.(modelOption)
			name := opt.name
			if name == "" {
				name = opt.id
			}
			return picker.FormatItem(name, selected)
		})
		p.SetFilterFn(func(item any, q string) bool {
			opt := item.(modelOption)
			return strings.Contains(strings.ToLower(opt.name+" "+opt.id), strings.ToLower(q))
		})
		p.SetOnSelect(func(item any) tea.Cmd {
			return func() tea.Msg { return selectMsg{value: item.(modelOption).id} }
		})
		p.SetHelpText("[up/down] move  [enter] select  [type] filter  [esc] back")
		m.picker = p

	case stepMode:
		items := make([]any, len(modes))
		for i, md := range modes {
			items[i] = md
		}
		p := picker.New("[5/5] Default mode", items, 8, func(item any, selected bool) string {
			opt := item.(modeOption)
			line := opt.id
			if opt.desc != "" {
				line += "  " + opt.desc
			}
			return picker.FormatItem(line, selected)
		})
		p.SetOnSelect(func(item any) tea.Cmd {
			return func() tea.Msg { return selectMsg{value: item.(modeOption).id} }
		})
		p.SetHelpText("[up/down] move  [enter] select  [esc] back")
		m.picker = p
	}
}

// save writes the collected config to .cece/settings.json in the project directory.
func (m *SetupModel) save() error {
	sf := settingsFile{
		Provider: providerSection{
			Model: []string{m.col.model},
			Providers: []providerEntry{
				{
					Name:     m.col.protocol,
					Protocol: m.col.protocol,
					APIKey:   m.col.apiKey,
					BaseURL:  m.col.baseURL,
				},
			},
		},
		DefaultMode: modeSection{Mode: m.col.mode},
	}

	data, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(m.configPath), 0o755); err != nil {
		return fmt.Errorf("create .cece dir: %w", err)
	}
	return os.WriteFile(m.configPath, append(data, '\n'), 0o644)
}

func (m SetupModel) View() tea.View {
	var s string
	switch m.step {
	case stepWelcome:
		s = m.welcomeView()
	case stepProtocol, stepModel, stepMode:
		s = m.pickerView()
	case stepAPIKey:
		s = m.textView("2/5", "API Key")
	case stepBaseURL:
		s = m.textView("3/5", "Base URL")
	case stepDone:
		s = m.doneView()
	}
	return tea.NewView(s)
}

func (m SetupModel) welcomeView() string {
	var b strings.Builder
	b.WriteString("cece setup\n\n")
	if m.existing {
		fmt.Fprintf(&b, "Existing config found at %s\n", m.configPath)
		b.WriteString("Running setup will overwrite it.\n\n")
	} else {
		fmt.Fprintf(&b, "Config will be written to %s\n\n", m.configPath)
	}
	b.WriteString("[enter] start  [esc] quit")
	return b.String()
}

func (m SetupModel) pickerView() string {
	// Custom model input mode
	if m.step == stepModel && m.customInput {
		var b strings.Builder
		b.WriteString("[4/5] Default model\n")
		b.WriteString("Custom model ID: " + m.textInput + "▌\n")
		b.WriteString("[enter] confirm  [esc] back")
		return b.String()
	}
	if m.picker == nil {
		return ""
	}
	return m.picker.View()
}

func (m SetupModel) textView(stepNum, label string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[%s] %s\n", stepNum, label)
	if m.textInput == "" {
		b.WriteString("▌\n")
	} else {
		display := m.textInput
		if m.step == stepAPIKey && len(display) > 8 {
			display = display[:4] + strings.Repeat("*", len(display)-8) + display[len(display)-4:]
		}
		b.WriteString(display + "▌\n")
	}
	if m.err != "" {
		fmt.Fprintf(&b, "error: %s\n", m.err)
	}
	b.WriteString("[enter] next  [esc] back")
	return b.String()
}

func (m SetupModel) doneView() string {
	var b strings.Builder
	b.WriteString("Setup complete.\n\n")
	fmt.Fprintf(&b, "  protocol:  %s\n", m.col.protocol)
	fmt.Fprintf(&b, "  base URL:  %s\n", m.col.baseURL)
	fmt.Fprintf(&b, "  model:     %s\n", m.col.model)
	fmt.Fprintf(&b, "  mode:      %s\n", m.col.mode)
	keyPreview := m.col.apiKey
	if len(keyPreview) > 4 {
		keyPreview = keyPreview[:4] + "****"
	}
	fmt.Fprintf(&b, "  apiKey:    %s\n", keyPreview)
	b.WriteString("\nConfig written to " + m.configPath + "\n\n")
	b.WriteString("Run 'cece' to start.\n\n")
	b.WriteString("[enter] quit")
	return b.String()
}

// JSON structures for settings file output.

type providerEntry struct {
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	APIKey   string `json:"apiKey"`
	BaseURL  string `json:"baseURL"`
}

type providerSection struct {
	Model     []string        `json:"model"`
	Providers []providerEntry `json:"providers"`
}

type modeSection struct {
	Mode string `json:"mode"`
}

type settingsFile struct {
	Provider    providerSection `json:"provider"`
	DefaultMode modeSection     `json:"defaultMode"`
}
