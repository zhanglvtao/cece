package evals

import (
	"fmt"
	"strings"

	"github.com/zhanglvtao/cece/internal/testkit"
)

// Evaluate applies generic expectation checks to a harness run.
func Evaluate(c Case, h *testkit.Harness, finalAnswer string) Outcome {
	metrics := CollectMetrics(h, finalAnswer)
	transcript := BuildTranscript(h)
	out := Outcome{Case: c, Passed: true, Metrics: metrics, Transcript: transcript}

	if c.Expectation.ExpectedArtifactRef && metrics.ArtifactRefsSeen == 0 {
		out.Passed = false
		out.Failure = "expected artifact reference but none observed"
		return out
	}
	if c.Expectation.ShouldReadArtifact && metrics.ReadCalls == 0 {
		out.Passed = false
		out.Failure = "expected at least one Read expansion but none observed"
		return out
	}
	if !c.Expectation.ShouldReadArtifact && metrics.ReadCalls > 0 {
		out.Passed = false
		out.Failure = fmt.Sprintf("unexpected Read expansion: got %d", metrics.ReadCalls)
		return out
	}
	if want := strings.TrimSpace(c.Expectation.ExpectedFinalAnswer); want != "" {
		got := strings.TrimSpace(finalAnswer)
		if got != want {
			out.Passed = false
			out.Failure = fmt.Sprintf("final answer mismatch: got %q want %q", got, want)
			return out
		}
	}
	return out
}
