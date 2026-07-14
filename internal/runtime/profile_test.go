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
		t.Fatal("interactive profile should allow spawning task agents")
	}
	if interactive.Execution.DefaultMaxTurns != 0 {
		t.Fatalf("interactive DefaultMaxTurns = %d, want 0", interactive.Execution.DefaultMaxTurns)
	}
	wantAllowed := []ProfileName{ProfileExplore, ProfileCoding, ProfileReview, ProfileExecution}
	if len(interactive.Spawn.AllowedProfiles) != len(wantAllowed) {
		t.Fatalf("interactive AllowedProfiles = %#v", interactive.Spawn.AllowedProfiles)
	}
	for i, want := range wantAllowed {
		if interactive.Spawn.AllowedProfiles[i] != want {
			t.Fatalf("interactive AllowedProfiles[%d] = %q, want %q", i, interactive.Spawn.AllowedProfiles[i], want)
		}
	}

	cases := []struct {
		name   ProfileName
		effort string
	}{
		{ProfileExplore, "high"},
		{ProfileCoding, "medium"},
		{ProfileReview, "high"},
		{ProfileExecution, "medium"},
	}
	for _, tc := range cases {
		profile, err := ProfileByName(tc.name)
		if err != nil {
			t.Fatalf("ProfileByName(%s) error = %v", tc.name, err)
		}
		if profile.Name != tc.name {
			t.Fatalf("profile.Name = %q, want %q", profile.Name, tc.name)
		}
		if profile.Tools.AllowAgentTool {
			t.Fatalf("%s profile must not allow Agent tool", tc.name)
		}
		if profile.Interaction.UserFacing {
			t.Fatalf("%s profile must not be user-facing", tc.name)
		}
		if !profile.Interaction.PendingToParent {
			t.Fatalf("%s profile should route question/confirm/plan to parent", tc.name)
		}
		if !profile.Result.ArtifactFirst {
			t.Fatalf("%s profile should prefer artifact-first results", tc.name)
		}
		if profile.Execution.DefaultEffort != tc.effort {
			t.Fatalf("%s DefaultEffort = %q, want %q", tc.name, profile.Execution.DefaultEffort, tc.effort)
		}
		if profile.Execution.DefaultMaxTurns != 200 {
			t.Fatalf("%s DefaultMaxTurns = %d, want 200", tc.name, profile.Execution.DefaultMaxTurns)
		}
		if profile.Spawn.AllowChildAgents {
			t.Fatalf("%s profile must not allow spawning child agents", tc.name)
		}
	}
}

func TestProfileByName_UnknownProfile(t *testing.T) {
	if _, err := ProfileByName(ProfileName("reviewer")); err == nil {
		t.Fatal("expected unknown profile error")
	}
}

func TestProfileForAgentTypeRejectsResearch(t *testing.T) {
	if _, err := profileForAgentType("research"); err == nil {
		t.Fatal("expected unknown agent_type error for research")
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
