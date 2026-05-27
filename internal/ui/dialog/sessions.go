package dialog

import (
	"strings"
	"time"

	"cece/internal/ui/list"
	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

const SessionsID = "session"

type sessionsMode uint8

const (
	sessionsModeNormal sessionsMode = iota
	sessionsModeDeleting
	sessionsModeUpdating
)

// SessionInfo holds basic session metadata for the dialog.
type SessionInfo struct {
	ID        string
	Title     string
	UpdatedAt time.Time
}

// Session is a session selector dialog.
type Session struct {
	styles            DialogStyles
	help              help.Model
	list              *list.FilterableList
	input             textinput.Model
	selectedSessionID string
	sessions          []SessionInfo
	sessionsMode      sessionsMode

	keyMap struct {
		Select        key.Binding
		Next          key.Binding
		Previous      key.Binding
		UpDown        key.Binding
		Delete        key.Binding
		Rename        key.Binding
		ConfirmRename key.Binding
		CancelRename  key.Binding
		ConfirmDelete key.Binding
		CancelDelete  key.Binding
		Close         key.Binding
	}
}

var _ Dialog = (*Session)(nil)

// NewSessions creates a new Session dialog.
func NewSessions(styles DialogStyles, sessions []SessionInfo, selectedID string) *Session {
	s := &Session{
		styles:            styles,
		sessions:          sessions,
		sessionsMode:      sessionsModeNormal,
		selectedSessionID: selectedID,
	}

	s.help = help.New()
	s.list = list.NewFilterableList(sessionItems(styles, sessionsModeNormal, sessions...)...)
	s.list.Focus()

	selectedInx := 0
	for i, sess := range sessions {
		if sess.ID == selectedID {
			selectedInx = i
			break
		}
	}
	s.list.SetSelected(selectedInx)

	s.input = textinput.New()
	s.input.SetVirtualCursor(false)
	s.input.Placeholder = "Filter sessions..."
	s.input.Focus()

	s.keyMap.Select = key.NewBinding(
		key.WithKeys("enter", "tab", "ctrl+y"),
		key.WithHelp("enter", "choose"),
	)
	s.keyMap.Next = key.NewBinding(
		key.WithKeys("down", "ctrl+n"),
		key.WithHelp("↓", "next"),
	)
	s.keyMap.Previous = key.NewBinding(
		key.WithKeys("up", "ctrl+p"),
		key.WithHelp("↑", "prev"),
	)
	s.keyMap.UpDown = key.NewBinding(
		key.WithKeys("up", "down"),
		key.WithHelp("↑↓", "navigate"),
	)
	s.keyMap.Delete = key.NewBinding(
		key.WithKeys("ctrl+x"),
		key.WithHelp("ctrl+x", "delete"),
	)
	s.keyMap.Rename = key.NewBinding(
		key.WithKeys("ctrl+r"),
		key.WithHelp("ctrl+r", "rename"),
	)
	s.keyMap.ConfirmRename = key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "confirm"),
	)
	s.keyMap.CancelRename = key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "cancel"),
	)
	s.keyMap.ConfirmDelete = key.NewBinding(
		key.WithKeys("y"),
		key.WithHelp("y", "delete"),
	)
	s.keyMap.CancelDelete = key.NewBinding(
		key.WithKeys("n", "esc"),
		key.WithHelp("n", "cancel"),
	)
	s.keyMap.Close = CloseKey

	return s
}

// ID implements Dialog.
func (s *Session) ID() string { return SessionsID }

// DesiredHeight implements Dialog.
func (s *Session) DesiredHeight() int { return 20 }

