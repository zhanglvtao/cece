package dialog

import (
	"cece/internal/chat"
	"cece/internal/ui/list"
	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

const ModelPickerID = "model-picker"

// ModelPicker is a dialog for selecting and switching models.
type ModelPicker struct {
	styles       DialogStyles
	help         help.Model
	list         *list.FilterableList
	input        textinput.Model
	models       []chat.ModelInfo
	currentModel string

	keyMap struct {
		Select   key.Binding
		Next     key.Binding
		Previous key.Binding
		UpDown   key.Binding
		Close    key.Binding
	}
}

var _ Dialog = (*ModelPicker)(nil)

// NewModelPicker creates a new ModelPicker dialog.
func NewModelPicker(styles DialogStyles, models []chat.ModelInfo, currentModel string) *ModelPicker {
	p := &ModelPicker{
		styles:       styles,
		models:       models,
		currentModel: currentModel,
	}

	p.help = help.New()
	p.list = list.NewFilterableList(modelPickerItems(styles, models, currentModel)...)
	p.list.Focus()

	// Select current model in list
	selectedInx := 0
	for i, m := range models {
		if m.ID == currentModel {
			selectedInx = i
			break
		}
	}
	p.list.SetSelected(selectedInx)

	p.input = textinput.New()
	p.input.SetVirtualCursor(false)
	p.input.Placeholder = "Filter models..."
	p.input.Focus()

	p.keyMap.Select = key.NewBinding(
		key.WithKeys("enter", "tab"),
		key.WithHelp("enter", "choose"),
	)
	p.keyMap.Next = key.NewBinding(
		key.WithKeys("down", "ctrl+n"),
		key.WithHelp("↓", "next"),
	)
	p.keyMap.Previous = key.NewBinding(
		key.WithKeys("up", "ctrl+p"),
		key.WithHelp("↑", "prev"),
	)
	p.keyMap.UpDown = key.NewBinding(
		key.WithKeys("up", "down"),
		key.WithHelp("↑↓", "navigate"),
	)
	p.keyMap.Close = CloseKey

	return p
}

// ID implements Dialog.
func (p *ModelPicker) ID() string { return ModelPickerID }

// HandleMsg implements Dialog.
func (p *ModelPicker) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, p.keyMap.Close):
			return ActionClose{}
		case key.Matches(msg, p.keyMap.Previous):
			p.list.Focus()
			if p.list.IsSelectedFirst() {
				p.list.SelectLast()
			} else {
				p.list.SelectPrev()
			}
			p.list.ScrollToSelected()
		case key.Matches(msg, p.keyMap.Next):
			p.list.Focus()
			if p.list.IsSelectedLast() {
				p.list.SelectFirst()
			} else {
				p.list.SelectNext()
			}
			p.list.ScrollToSelected()
		case key.Matches(msg, p.keyMap.Select):
			if item := p.list.SelectedItem(); item != nil {
				pi := item.(*ModelPickerItem)
				return ActionSelectModel{
					ID:               pi.ID,
					DisplayName:      pi.DisplayName,
					MaxContextWindow: pi.MaxContextWindow,
					Provider:         pi.Provider,
					APIKey:           pi.APIKey,
					BaseURL:          pi.BaseURL,
					AuthMode:         pi.AuthMode,
					AuthHelper:       pi.AuthHelper,
					Protocol:         pi.Protocol,
				}
			}
		default:
			var cmd tea.Cmd
			p.input, cmd = p.input.Update(msg)
			p.list.SetFilter(p.input.Value())
			p.list.ScrollToTop()
			p.list.SetSelected(0)
			return ActionCmd{cmd}
		}
	}
	return nil
}

// Draw implements Dialog.
func (p *ModelPicker) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := p.styles

	width := max(0, min(defaultDialogMaxWidth, area.Dx()-t.View.GetHorizontalBorderSize()))
	height := max(0, min(defaultDialogHeight, area.Dy()-t.View.GetVerticalBorderSize()))
	innerWidth := width - t.View.GetHorizontalFrameSize()
	heightOffset := t.Title.GetVerticalFrameSize() + titleContentHeight +
		t.InputPrompt.GetVerticalFrameSize() + inputContentHeight +
		t.HelpView.GetVerticalFrameSize() +
		t.View.GetVerticalFrameSize()
	p.input.SetWidth(max(0, innerWidth-t.InputPrompt.GetHorizontalFrameSize()-1))
	p.list.SetSize(innerWidth, height-heightOffset)
	p.help.SetWidth(innerWidth)

	var cur *tea.Cursor
	rc := NewRenderContext(t.Title, t.View, width)
	rc.Title = "Switch Model"

	inputView := t.InputPrompt.Render(p.input.View())
	cur = InputCursor(t.Title, t.View, t.InputPrompt, p.input.Cursor())
	rc.AddPart(inputView)

	listView := t.ListStyle.Height(p.list.Height()).Render(p.list.Render())
	rc.AddPart(listView)
	rc.Help = p.help.View(p)

	view := rc.Render()

	DrawCenterCursor(scr, area, view, cur)
	return cur
}

// ShortHelp implements help.KeyMap.
func (p *ModelPicker) ShortHelp() []key.Binding {
	return []key.Binding{p.keyMap.UpDown, p.keyMap.Select, p.keyMap.Close}
}

// FullHelp implements help.KeyMap.
func (p *ModelPicker) FullHelp() [][]key.Binding {
	slice := []key.Binding{p.keyMap.UpDown, p.keyMap.Select, p.keyMap.Close}
	var m [][]key.Binding
	for i := 0; i < len(slice); i += 4 {
		end := min(i+4, len(slice))
		m = append(m, slice[i:end])
	}
	return m
}
