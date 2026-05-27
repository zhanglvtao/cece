package skill

// Store holds discovered skills and provides lookup by name.
type Store struct {
	skills []*Skill
	byName map[string]*Skill
}

// NewStore creates a Store from a deduplicated skill list.
func NewStore(skills []*Skill) *Store {
	byName := make(map[string]*Skill, len(skills))
	for _, s := range skills {
		byName[s.Name] = s
	}
	return &Store{skills: skills, byName: byName}
}

// Get returns a skill by name.
func (s *Store) Get(name string) (*Skill, bool) {
	skill, ok := s.byName[name]
	return skill, ok
}

// All returns all skills.
func (s *Store) All() []*Skill {
	return s.skills
}

// Listing returns the formatted skill listing for system prompt injection.
func (s *Store) Listing() string {
	return FormatListing(s.skills)
}
