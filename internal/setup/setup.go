package setup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/zhanglvtao/cece/internal/codebase"
	"github.com/zhanglvtao/cece/internal/ui/picker"
	"github.com/zhanglvtao/cece/internal/ui/theme"
)

var csiResidueRe = regexp.MustCompile(`^\[\d+(;\d+)*[~A-Za-z]$`)

const settingsRelPath = ".cece/settings.json"

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// step identifies the current wizard step.
type step int

const (
	stepWelcome step = iota
	stepProtocol
	stepBaseURL
	stepAPIKey
	stepLoading
	stepModel
	stepMode
	stepDone
)

// navigableSteps is the ordered list of steps the user can navigate with left/right.
var navigableSteps = []step{stepProtocol, stepBaseURL, stepAPIKey, stepModel, stepMode}

// selectMsg carries a picker selection back to Update.
type selectMsg struct{ value string }

// backMsg signals the user wants to go to the previous step.
type backMsg struct{}

// forwardMsg signals the user wants to go to the next step (when already filled).
type forwardMsg struct{}

// modelsLoadedMsg carries the result of fetching models from the API.
type modelsLoadedMsg struct {
	models []modelOption
	err    error
}

// tickMsg drives the spinner animation.
type tickMsg struct{}

// protocolOption is a picker item for protocol selection.
type protocolOption struct{ id string }

// modelOption is a picker item for model selection.
type modelOption struct {
	id               string
	name             string
	configName       string
	baseURL          string
	maxContextWindow int
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
	{id: "bytedance"},
}

var modes = []modeOption{
	{id: "default", desc: "Confirm before writes and shell commands"},
	{id: "auto-accept", desc: "Auto-approve all tool calls"},
	{id: "plan", desc: "LLM writes plan first, you review before execution"},
}

// collected stores the user's choices.
type collected struct {
	protocol         string
	apiKey           string
	baseURL          string
	model            string
	configName       string
	maxContextWindow int
	mode             string
}

// lipgloss styles for the setup wizard.
var (
	styleTitle         = lipgloss.NewStyle().Foreground(theme.Primary).Bold(true)
	styleStep          = lipgloss.NewStyle().Foreground(theme.Yellow)
	styleCursor        = lipgloss.NewStyle().Foreground(theme.Primary)
	styleError         = lipgloss.NewStyle().Foreground(theme.Red)
	styleHelp          = lipgloss.NewStyle().Foreground(theme.FgMuted)
	styleSuccess       = lipgloss.NewStyle().Foreground(theme.Green).Bold(true)
	styleSpinner       = lipgloss.NewStyle().Foreground(theme.Primary)
	styleLabel         = lipgloss.NewStyle().Foreground(theme.FgSubtle)
	styleValue         = lipgloss.NewStyle().Foreground(theme.Fg)
	styleKeyEnter      = lipgloss.NewStyle().Foreground(theme.Green).Bold(true)
	styleKeyEsc        = lipgloss.NewStyle().Foreground(theme.Red).Bold(true)
	styleKeyLeft       = lipgloss.NewStyle().Foreground(theme.Blue).Bold(true)
	styleKeyRight      = lipgloss.NewStyle().Foreground(theme.Blue).Bold(true)
	styleKeyOther      = lipgloss.NewStyle().Foreground(theme.Primary).Bold(true)
	styleProgress      = lipgloss.NewStyle().Foreground(theme.Primary)
	styleProgressDone  = lipgloss.NewStyle().Foreground(theme.Green)
	styleProgressEmpty = lipgloss.NewStyle().Foreground(theme.FgMuted)
	styleBox           = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(theme.Primary).Padding(0, 1)
)

// key renders a colored key label like [enter], [esc], etc.
func key(style *lipgloss.Style, text string) string {
	return style.Render("[" + text + "]")
}

// helpSep renders a dimmed separator between key hints.
const helpSep = "  "

