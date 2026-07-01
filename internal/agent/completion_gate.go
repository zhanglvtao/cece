package agent


type ClosureEvidenceKind string

const (
	ClosureEvidenceCodeChange   ClosureEvidenceKind = "code_change"
	ClosureEvidenceVerification ClosureEvidenceKind = "verification"
	ClosureEvidenceRead         ClosureEvidenceKind = "read"
)

type ClosureEvidence struct {
	ToolUseID string
	Kind      ClosureEvidenceKind
	ToolName  string
	OK        bool
	Command   string
	Summary   string
}