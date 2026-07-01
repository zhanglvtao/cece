package runtime

import "fmt"

type ProfileName string

const (
	ProfileInteractive ProfileName = "interactive"
	ProfileResearch    ProfileName = "research"
	ProfileCoding      ProfileName = "coding"
	ProfileReview      ProfileName = "review"
	ProfileExecution   ProfileName = "execution"
)

type PromptPolicy struct {
	UseSubAgentPrompt bool
}

type ToolPolicy struct {
	AllowAgentTool bool
}

type InteractionPolicy struct {
	UserFacing      bool
	PendingToParent bool
}

type ResultPolicy struct {
	ArtifactFirst   bool
	PreviewMaxChars int
}

type ExecutionPolicy struct {
	DefaultEffort   string
	DefaultMaxTurns int
}

type SpawnPolicy struct {
	AllowChildAgents bool
	AllowedProfiles  []ProfileName
}

type AgentProfile struct {
	Name        ProfileName
	Prompt      PromptPolicy
	Tools       ToolPolicy
	Interaction InteractionPolicy
	Result      ResultPolicy
	Execution   ExecutionPolicy
	Spawn       SpawnPolicy
}

func defaultProfiles() map[ProfileName]AgentProfile {
	return map[ProfileName]AgentProfile{
		ProfileInteractive: {
			Name: ProfileInteractive,
			Prompt: PromptPolicy{
				UseSubAgentPrompt: false,
			},
			Tools: ToolPolicy{
				AllowAgentTool: true,
			},
			Interaction: InteractionPolicy{
				UserFacing:      true,
				PendingToParent: false,
			},
			Result: ResultPolicy{
				ArtifactFirst:   false,
				PreviewMaxChars: 16000,
			},
			Execution: ExecutionPolicy{
				DefaultEffort:   "",
				DefaultMaxTurns: 0,
			},
			Spawn: SpawnPolicy{
				AllowChildAgents: true,
				AllowedProfiles:  []ProfileName{ProfileResearch, ProfileCoding, ProfileReview, ProfileExecution},
			},
		},
		ProfileResearch: taskProfile(ProfileResearch, "high"),
		ProfileCoding:   taskProfile(ProfileCoding, "medium"),
		ProfileReview:   taskProfile(ProfileReview, "high"),
		ProfileExecution: taskProfile(ProfileExecution, "medium"),
	}
}

func taskProfile(name ProfileName, effort string) AgentProfile {
	return AgentProfile{
		Name: name,
		Prompt: PromptPolicy{
			UseSubAgentPrompt: true,
		},
		Tools: ToolPolicy{
			AllowAgentTool: false,
		},
		Interaction: InteractionPolicy{
			UserFacing:      false,
			PendingToParent: true,
		},
		Result: ResultPolicy{
			ArtifactFirst:   true,
			PreviewMaxChars: 16000,
		},
		Execution: ExecutionPolicy{
			DefaultEffort:   effort,
			DefaultMaxTurns: 200,
		},
		Spawn: SpawnPolicy{
			AllowChildAgents: false,
		},
	}
}

func ProfileByName(name ProfileName) (AgentProfile, error) {
	profile, ok := defaultProfiles()[name]
	if !ok {
		return AgentProfile{}, fmt.Errorf("unknown agent profile: %s", name)
	}
	return profile, nil
}

func MustProfile(name ProfileName) AgentProfile {
	profile, err := ProfileByName(name)
	if err != nil {
		panic(err)
	}
	return profile
}