// HandleMsg implements Dialog.
func (s *Session) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch s.sessionsMode {
		case sessionsModeDeleting:
			switch {
			case key.Matches(msg, s.keyMap.ConfirmDelete):
				action := s.confirmDeleteSession()
				s.list.SetItems(sessionItems(s.styles, sessionsModeNormal, s.sessions...)...)
				s.list.SelectFirst()
				s.list.ScrollToSelected()
				return action
			case key.Matches(msg, s.keyMap.CancelDelete):
				s.sessionsMode = sessionsModeNormal
				s.list.SetItems(sessionItems(s.styles, sessionsModeNormal, s.sessions...)...)
			}
		case sessionsModeUpdating:
			switch {
			case key.Matches(msg, s.keyMap.ConfirmRename):
				action := s.confirmRenameSession()
				s.list.SetItems(sessionItems(s.styles, sessionsModeNormal, s.sessions...)...)
				return action
			case key.Matches(msg, s.keyMap.CancelRename):
				s.sessionsMode = sessionsModeNormal
				s.list.SetItems(sessionItems(s.styles, sessionsModeNormal, s.sessions...)...)
			default:
				item := s.list.SelectedItem()
				if item == nil {
					return nil
				}
				if sessionItem, ok := item.(*SessionItem); ok {
					return sessionItem.HandleInput(msg)
				}
			}
		default:
			switch {
			case key.Matches(msg, s.keyMap.Close):
				return ActionClose{}
			case key.Matches(msg, s.keyMap.Rename):
				s.sessionsMode = sessionsModeUpdating
				s.list.SetItems(sessionItems(s.styles, sessionsModeUpdating, s.sessions...)...)
			case key.Matches(msg, s.keyMap.Delete):
				s.sessionsMode = sessionsModeDeleting
				s.list.SetItems(sessionItems(s.styles, sessionsModeDeleting, s.sessions...)...)
			case key.Matches(msg, s.keyMap.Previous):
				s.list.Focus()
				if s.list.IsSelectedFirst() {
					s.list.SelectLast()
				} else {
					s.list.SelectPrev()
				}
				s.list.ScrollToSelected()
			case key.Matches(msg, s.keyMap.Next):
				s.list.Focus()
				if s.list.IsSelectedLast() {
					s.list.SelectFirst()
				} else {
					s.list.SelectNext()
				}
				s.list.ScrollToSelected()
			case key.Matches(msg, s.keyMap.Select):
				if item := s.list.SelectedItem(); item != nil {
					sessionItem := item.(*SessionItem)
					return ActionSelectSession{
						ID:    sessionItem.ID(),
						Title: sessionItem.Title,
					}
				}
			default:
				var cmd tea.Cmd
				s.input, cmd = s.input.Update(msg)
				s.list.SetFilter(s.input.Value())
				s.list.ScrollToTop()
				s.list.SetSelected(0)
				return ActionCmd{cmd}
			}
		}
	}
	return nil
}

// Draw implements Dialog.
func (s *Session) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := s.styles

	// Mode-specific border color: destructive for delete, warning for rename.
	viewStyle := t.View
	titleStyle := t.Title
	switch s.sessionsMode {
	case sessionsModeDeleting:
		viewStyle = viewStyle.BorderForeground(t.DeletingMessage.GetForeground())
		titleStyle = titleStyle.Foreground(t.DeletingMessage.GetForeground())
	case sessionsModeUpdating:
		viewStyle = viewStyle.BorderForeground(t.RenamingMessage.GetForeground())
		titleStyle = titleStyle.Foreground(t.RenamingMessage.GetForeground())
	}

	width := max(0, area.Dx()-viewStyle.GetHorizontalBorderSize())
	height := max(0, min(defaultDialogHeight, area.Dy()-viewStyle.GetVerticalBorderSize()))
	innerWidth := width - viewStyle.GetHorizontalFrameSize()
	heightOffset := titleStyle.GetVerticalFrameSize() + titleContentHeight +
		t.InputPrompt.GetVerticalFrameSize() + inputContentHeight +
		t.HelpView.GetVerticalFrameSize() +
		viewStyle.GetVerticalFrameSize()
	s.input.SetWidth(max(0, innerWidth-t.InputPrompt.GetHorizontalFrameSize()-1))
	s.list.SetSize(innerWidth, height-heightOffset)
	s.help.SetWidth(innerWidth)

	start, end := s.list.VisibleItemIndices()
	_ = start
	_ = end

	var cur *tea.Cursor
	rc := NewRenderContext(titleStyle, viewStyle, width)

	switch s.sessionsMode {
	case sessionsModeDeleting:
		rc.Title = "Delete session?"
	case sessionsModeUpdating:
		rc.Title = "Rename session?"
	default:
		rc.Title = "Sessions"
	}

	switch s.sessionsMode {
	case sessionsModeDeleting:
		rc.AddPart(t.DeletingMessage.Render("This action cannot be undone."))
	case sessionsModeUpdating:
		item := s.selectedSessionItem()
		if item != nil && item.Cursor() != nil {
			cur = item.Cursor()
		}
	default:
		inputView := t.InputPrompt.Render(s.input.View())
		cur = InputCursor(titleStyle, viewStyle, t.InputPrompt, s.input.Cursor())
		rc.AddPart(inputView)
	}

	listView := t.ListStyle.Height(s.list.Height()).Render(s.list.Render())
	rc.AddPart(listView)
	rc.Help = s.help.View(s)

	view := rc.Render()

	DrawInline(scr, area, view, cur)
	return cur
}

