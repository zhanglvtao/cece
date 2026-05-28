package ui

import (
	"image/color"
	"regexp"
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// csiResidueRe matches the visible portion of a terminal CSI escape sequence
// that leaked into a KeyPress message as text. Example: [27;5;106~
// These appear when ultraviolet fails to fully consume a modifyOtherKeys
// sequence like \x1b[27;5;106~ (Ctrl+J) and the printable remainder is
// treated as ordinary text input.
var csiResidueRe = regexp.MustCompile(`^\[\d+(;\d+)*[~A-Za-z]$`)

const (
	inputMinHeight = 3
	inputMaxHeight = 10
)

// Input is the editor input component with prompt history navigation.
type Input struct {
	styles  Styles
	ta      textarea.Model
	history promptHistory
}

type promptHistory struct {
	messages []string
	index    int
	draft    string
}

// NewInput creates a new Input component.
func NewInput(styles Styles) *Input {
	ta := textarea.New()
	ta.Placeholder = "Send a message…"
	ta.ShowLineNumbers = false
	ta.CharLimit = -1
	ta.SetVirtualCursor(false)
	ta.DynamicHeight = true
	ta.MinHeight = inputMinHeight
	ta.MaxHeight = inputMaxHeight
	ta.Focus()

	return &Input{
		styles: styles,
		ta:     ta,
		history: promptHistory{
			index: -1,
		},
	}
}

// frameSize returns the horizontal and vertical frame size (border+padding)
// of the input box style.
func (i *Input) frameSize() (hFrame, vFrame int) {
	// Both focused and blurred have the same frame size
	hFrame = i.styles.Input.BoxFocused.GetHorizontalFrameSize()
	vFrame = i.styles.Input.BoxFocused.GetVerticalFrameSize()
	return
}

// Update delegates a tea.Msg to the textarea and returns any cmd.
// It filters out CSI escape sequence residues that leak into KeyPress messages.
func (i *Input) Update(msg tea.Msg) tea.Cmd {
	if k, ok := msg.(tea.KeyPressMsg); ok && csiResidueRe.MatchString(k.Key().Text) {
		return nil
	}
	var cmd tea.Cmd
	i.ta, cmd = i.ta.Update(msg)
	return cmd
}

// Value returns the current input text.
func (i *Input) Value() string {
	return i.ta.Value()
}

// SetValue replaces the input text.
func (i *Input) SetValue(v string) {
	i.ta.SetValue(v)
}

// Reset clears the input and history position.
func (i *Input) Reset() {
	i.ta.Reset()
	i.history.index = -1
	i.history.draft = ""
}

// SetWidth sets the input width (including the box frame).
func (i *Input) SetWidth(w int) {
	hFrame, _ := i.frameSize()
	i.ta.SetWidth(w - hFrame)
}

// Height returns the current input height including the box frame.
func (i *Input) Height() int {
	_, vFrame := i.frameSize()
	return i.ta.Height() + vFrame
}

// Focus focuses the textarea.
func (i *Input) Focus() {
	i.ta.Focus()
}

// Blur blurs the textarea.
func (i *Input) Blur() {
	i.ta.Blur()
}

// Cursor returns the textarea cursor for screen rendering.
// The coordinates are offset to account for the box border and padding.
func (i *Input) Cursor() *tea.Cursor {
	cur := i.ta.Cursor()
	if cur == nil {
		return nil
	}
	hFrame, vFrame := i.frameSize()
	cur.X += i.styles.Input.BoxFocused.GetBorderLeftSize() +
		i.styles.Input.BoxFocused.GetPaddingLeft()
	cur.Y += vFrame - i.styles.Input.BoxFocused.GetBorderBottomSize() -
		i.styles.Input.BoxFocused.GetPaddingBottom()
	_ = hFrame // hFrame already accounted for in X via border+padding
	return cur
}

// InsertRune inserts a rune at the cursor position.
func (i *Input) InsertRune(r rune) {
	i.ta.InsertRune(r)
}

// ScrollBy moves the cursor by n lines (negative = up). The textarea
// repositions its viewport to keep the cursor visible, so this also
// scrolls the visible content. Used to translate mouse-wheel events that
// land inside the input box into a natural cursor/viewport movement.
func (i *Input) ScrollBy(n int) {
	if n < 0 {
		for k := 0; k < -n; k++ {
			i.ta.CursorUp()
		}
		return
	}
	for k := 0; k < n; k++ {
		i.ta.CursorDown()
	}
}

// SetPromptStyle configures the textarea with no prompt (empty) and full styles.
func (i *Input) SetPromptStyle() {
	i.ta.SetPromptFunc(0, func(info textarea.PromptInfo) string {
		return ""
	})
	i.ta.SetStyles(i.styles.Input.Textarea)
}

// HistoryUp navigates to the previous prompt in history.
// Returns true if history was navigated, false if the textarea should handle ↑ normally.
func (i *Input) HistoryUp() bool {
	if i.ta.Length() == 0 || i.isAtStart() {
		return i.historyPrev()
	}
	if i.ta.Line() == 0 {
		i.ta.CursorStart()
		return false
	}
	return false
}

// HistoryDown navigates to the next prompt in history.
// Returns true if history was navigated, false if the textarea should handle ↓ normally.
func (i *Input) HistoryDown() bool {
	if i.isAtEnd() {
		return i.historyNext()
	}
	if i.ta.Line() >= i.ta.LineCount()-1 {
		i.ta.MoveToEnd()
		return false
	}
	return false
}

// AddHistory appends a message to the prompt history.
func (i *Input) AddHistory(msg string) {
	if msg == "" {
		return
	}
	// Deduplicate: don't add if same as last entry
	if len(i.history.messages) > 0 && i.history.messages[0] == msg {
		return
	}
	i.history.messages = append([]string{msg}, i.history.messages...)
}

// Draw renders the floating input box onto the screen.
// busy controls the border style: thick line when busy, thin line when idle.
// borderColor overrides the border foreground when non-nil (used for breathing animation).
func (i *Input) Draw(scr uv.Screen, area uv.Rectangle, busy bool, borderColor color.Color) {
	var boxStyle lipgloss.Style
	if busy {
		boxStyle = i.styles.Input.BoxBusy
	} else {
		boxStyle = i.styles.Input.BoxIdle
	}
	if borderColor != nil {
		boxStyle = boxStyle.BorderForeground(borderColor)
	}

	inputView := i.ta.View()
	boxed := boxStyle.Width(area.Dx()).Height(area.Dy()).Render(inputView)
	uv.NewStyledString(boxed).Draw(scr, area)
	i.drawSlashHighlight(scr, area, boxStyle)
}

func (i *Input) drawSlashHighlight(scr uv.Screen, area uv.Rectangle, boxStyle lipgloss.Style) {
	highlighted := slashHighlightValue(i.styles, i.ta.Value())
	if highlighted == i.ta.Value() {
		return
	}
	line, ok := firstVisibleLine(highlighted)
	if !ok {
		return
	}
	textWidth := max(0, area.Dx()-boxStyle.GetHorizontalFrameSize())
	line = ansi.Truncate(line, textWidth, "")
	lineArea := uv.Rect(
		area.Min.X+boxStyle.GetBorderLeftSize()+boxStyle.GetPaddingLeft(),
		area.Min.Y+boxStyle.GetBorderTopSize()+boxStyle.GetPaddingTop(),
		textWidth,
		1,
	)
	uv.NewStyledString(line).Draw(scr, lineArea)
}

func firstVisibleLine(v string) (string, bool) {
	if v == "" {
		return "", false
	}
	for _, line := range strings.Split(v, "\n") {
		if strings.TrimSpace(ansi.Strip(line)) != "" {
			return line, true
		}
	}
	return "", false
}

func (i *Input) SlashSpec() slashSpec {
	return parseSlashSpec(i.ta.Value())
}

// isAtStart returns true if the cursor is at position (0, 0).
func (i *Input) isAtStart() bool {
	return i.ta.Line() == 0 && i.ta.LineInfo().ColumnOffset == 0
}

// isAtEnd returns true if the cursor is at the end of the text.
func (i *Input) isAtEnd() bool {
	lineCount := i.ta.LineCount()
	if lineCount == 0 {
		return true
	}
	if i.ta.Line() != lineCount-1 {
		return false
	}
	info := i.ta.LineInfo()
	return info.CharOffset >= info.CharWidth-1 || info.CharWidth == 0
}

// historyPrev navigates to an older history entry.
func (i *Input) historyPrev() bool {
	if len(i.history.messages) == 0 {
		return false
	}
	if i.history.index == -1 {
		i.history.draft = i.ta.Value()
	}
	nextIdx := i.history.index + 1
	if nextIdx >= len(i.history.messages) {
		return false
	}
	i.history.index = nextIdx
	i.ta.Reset()
	i.ta.InsertString(i.history.messages[nextIdx])
	i.ta.MoveToBegin()
	return true
}

// historyNext navigates to a newer history entry.
func (i *Input) historyNext() bool {
	if i.history.index < 0 {
		return false
	}
	nextIdx := i.history.index - 1
	if nextIdx < 0 {
		i.history.index = -1
		i.ta.Reset()
		i.ta.InsertString(i.history.draft)
		return true
	}
	i.history.index = nextIdx
	i.ta.Reset()
	i.ta.InsertString(i.history.messages[nextIdx])
	return true
}
