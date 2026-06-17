package evals

// Layer identifies which evaluation layer a case belongs to.
type Layer string

const (
	LayerContract Layer = "contract"
	LayerBehavior Layer = "behavior"
	LayerScenario Layer = "scenario"
)

// Expectation captures the generic capabilities we care about when evaluating
// agent-to-agent interaction, independent of the eventual protocol shape.
type Expectation struct {
	NeedsExpansion      bool
	ShouldReadArtifact  bool
	ExpectedFinalAnswer string
	ExpectedArtifactRef bool
}

// Case defines a single evaluation input and its high-level expectations.
type Case struct {
	Name        string
	Layer       Layer
	Description string
	Prompt      string
	Expectation Expectation
}

// Metrics are collected from a single run.
type Metrics struct {
	ToolCalls         int
	ReadCalls         int
	ArtifactRefsSeen  int
	ExpandedOnDemand  bool
	ReturnedAnswer    string
	ContextCharacters int
}

// Outcome is the normalized result from an eval run.
type Outcome struct {
	Case        Case
	Passed      bool
	Failure     string
	Metrics     Metrics
	Transcript  []string
}
