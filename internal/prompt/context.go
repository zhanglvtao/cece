package prompt

import (
	"context"
	"time"
)

type SessionContext struct {
	RepoRoot           string
	IsGitRepo          bool
	OSName             string
	OSVersion          string
	SessionStartBranch string
	CLAUDEmd           string // project instructions from CLAUDE.md
	ToolDescriptions   string // rendered tool summary text
	ModelName          string // current model identifier
	SkillListing       string // rendered <available_skills> XML
}

// SessionCollector abstracts session context data collection.
// Implementations gather environment info, project instructions, etc.
type SessionCollector interface {
	Collect(ctx context.Context) (SessionContext, error)
}

type TurnContext struct {
	IncludeTime             bool
	Now                     time.Time
	CurrentWorkingDirectory string
	CurrentBranch           string
	Mode                    string
	ConversationTurnNumber  int // which turn in the conversation (1-based)
}
