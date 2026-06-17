// Package effort provides reasoning effort level selection.
//
// Reasoning effort controls how much the model "thinks" before producing
// output. Higher effort = more reasoning tokens = better results on hard
// problems, at the cost of latency and token spend.
//
// Supported levels (aligned with Aiden):
//
//	low    – fast, simple tasks (search, lookup, sub-agents)
//	medium – balanced effort
//	high   – default for routine dev work
//	xhigh  – deep debugging, complex refactoring
//	auto   – keyword-based automatic selection
package effort

import "strings"

// ReasoningEffort is the reasoning effort level.
type ReasoningEffort string

const (
	Low    ReasoningEffort = "low"
	Medium ReasoningEffort = "medium"
	High   ReasoningEffort = "high"
	XHigh  ReasoningEffort = "xhigh"
	Auto   ReasoningEffort = "auto"
)

// Keywords that bump reasoning effort to XHigh.
var xhighKeywords = []string{
	"debug",
	"error",
	"bug",
	"crash",
	"fix",
}

// Keywords that drop reasoning effort to Low.
var lowKeywords = []string{
	"search",
	"lookup",
}

// SelectAuto resolves an "auto" effort to a concrete level based on the
// user's input text. Sub-agents always get Low.
//
// Rules:
//
//   - isSubAgent → Low
//   - input contains an xhigh keyword → XHigh
//   - input contains a low keyword → Low
//   - otherwise → High
func SelectAuto(isSubAgent bool, input string) ReasoningEffort {
	if isSubAgent {
		return Low
	}

	lower := strings.ToLower(input)

	for _, kw := range xhighKeywords {
		if strings.Contains(lower, kw) {
			return XHigh
		}
	}

	for _, kw := range lowKeywords {
		if strings.Contains(lower, kw) {
			return Low
		}
	}

	return High
}

// Resolve returns the concrete effort level for the given config value.
// If effort is "auto" (or empty), it uses SelectAuto on the input.
func Resolve(effort ReasoningEffort, isSubAgent bool, input string) ReasoningEffort {
	if effort == "" || effort == Auto {
		return SelectAuto(isSubAgent, input)
	}
	return effort
}

// Valid reports whether v is a known effort level (including auto).
func Valid(v string) bool {
	switch ReasoningEffort(v) {
	case Low, Medium, High, XHigh, Auto:
		return true
	default:
		return false
	}
}