// SetupModel is a standalone bubbletea model for the setup wizard.
type SetupModel struct {
	step          step
	col           collected
	picker        *picker.Picker
	textInput     string
	customInput   bool
	width         int
	height        int
	err           string
	existing      bool
	projectDir    string
	configPath    string
	spinnerIdx    int
	fetchErr      string
	fetchedModels []modelOption
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

// navigableIdx returns the index of s in navigableSteps, or -1.
func navigableIdx(s step) int {
	for i, ns := range navigableSteps {
		if ns == s {
			return i
		}
	}
	return -1
}

// stepLabel returns the display label for a navigable step.
func stepLabel(s step) string {
	switch s {
	case stepProtocol:
		return "Protocol"
	case stepBaseURL:
		return "Base URL"
	case stepAPIKey:
		return "API Key"
	case stepModel:
		return "Model"
	case stepMode:
		return "Mode"
	}
	return "?"
}

// stepFilled returns true if the step has a user-entered value.
func (m *SetupModel) stepFilled(s step) bool {
	switch s {
	case stepProtocol:
		return m.col.protocol != ""
	case stepBaseURL:
		return m.col.baseURL != ""
	case stepAPIKey:
		return m.col.apiKey != ""
	case stepModel:
		return m.col.model != ""
	case stepMode:
		return m.col.mode != ""
	}
	return false
}

// canGoBack returns true if the user can navigate to the previous step.
func (m *SetupModel) canGoBack() bool {
	idx := navigableIdx(m.step)
	return idx > 0
}

// canGoForward returns true if the current step is filled and there's a next step.
func (m *SetupModel) canGoForward() bool {
	idx := navigableIdx(m.step)
	if idx < 0 || idx >= len(navigableSteps)-1 {
		return false
	}
	return m.stepFilled(m.step)
}

// goBack navigates to the previous step.
func (m *SetupModel) goBack() (tea.Model, tea.Cmd) {
	idx := navigableIdx(m.step)
	if idx > 0 {
		return m.goToStep(navigableSteps[idx-1])
	}
	m.step = stepWelcome
	m.picker = nil
	return m, nil
}

// goForward navigates to the next step if the current one is filled.
func (m *SetupModel) goForward() (tea.Model, tea.Cmd) {
	idx := navigableIdx(m.step)
	if idx >= 0 && idx < len(navigableSteps)-1 && m.stepFilled(m.step) {
		return m.goToStep(navigableSteps[idx+1])
	}
	return m, nil
}

// goToStep transitions to a given step, restoring state.
func (m *SetupModel) goToStep(s step) (tea.Model, tea.Cmd) {
	m.step = s
	m.err = ""
	m.customInput = false
	m.picker = nil
	switch s {
	case stepProtocol:
		m.openPicker()
	case stepBaseURL:
		if m.col.protocol == "codebase" {
			m.col.baseURL = codebase.DefaultBaseURL
			m.step = stepLoading
			m.textInput = ""
			m.fetchErr = ""
			m.fetchedModels = nil
			cmd := m.fetchModelsCmd()
			tickCmd := tea.Tick(80*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg{} })
			return m, tea.Batch(cmd, tickCmd)
		}
		m.textInput = m.col.baseURL
	case stepAPIKey:
		m.textInput = m.col.apiKey
	case stepModel:
		if len(m.fetchedModels) > 0 {
			m.openModelPicker()
		} else {
			m.openModelPicker()
		}
	case stepMode:
		m.openPicker()
	}
	return m, nil
}

