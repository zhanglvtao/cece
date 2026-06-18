package runtime

import "testing"

func TestProfileByName_Defaults(t *testing.T) {
	interactive, err := ProfileByName(ProfileInteractive)
	if err != nil {
		t.Fatalf("ProfileByName(interactive) error = %v", err)
	}
	if interactive.Name != ProfileInteractive {
		t.Fatalf("interactive.Name = %q", interactive.Name)
	}
	if !interactive.Tools.AllowAgentTool {
		t.Fatal("interactive profile should allow Agent tool")
	}
	if !interactive.Interaction.UserFacing {
		t.Fatal("interactive profile should be user-facing")
	}
	if !interactive.Spawn.AllowChildAgents {
		t.Fatal("interactive profile should allow spawning worker agents")
	}
	if interactive.Execution.DefaultMaxTurns != 0 {
		t.Fatalf("interactive DefaultMaxTurns = %d, want 0", interactive.Execution.DefaultMaxTurns)
	}

	worker, err := ProfileByName(ProfileWorker)
	if err != nil {
		t.Fatalf("ProfileByName(worker) error = %v", err)
	}
	if worker.Name != ProfileWorker {
		t.Fatalf("worker.Name = %q", worker.Name)
	}
	if worker.Tools.AllowAgentTool {
		t.Fatal("worker profile must not allow Agent tool")
	}
	if worker.Interaction.UserFacing {
		t.Fatal("worker profile must not be user-facing")
	}
	if !worker.Interaction.PendingToParent {
		t.Fatal("worker profile should route question/confirm/plan to parent")
	}
	if !worker.Result.ArtifactFirst {
		t.Fatal("worker profile should prefer artifact-first results")
	}
	if worker.Execution.DefaultEffort != "low" {
		t.Fatalf("worker DefaultEffort = %q, want low", worker.Execution.DefaultEffort)
	}
	if worker.Execution.DefaultMaxTurns != 8 {
		t.Fatalf("worker DefaultMaxTurns = %d, want 8", worker.Execution.DefaultMaxTurns)
	}
	if worker.Spawn.AllowChildAgents {
		t.Fatal("worker profile must not allow spawning child agents in v1")
	}
}

func TestProfileByName_UnknownProfile(t *testing.T) {
	if _, err := ProfileByName(ProfileName("reviewer")); err == nil {
		t.Fatal("expected unknown profile error")
	}
}

func TestMustProfile_PanicsOnUnknownProfile(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("MustProfile should panic for unknown profile")
		}
	}()
	_ = MustProfile(ProfileName("unknown"))
}
