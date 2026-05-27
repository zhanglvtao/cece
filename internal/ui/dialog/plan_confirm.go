package dialog

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

const PlanConfirmID = "confirm-plan"

// ActionApprovePlan signals that the user approved the plan.
type ActionApprovePlan struct{}

// ActionRejectPlan signals that the user rejected the plan (stay in plan mode).
type ActionRejectPlan struct{}

// PlanConfirm is a dialog that asks the user to approve a plan.
type PlanConfirm struct {
	styles   DialogStyles
	help     help.Model
	planFile string
}

var _ Dialog = (*PlanConfirm)(nil)

// NewPlanConfirm creates a new plan confirmation dialog.
func NewPlanConfirm(styles DialogStyles, planFile string) *PlanConfirm {
	d := &PlanConfirm{
		styles:   styles,
		planFile: planFile,
	}
	d.help = help.New()
	return d
}

// ID implements Dialog.
func (p *PlanConfirm) ID() string { return PlanConfirmID }

// DesiredHeight implements Dialog.
func (p *PlanConfirm) DesiredHeight() int { return 8 }

// HandleMsg implements Dialog.
func (p *PlanConfirm) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "y":
			return ActionApprovePlan{}
		case "n", "esc":
			return ActionRejectPlan{}
		}
	}
	return nil
}

// Draw implements Dialog.
func (p *PlanConfirm) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := p.styles
	width := max(0, area.Dx()-t.View.GetHorizontalBorderSize())

	rc := NewRenderContext(t.Title, t.View, width)
	rc.Title = "Approve Plan?"
	rc.Gap = 1

	lines := []string{}
	if p.planFile != "" {
		lines = append(lines, "  "+t.InfoBlurred.Render("Plan: ")+t.InfoFocused.Render(p.planFile))
	}
	lines = append(lines, "  "+t.InfoBlurred.Render("Review the plan above, then choose:"))
	lines = append(lines, "  "+t.DeletingMessage.Render("Press [n] to reject and continue editing."))
	panelContent := strings.Join(lines, "\n")
	rc.AddPart(t.ContentPanel.Width(width - t.View.GetHorizontalFrameSize()).Render(panelContent))

	approveBtn := t.AllowBtn.Render("[y] approve")
	rejectBtn := t.DenyBtn.Render("[n] reject")
	rc.Help = fmt.Sprintf("%s  %s", approveBtn, rejectBtn)

	view := rc.Render()
	DrawInline(scr, area, view, nil)
	return nil
}

// ShortHelp implements help.KeyMap.
func (p *PlanConfirm) ShortHelp() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "approve")),
		key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "reject")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "reject")),
	}
}

// FullHelp implements help.KeyMap.
func (p *PlanConfirm) FullHelp() [][]key.Binding {
	return [][]key.Binding{p.ShortHelp()}
}