func (s *Session) selectedSessionItem() *SessionItem {
	if item := s.list.SelectedItem(); item != nil {
		return item.(*SessionItem)
	}
	return nil
}

func (s *Session) confirmDeleteSession() Action {
	sessionItem := s.selectedSessionItem()
	s.sessionsMode = sessionsModeNormal
	if sessionItem == nil {
		return nil
	}
	id := sessionItem.ID()
	s.removeSession(id)
	return ActionDeleteSession{ID: id}
}

func (s *Session) removeSession(id string) {
	var newSessions []SessionInfo
	for _, sess := range s.sessions {
		if sess.ID != id {
			newSessions = append(newSessions, sess)
		}
	}
	s.sessions = newSessions
}

func (s *Session) confirmRenameSession() Action {
	sessionItem := s.selectedSessionItem()
	s.sessionsMode = sessionsModeNormal
	if sessionItem == nil {
		return nil
	}
	newTitle := strings.TrimSpace(sessionItem.InputValue())
	if newTitle == "" {
		return nil
	}
	id := sessionItem.ID()
	for i, sess := range s.sessions {
		if sess.ID == id {
			s.sessions[i].Title = newTitle
			break
		}
	}
	return ActionRenameSession{ID: id, Title: newTitle}
}

// ShortHelp implements help.KeyMap.
func (s *Session) ShortHelp() []key.Binding {
	switch s.sessionsMode {
	case sessionsModeDeleting:
		return []key.Binding{s.keyMap.ConfirmDelete, s.keyMap.CancelDelete}
	case sessionsModeUpdating:
		return []key.Binding{s.keyMap.ConfirmRename, s.keyMap.CancelRename}
	default:
		return []key.Binding{s.keyMap.UpDown, s.keyMap.Rename, s.keyMap.Delete, s.keyMap.Select, s.keyMap.Close}
	}
}

// FullHelp implements help.KeyMap.
func (s *Session) FullHelp() [][]key.Binding {
	var slice []key.Binding
	switch s.sessionsMode {
	case sessionsModeDeleting:
		slice = []key.Binding{s.keyMap.ConfirmDelete, s.keyMap.CancelDelete}
	case sessionsModeUpdating:
		slice = []key.Binding{s.keyMap.ConfirmRename, s.keyMap.CancelRename}
	default:
		slice = []key.Binding{s.keyMap.UpDown, s.keyMap.Rename, s.keyMap.Delete, s.keyMap.Select, s.keyMap.Close}
	}
	var m [][]key.Binding
	for i := 0; i < len(slice); i += 4 {
		end := min(i+4, len(slice))
		m = append(m, slice[i:end])
	}
	return m
}
