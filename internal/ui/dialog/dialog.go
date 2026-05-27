package dialog

import (
	"image"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

const (
	defaultDialogHeight = 20
	titleContentHeight  = 1
	inputContentHeight  = 1
)

// CloseKey is the default key binding to close dialogs.
var CloseKey = key.NewBinding(
	key.WithKeys("esc", "alt+esc"),
	key.WithHelp("esc", "exit"),
)

// Action represents an action taken in a dialog after handling a message.
type Action any

// Dialog is a component that can be displayed on top of the UI.
type Dialog interface {
	ID() string
	HandleMsg(msg tea.Msg) Action
	Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor
	DesiredHeight() int
}

// ActionClose signals that the dialog should be closed.
type ActionClose struct{}

// ActionSelectSession signals that a session was selected.
type ActionSelectSession struct {
	ID    string
	Title string
}

// ActionSelectModel signals that a model was selected for switching.
type ActionSelectModel struct {
	ID               string
	DisplayName      string
	MaxContextWindow int
	Provider         string
	APIKey           string
	BaseURL          string
	AuthMode         string // "apikey" or "bearer"
	AuthHelper       string // shell command to fetch dynamic token
	Protocol         string // "anthropic" (default), "aiden", or "codebase"
	ConfigName       string
}

// ActionDeleteSession signals that a session should be deleted.
type ActionDeleteSession struct {
	ID string
}

// ActionRenameSession signals that a session should be renamed.
type ActionRenameSession struct {
	ID    string
	Title string
}

// ActionSaveSessionAndQuit signals that the current session should be kept and the app should quit.
type ActionSaveSessionAndQuit struct{}

// ActionDiscardSessionAndQuit signals that the current session should be discarded and the app should quit.
type ActionDiscardSessionAndQuit struct{}

// ActionConsumed signals that the dialog consumed the key event but no
// high-level action is needed. This prevents the key from falling through
// to the main model's input handler.
type ActionConsumed struct{}

// ActionCancelQuestion signals that the user cancelled the question dialog
// (e.g. via ctrl+c).
type ActionCancelQuestion struct{}

// ActionCmd wraps a tea.Cmd to be executed by the main model.
type ActionCmd struct{ Cmd tea.Cmd }

// Overlay manages multiple dialogs as an overlay.
type Overlay struct {
	dialogs []Dialog
}

// NewOverlay creates a new Overlay instance.
func NewOverlay(dialogs ...Dialog) *Overlay {
	return &Overlay{dialogs: dialogs}
}

// HasDialogs checks if there are any active dialogs.
func (d *Overlay) HasDialogs() bool {
	return len(d.dialogs) > 0
}

// ContainsDialog checks if a dialog with the specified ID exists.
func (d *Overlay) ContainsDialog(dialogID string) bool {
	for _, dialog := range d.dialogs {
		if dialog.ID() == dialogID {
			return true
		}
	}
	return false
}

// OpenDialog opens a new dialog to the stack.
func (d *Overlay) OpenDialog(dialog Dialog) {
	d.dialogs = append(d.dialogs, dialog)
}

// CloseDialog closes the dialog with the specified ID from the stack.
func (d *Overlay) CloseDialog(dialogID string) {
	for i, dialog := range d.dialogs {
		if dialog.ID() == dialogID {
			d.removeDialog(i)
			return
		}
	}
}

// CloseFrontDialog closes the front dialog in the stack.
func (d *Overlay) CloseFrontDialog() {
	if len(d.dialogs) == 0 {
		return
	}
	d.removeDialog(len(d.dialogs) - 1)
}

// Dialog returns the dialog with the specified ID, or nil if not found.
func (d *Overlay) Dialog(dialogID string) Dialog {
	for _, dialog := range d.dialogs {
		if dialog.ID() == dialogID {
			return dialog
		}
	}
	return nil
}

// DialogLast returns the front dialog, or nil if there are no dialogs.
func (d *Overlay) DialogLast() Dialog {
	if len(d.dialogs) == 0 {
		return nil
	}
	return d.dialogs[len(d.dialogs)-1]
}

// Update handles dialog updates.
func (d *Overlay) Update(msg tea.Msg) tea.Msg {
	if len(d.dialogs) == 0 {
		return nil
	}
	idx := len(d.dialogs) - 1
	dialog := d.dialogs[idx]
	if dialog == nil {
		return nil
	}
	return dialog.HandleMsg(msg)
}

// Draw renders the overlay and its dialogs.
func (d *Overlay) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	var cur *tea.Cursor
	for _, dialog := range d.dialogs {
		cur = dialog.Draw(scr, area)
	}
	return cur
}

func (d *Overlay) removeDialog(idx int) {
	if idx < 0 || idx >= len(d.dialogs) {
		return
	}
	d.dialogs = append(d.dialogs[:idx], d.dialogs[idx+1:]...)
}

// centerRect returns a Rectangle centered within the given area.
func centerRect(area uv.Rectangle, width, height int) uv.Rectangle {
	centerX := area.Min.X + area.Dx()/2
	centerY := area.Min.Y + area.Dy()/2
	minX := centerX - width/2
	minY := centerY - height/2
	return image.Rect(minX, minY, minX+width, minY+height)
}

// DrawCenter draws the given string view centered in the screen area.
func DrawCenter(scr uv.Screen, area uv.Rectangle, view string) {
	DrawCenterCursor(scr, area, view, nil)
}

// DrawCenterCursor draws the given string view centered in the screen area and
// adjusts the cursor position accordingly.
func DrawCenterCursor(scr uv.Screen, area uv.Rectangle, view string, cur *tea.Cursor) {
	width, height := lipgloss.Size(view)
	center := centerRect(area, width, height)
	if cur != nil {
		cur.X += center.Min.X
		cur.Y += center.Min.Y
	}
	uv.NewStyledString(view).Draw(scr, center)
}

// DrawInline draws the dialog view inline within the given area (like slash popup).
// Horizontally fills area width, vertically top-aligned with cursor offset.
func DrawInline(scr uv.Screen, area uv.Rectangle, view string, cur *tea.Cursor) {
	if cur != nil {
		cur.X += area.Min.X
		cur.Y += area.Min.Y
	}
	uv.NewStyledString(view).Draw(scr, area)
}