// progressBar renders a step progress indicator like ● ○ ○ ○ ○.
func (m SetupModel) progressBar() string {
	idx := navigableIdx(m.step)
	var b strings.Builder
	for i, s := range navigableSteps {
		if i > 0 {
			b.WriteString(styleProgressEmpty.Render(" ─ "))
		}
		if i < idx || m.stepFilled(s) {
			b.WriteString(styleProgressDone.Render("●"))
		} else if i == idx {
			b.WriteString(styleProgress.Render("●"))
		} else {
			b.WriteString(styleProgressEmpty.Render("○"))
		}
	}
	// Step labels below dots
	b.WriteString("\n")
	for i, s := range navigableSteps {
		if i > 0 {
			b.WriteString("      ") // align with " ─ "
		}
		if i == idx {
			b.WriteString(styleStep.Render(stepLabel(s)))
		} else if m.stepFilled(s) {
			b.WriteString(styleProgressDone.Render(stepLabel(s)))
		} else {
			b.WriteString(styleProgressEmpty.Render(stepLabel(s)))
		}
	}
	return b.String()
}

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
		// Left/right navigation (not in text input focus or picker open)
		switch msg.String() {
		case "left":
			if m.canGoBack() {
				return m.goBack()
			}
			return m, nil
		case "right":
			if m.canGoForward() {
				return m.goForward()
			}
			return m, nil
		}
	case selectMsg:
		return m.handleSelect(msg.value)
	case backMsg:
		return m.goBack()
	case forwardMsg:
		return m.goForward()
	case modelsLoadedMsg:
		return m.handleModelsLoaded(msg)
	case tickMsg:
		if m.step == stepLoading {
			m.spinnerIdx = (m.spinnerIdx + 1) % len(spinnerFrames)
			return m, tea.Tick(80*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg{} })
		}
		return m, nil
	}

	switch m.step {
	case stepWelcome:
		return m.updateWelcome(msg)
	case stepProtocol, stepModel, stepMode:
		return m.updatePicker(msg)
	case stepBaseURL, stepAPIKey:
		return m.updateTextInput(msg)
	case stepLoading:
		return m, nil
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
		if cmd != nil {
			return m, cmd
		}
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
			m.col.configName = ""
			m.col.maxContextWindow = 0
			m.customInput = false
			m.textInput = ""
			m.step = stepMode
			m.openPicker()
			return m, nil
		}
	case "esc":
		m.customInput = false
		m.textInput = ""
		m.openModelPicker()
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
			field := "Base URL"
			if m.step == stepAPIKey {
				field = "API key"
			}
			if m.col.protocol == "codebase" && m.step == stepBaseURL {
				input = codebase.DefaultBaseURL
			} else {
				m.err = field + " is required"
				return m, nil
			}
		}
		switch m.step {
		case stepBaseURL:
			m.col.baseURL = input
			if m.col.protocol == "codebase" {
				m.col.apiKey = ""
				m.step = stepLoading
				m.textInput = ""
				m.fetchErr = ""
				m.fetchedModels = nil
				cmd := m.fetchModelsCmd()
				tickCmd := tea.Tick(80*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg{} })
				return m, tea.Batch(cmd, tickCmd)
			}
			m.step = stepAPIKey
			m.textInput = ""
			return m, nil
		case stepAPIKey:
			m.col.apiKey = input
			m.step = stepLoading
			m.textInput = ""
			m.fetchErr = ""
			m.fetchedModels = nil
			cmd := m.fetchModelsCmd()
			tickCmd := tea.Tick(80*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg{} })
			return m, tea.Batch(cmd, tickCmd)
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

// fetchModelsCmd returns a tea.Cmd that fetches models from the provider API.
func (m SetupModel) fetchModelsCmd() tea.Cmd {
	protocol := m.col.protocol
	baseURL := m.col.baseURL
	apiKey := m.col.apiKey
	return func() tea.Msg {
		models, err := fetchModels(protocol, baseURL, apiKey)
		return modelsLoadedMsg{models: models, err: err}
	}
}

// handleModelsLoaded processes the result of the model fetch.
func (m SetupModel) handleModelsLoaded(msg modelsLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.fetchErr = msg.err.Error()
		m.fetchedModels = nil
	} else {
		m.fetchErr = ""
		m.fetchedModels = msg.models
	}
	m.step = stepModel
	m.openModelPicker()
	return m, nil
}

func (m SetupModel) modelOptionByID(id string) (modelOption, bool) {
	for _, mo := range m.fetchedModels {
		if mo.id == id {
			return mo, true
		}
	}
	return modelOption{}, false
}

// handleSelect processes a picker selection message.
func (m SetupModel) handleSelect(value string) (tea.Model, tea.Cmd) {
	switch m.step {
	case stepProtocol:
		m.col.protocol = value
		if value == "bytedance" {
			m.col.baseURL = "https://aiden-aiproxy.bytedance.net"
			m.step = stepAPIKey
		} else if value == "codebase" {
			m.col.baseURL = codebase.DefaultBaseURL
			m.step = stepLoading
			m.fetchErr = ""
			m.fetchedModels = nil
			cmd := m.fetchModelsCmd()
			tickCmd := tea.Tick(80*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg{} })
			return m, tea.Batch(cmd, tickCmd)
		} else {
			m.step = stepBaseURL
		}
		m.textInput = ""
	case stepModel:
		if value == "__custom__" {
			m.customInput = true
			m.textInput = ""
			return m, nil
		}
		m.col.model = value
		m.col.configName = ""
		m.col.maxContextWindow = 0
		if mo, ok := m.modelOptionByID(value); ok {
			m.col.configName = mo.configName
			m.col.maxContextWindow = mo.maxContextWindow
			if mo.baseURL != "" {
				m.col.baseURL = mo.baseURL
			}
		}
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
			id := item.(protocolOption).id
			line := id
			if id == "codebase" {
				line += "  coco plugin models"
			}
			return picker.FormatItem(line, selected)
		})
		p.SetOnSelect(func(item any) tea.Cmd {
			return func() tea.Msg { return selectMsg{value: item.(protocolOption).id} }
		})
		help := key(&styleKeyOther, "up/down") + " move" + helpSep + key(&styleKeyEnter, "enter") + " select" + helpSep + key(&styleKeyEsc, "esc") + " quit"
		p.SetHelpText(help)
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
		help := key(&styleKeyOther, "up/down") + " move" + helpSep + key(&styleKeyEnter, "enter") + " select" + helpSep + key(&styleKeyLeft, "←") + " back" + helpSep + key(&styleKeyEsc, "esc") + " back"
		p.SetHelpText(help)
		m.picker = p
	}
}

