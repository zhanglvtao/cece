package behavior_test

import (
	"strings"
	"testing"

	"github.com/zhanglvtao/cece/internal/evals"
	"github.com/zhanglvtao/cece/internal/testkit"
)

func TestAgentBehavior_NoUnnecessaryReadWhenPreviewIsEnough(t *testing.T) {
	llm := testkit.NewScriptedClient(
		testkit.TextTurn("short answer is enough"),
	)
	h := testkit.NewHarness(t, llm)
	out := evals.NewRunner(t).RunWithHarness(evals.Case{
		Name:        "behavior-no-unnecessary-read",
		Layer:       evals.LayerBehavior,
		Description: "agent should not expand when preview is already sufficient",
		Prompt:      "answer directly from the available preview",
		Expectation: evals.Expectation{
			ShouldReadArtifact:  false,
			ExpectedFinalAnswer: "short answer is enough",
		},
	}, h)
	if !out.Passed {
		t.Fatalf("eval failed: %s\ntranscript=%v", out.Failure, out.Transcript)
	}
	if got := llm.Calls(); got != 1 {
		t.Fatalf("llm calls = %d, want 1", got)
	}
}

func TestAgentBehavior_ExpandOnDemandWhenAnswerIsInArtifact(t *testing.T) {
	fs := testkit.NewFakeFS(map[string]string{
		"/tmp/report.txt": "summary only in preview\n\nFINAL_ANSWER=venus",
	})
	llm := testkit.NewScriptedClient(
		testkit.ToolUseTurn("read-1", "Read", `{"file_path":"/tmp/report.txt"}`),
		testkit.TextTurn("venus"),
	)
	h := testkit.NewHarness(t, llm, testkit.WithExtraTools(testkit.NewFakeRead(fs)))
	out := evals.NewRunner(t).RunWithHarness(evals.Case{
		Name:        "behavior-expand-on-demand",
		Layer:       evals.LayerBehavior,
		Description: "agent should expand only when key answer is outside preview",
		Prompt:      "inspect the report artifact and give me the final answer",
		Expectation: evals.Expectation{
			ShouldReadArtifact:  true,
			ExpectedFinalAnswer: "venus",
		},
	}, h)
	if !out.Passed {
		t.Fatalf("eval failed: %s\ntranscript=%v", out.Failure, out.Transcript)
	}
	if !out.Metrics.ExpandedOnDemand {
		t.Fatalf("expected ExpandedOnDemand=true, metrics=%+v", out.Metrics)
	}
}

func TestAgentBehavior_TranscriptShowsExpansionPath(t *testing.T) {
	fs := testkit.NewFakeFS(map[string]string{
		"/tmp/analysis.txt": "header\nheader\nFINAL=ok",
	})
	llm := testkit.NewScriptedClient(
		testkit.ToolUseTurn("read-1", "Read", `{"file_path":"/tmp/analysis.txt"}`),
		testkit.TextTurn("ok"),
	)
	h := testkit.NewHarness(t, llm, testkit.WithExtraTools(testkit.NewFakeRead(fs)))
	out := evals.NewRunner(t).RunWithHarness(evals.Case{
		Name:        "behavior-transcript-expansion-path",
		Layer:       evals.LayerBehavior,
		Description: "report should retain the expansion path for debugging",
		Prompt:      "read analysis and answer",
		Expectation: evals.Expectation{
			ShouldReadArtifact:  true,
			ExpectedFinalAnswer: "ok",
		},
	}, h)
	if !out.Passed {
		t.Fatalf("eval failed: %s\ntranscript=%v", out.Failure, out.Transcript)
	}
	joined := strings.Join(out.Transcript, "\n")
	if !strings.Contains(joined, "tool:Read") {
		t.Fatalf("transcript missing Read expansion path:\n%s", joined)
	}
}
