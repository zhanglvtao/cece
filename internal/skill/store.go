package skill

// Store holds discovered skills and provides lookup by name.
type Store struct {
	skills  []*Skill
	byName  map[string]*Skill
	enabled map[string]bool // nil = all enabled; non-nil = whitelist
}

// NewStore creates a Store from a deduplicated skill list.
func NewStore(skills []*Skill) *Store {
	byName := make(map[string]*Skill, len(skills))
	for _, s := range skills {
		byName[s.Name] = s
	}
	return &Store{skills: skills, byName: byName}
}

// Get returns a skill by name (regardless of enabled state).
func (s *Store) Get(name string) (*Skill, bool) {
	skill, ok := s.byName[name]
	return skill, ok
}

// All returns all skills (including disabled ones).
func (s *Store) All() []*Skill {
	return s.skills
}

// Enabled returns only enabled skills. If no enabled filter is set, all are returned.
func (s *Store) Enabled() []*Skill {
	if s.enabled == nil {
		return s.skills
	}
	var out []*Skill
	for _, sk := range s.skills {
		if s.enabled[sk.Name] {
			out = append(out, sk)
		}
	}
	return out
}

// SetEnabled sets the enabled whitelist. Empty or nil means all enabled.
func (s *Store) SetEnabled(names []string) {
	if len(names) == 0 {
		s.enabled = nil
		return
	}
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	s.enabled = m
}

// IsEnabled reports whether a skill is enabled.
func (s *Store) IsEnabled(name string) bool {
	if s.enabled == nil {
		return true
	}
	return s.enabled[name]
}

// EnabledNames returns the list of currently enabled skill names.
// If all are enabled (no filter), returns nil.
func (s *Store) EnabledNames() []string {
	if s.enabled == nil {
		return nil
	}
	var names []string
	for _, sk := range s.skills {
		if s.enabled[sk.Name] {
			names = append(names, sk.Name)
		}
	}
	return names
}

// Listing returns the formatted skill listing for system prompt injection.
// Only enabled skills are included.
func (s *Store) Listing() string {
	return FormatListing(s.Enabled())
}