// openModelPicker initializes the model picker with fetched models or fallback.
func (m *SetupModel) openModelPicker() {
	m.picker = nil
	m.customInput = false

	models := make([]modelOption, len(m.fetchedModels))
	copy(models, m.fetchedModels)
	if m.fetchErr != "" {
		models = append(models, modelOption{id: "__custom__", name: "Custom input..."})
	}

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
		if opt.configName != "" && opt.configName != opt.id {
			name += "  " + opt.configName
		}
		return picker.FormatItem(name, selected)
	})
	p.SetFilterFn(func(item any, q string) bool {
		opt := item.(modelOption)
		return strings.Contains(strings.ToLower(opt.name+" "+opt.id+" "+opt.configName), strings.ToLower(q))
	})
	p.SetOnSelect(func(item any) tea.Cmd {
		return func() tea.Msg { return selectMsg{value: item.(modelOption).id} }
	})
	help := key(&styleKeyOther, "up/down") + " move" + helpSep + key(&styleKeyEnter, "enter") + " select" + helpSep + key(&styleKeyOther, "type") + " filter" + helpSep + key(&styleKeyLeft, "←") + " back" + helpSep + key(&styleKeyEsc, "esc") + " back"
	p.SetHelpText(help)
	m.picker = p
}

// save writes the collected config to .cece/settings.json in the project directory.
func (m *SetupModel) save() error {
	providers := []providerEntry{
		{
			Name:     m.col.protocol,
			Protocol: m.col.protocol,
			APIKey:   m.col.apiKey,
			BaseURL:  m.col.baseURL,
		},
	}
	if m.col.protocol == "codebase" {
		providers[0].AuthHelper = codebase.DefaultAuthHelper
		providers[0].Models = []staticModelEntry{{
			ID:               m.col.model,
			DisplayName:      m.col.model,
			MaxContextWindow: m.col.maxContextWindow,
			ConfigName:       m.col.configName,
		}}
		if providers[0].Models[0].ConfigName == "" {
			providers[0].Models[0].ConfigName = m.col.model
		}
	}

	sf := settingsFile{
		Provider: providerSection{
			Model:     []string{m.col.model},
			Providers: providers,
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
	case stepBaseURL:
		s = m.textView("2/5", "Base URL")
	case stepAPIKey:
		s = m.textView("3/5", "API Key")
	case stepLoading:
		s = m.loadingView()
	case stepDone:
		s = m.doneView()
	}
	return tea.NewView(s)
}

func (m SetupModel) welcomeView() string {
	var b strings.Builder
	b.WriteString(styleTitle.Render("cece setup") + "\n\n")
	// Progress bar showing all steps
	b.WriteString(m.progressBar() + "\n\n")
	if m.existing {
		fmt.Fprintf(&b, "%s Existing config found at %s\n", styleLabel.Render("⚠"), m.configPath)
		b.WriteString("Running setup will overwrite it.\n\n")
	} else {
		fmt.Fprintf(&b, "Config will be written to %s\n\n", m.configPath)
	}
	b.WriteString(key(&styleKeyEnter, "enter") + " start" + helpSep + key(&styleKeyEsc, "esc") + " quit")
	return b.String()
}

func (m SetupModel) pickerView() string {
	// Custom model input mode
	if m.step == stepModel && m.customInput {
		var b strings.Builder
		b.WriteString(styleStep.Render("[4/5]") + " Default model\n")
		b.WriteString(styleLabel.Render("Custom model ID: ") + m.textInput + styleCursor.Render("▌") + "\n")
		b.WriteString(key(&styleKeyEnter, "enter") + " confirm" + helpSep + key(&styleKeyLeft, "←") + " back" + helpSep + key(&styleKeyEsc, "esc") + " back")
		return b.String()
	}
	if m.picker == nil {
		return ""
	}
	return m.picker.View()
}

func (m SetupModel) textView(stepNum, label string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n", styleStep.Render("["+stepNum+"]"), label)
	if m.textInput == "" {
		b.WriteString(styleCursor.Render("▌") + "\n")
	} else {
		display := m.textInput
		if m.step == stepAPIKey && len(display) > 8 {
			display = display[:4] + strings.Repeat("*", len(display)-8) + display[len(display)-4:]
		}
		b.WriteString(display + styleCursor.Render("▌") + "\n")
	}
	if m.err != "" {
		fmt.Fprintf(&b, "\n%s %s\n", styleError.Render("error:"), m.err)
	}

	// Build navigation help line
	parts := []string{key(&styleKeyEnter, "enter") + " next"}
	if m.canGoBack() {
		parts = append(parts, key(&styleKeyLeft, "←")+" back")
	}
	parts = append(parts, key(&styleKeyEsc, "esc")+" back")
	b.WriteString("\n" + strings.Join(parts, helpSep))
	return b.String()
}

func (m SetupModel) loadingView() string {
	frame := spinnerFrames[m.spinnerIdx]
	var b strings.Builder
	target := m.col.baseURL
	if m.col.protocol == "codebase" {
		target = "coco plugins"
	}
	fmt.Fprintf(&b, "%s %s Fetching models from %s...\n\n", styleStep.Render("[4/5]"), styleSpinner.Render(frame), styleValue.Render(target))
	if m.col.protocol == "codebase" {
		b.WriteString(styleHelp.Render("Reading local coco plugin model configs") + "\n\n")
	} else {
		b.WriteString(styleHelp.Render("Querying available models from the provider") + "\n\n")
	}
	b.WriteString(key(&styleKeyOther, "ctrl+c") + " cancel")
	return b.String()
}

func (m SetupModel) doneView() string {
	// Build summary inside a bordered box
	var inner strings.Builder
	inner.WriteString(styleSuccess.Render("✓ Setup complete!") + "\n")
	inner.WriteString("\n")
	fmt.Fprintf(&inner, "  %s  %s\n", styleLabel.Render("protocol:"), styleValue.Render(m.col.protocol))
	fmt.Fprintf(&inner, "  %s  %s\n", styleLabel.Render("base URL:"), styleValue.Render(m.col.baseURL))
	fmt.Fprintf(&inner, "  %s     %s\n", styleLabel.Render("model:"), styleValue.Render(m.col.model))
	if m.col.protocol == "codebase" {
		if m.col.configName != "" {
			fmt.Fprintf(&inner, "  %s  %s\n", styleLabel.Render("config:"), styleValue.Render(m.col.configName))
		}
		if m.col.maxContextWindow > 0 {
			fmt.Fprintf(&inner, "  %s     %s\n", styleLabel.Render("ctx:"), styleValue.Render(fmt.Sprintf("%d", m.col.maxContextWindow)))
		}
	}
	fmt.Fprintf(&inner, "  %s      %s\n", styleLabel.Render("mode:"), styleValue.Render(m.col.mode))
	keyPreview := m.col.apiKey
	if len(keyPreview) > 4 {
		keyPreview = keyPreview[:4] + "****"
	}
	if m.col.protocol == "codebase" && keyPreview == "" {
		keyPreview = "authHelper"
	}
	fmt.Fprintf(&inner, "  %s    %s\n", styleLabel.Render("apiKey:"), styleValue.Render(keyPreview))
	inner.WriteString("\n" + styleHelp.Render("Config written to "+m.configPath))

	boxed := styleBox.Render(inner.String())
	var b strings.Builder
	b.WriteString(boxed + "\n\n")
	b.WriteString(styleHelp.Render("Run 'cece' to start.") + "\n")
	b.WriteString(key(&styleKeyEnter, "enter") + " quit" + helpSep + key(&styleKeyEsc, "esc") + " quit")
	return b.String()
}

// JSON structures for settings file output.

type providerEntry struct {
	Name       string             `json:"name"`
	Protocol   string             `json:"protocol"`
	APIKey     string             `json:"apiKey,omitempty"`
	BaseURL    string             `json:"baseURL"`
	AuthHelper string             `json:"authHelper,omitempty"`
	Models     []staticModelEntry `json:"models,omitempty"`
}

type staticModelEntry struct {
	ID               string `json:"id"`
	DisplayName      string `json:"displayName"`
	MaxContextWindow int    `json:"maxContextWindow,omitempty"`
	ConfigName       string `json:"configName,omitempty"`
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
