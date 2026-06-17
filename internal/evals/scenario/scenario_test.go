package scenario_test

import (
	"strings"
	"testing"

	"github.com/zhanglvtao/cece/internal/evals"
	"github.com/zhanglvtao/cece/internal/testkit"
)

func TestScenario_CodeAnalysis_LongReportThenReadAndAnswer(t *testing.T) {
	fs := testkit.NewFakeFS(map[string]string{
		"/tmp/code-analysis.txt": strings.Join([]string{
			"repo overview",
			"module graph",
			"critical finding: nil dereference in session loader",
			"suggested fix: guard nil session before dereference",
		}, "\n"),
	})
	llm := testkit.NewScriptedClient(
		testkit.ToolUseTurn("read-1", "Read", `{"file_path":"/tmp/code-analysis.txt"}`),
		testkit.TextTurn("fix nil guard in session loader"),
	)
	h := testkit.NewHarness(t, llm, testkit.WithExtraTools(testkit.NewFakeRead(fs)))
	out := evals.NewRunner(t).RunWithHarness(evals.Case{
		Name:        "scenario-code-analysis-read-then-answer",
		Layer:       evals.LayerScenario,
		Description: "parent agent should expand a long analysis report before deciding next action",
		Prompt:      "analyze the repository report and tell me the next fix",
		Expectation: evals.Expectation{
			ShouldReadArtifact:  true,
			ExpectedFinalAnswer: "fix nil guard in session loader",
		},
	}, h)
	if !out.Passed {
		t.Fatalf("eval failed: %s\ntranscript=%v", out.Failure, out.Transcript)
	}
}

func TestScenario_PlanPreviewEnough_NoExtraRead(t *testing.T) {
	llm := testkit.NewScriptedClient(
		testkit.TextTurn("next step: implement session artifact store"),
	)
	h := testkit.NewHarness(t, llm)
	out := evals.NewRunner(t).RunWithHarness(evals.Case{
		Name:        "scenario-plan-preview-enough",
		Layer:       evals.LayerScenario,
		Description: "when preview already contains the actionable next step, no expansion should happen",
		Prompt:      "summarize the next implementation step from the plan preview",
		Expectation: evals.Expectation{
			ShouldReadArtifact:  false,
			ExpectedFinalAnswer: "next step: implement session artifact store",
		},
	}, h)
	if !out.Passed {
		t.Fatalf("eval failed: %s\ntranscript=%v", out.Failure, out.Transcript)
	}
}

func TestScenario_TailCriticalAnswer_RequiresExpansion(t *testing.T) {
	fs := testkit.NewFakeFS(map[string]string{
		"/tmp/fusion-reducer.txt": strings.Join([]string{
			"worker-a agrees with plan A",
			"worker-b prefers plan B",
			"reducer conclusion at tail: choose plan B because it preserves decoupling",
		}, "\n"),
	})
	llm := testkit.NewScriptedClient(
		testkit.ToolUseTurn("read-1", "Read", `{"file_path":"/tmp/fusion-reducer.txt"}`),
		testkit.TextTurn("choose plan B because it preserves decoupling"),
	)
	h := testkit.NewHarness(t, llm, testkit.WithExtraTools(testkit.NewFakeRead(fs)))
	out := evals.NewRunner(t).RunWithHarness(evals.Case{
		Name:        "scenario-tail-critical-answer",
		Layer:       evals.LayerScenario,
		Description: "critical reducer conclusion hidden in tail should force expansion",
		Prompt:      "read the reducer result and tell me which plan to choose",
		Expectation: evals.Expectation{
			ShouldReadArtifact:  true,
			ExpectedFinalAnswer: "choose plan B because it preserves decoupling",
		},
	}, h)
	if !out.Passed {
		t.Fatalf("eval failed: %s\ntranscript=%v", out.Failure, out.Transcript)
	}
}
