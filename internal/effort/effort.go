// Package effort provides reasoning effort level selection.
//
// Reasoning effort controls how much the model "thinks" before producing
// output. Higher effort = more reasoning tokens = better results on hard
// problems, at the cost of latency and token spend.
//
// Supported levels follow the de facto industry standard:
//
//	low    – fast, simple tasks (search, lookup, sub-agents)
//	high   – default for routine dev work
//	max    – deep debugging, complex refactoring
//	auto   – keyword-based automatic selection
package effort

import "strings"

// ReasoningEffort is the reasoning effort level.
type ReasoningEffort string

const (
	Low  ReasoningEffort = "low"
	High ReasoningEffort = "high"
	Max  ReasoningEffort = "max"
	Auto ReasoningEffort = "auto"
)

// Keywords that bump reasoning effort to Max.
var maxKeywords = []string{
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
//   - input contains a max keyword → Max
//   - input contains a low keyword → Low
//   - otherwise → High
func SelectAuto(isSubAgent bool, input string) ReasoningEffort {
	if isSubAgent {
		return Low
	}

	lower := strings.ToLower(input)

	for _, kw := range maxKeywords {
		if strings.Contains(lower, kw) {
			return Max
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
	case Low, High, Max, Auto:
		return true
	default:
		return false
	}
}