package ui

import (
	"fmt"
	"strings"
)

type slashSpec struct {
	Command string
	Query   string
	Args    string
	Active  bool
	HasArgs bool
}

func parseSlashSpec(input string) slashSpec {
	trimmed := strings.TrimLeft(input, " \t")
	if !strings.HasPrefix(trimmed, "/") {
		return slashSpec{}
	}
	body := strings.TrimPrefix(trimmed, "/")
	commandPart, args, hasArgs := body, "", false
	if idx := strings.IndexAny(body, " \t\n"); idx >= 0 {
		commandPart = body[:idx]
		args = strings.TrimSpace(body[idx+1:])
		hasArgs = true
	}
	return slashSpec{
		Command: "/" + commandPart,
		Query:   commandPart,
		Args:    args,
		Active:  true,
		HasArgs: hasArgs,
	}
}

func formatSlashUnknown(command string) string {
	return fmt.Sprintf("Unknown command: %s", command)
}

func slashHighlightValue(_ Styles, input string) string { return input }
