package tool

// Permission controls which tools require user confirmation before execution.
type Permission struct {
	autoApproved map[string]bool
}

// NewPermission creates a Permission that auto-approves the named tools.
// All other tools require explicit confirmation.
func NewPermission(autoApproved ...string) *Permission {
	m := make(map[string]bool, len(autoApproved))
	for _, name := range autoApproved {
		m[name] = true
	}
	return &Permission{autoApproved: m}
}

// NeedConfirm returns whether the named tool requires user confirmation.
func (p *Permission) NeedConfirm(toolName string) bool {
	if p == nil {
		return true
	}
	return !p.autoApproved[toolName]
}
