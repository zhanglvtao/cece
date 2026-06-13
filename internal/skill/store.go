package skill

// Store holds discovered skills and provides lookup by name.
type Store struct {
	skills   []*Skill
	byName   map[string]*Skill
	enabled  map[string]bool // non-nil = whitelist; nil = none enabled (default)
	allOn    bool            // true = all enabled (for backward compat with empty enabled list in settings)
	OnChange func()          // called after SetEnabled/SetAllEnabled
}

func (s *Store) notify() {
	if s.OnChange != nil {
		s.OnChange()
	}
}

// NewStore creates a Store from a deduplicated skill list.
// By default, no skills are enabled.
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

// Enabled returns only enabled skills.
func (s *Store) Enabled() []*Skill {
	if s.allOn {
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

// SetEnabled sets the enabled whitelist.
// A nil or empty slice means "all enabled".
func (s *Store) SetEnabled(names []string) {
	if len(names) == 0 {
		s.allOn = true
		s.enabled = nil
		s.notify()
		return
	}
	s.allOn = false
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	s.enabled = m
	s.notify()
}

// SetAllEnabled enables or disables all skills at once.
func (s *Store) SetAllEnabled(on bool) {
	if on {
		s.allOn = true
		s.enabled = nil
	} else {
		s.allOn = false
		s.enabled = make(map[string]bool)
	}
	s.notify()
}

// IsEnabled reports whether a skill is enabled.
func (s *Store) IsEnabled(name string) bool {
	if s.allOn {
		return true
	}
	return s.enabled[name]
}

// AllEnabled reports whether all skills are enabled (no explicit whitelist).
func (s *Store) AllEnabled() bool {
	return s.allOn
}

// EnabledNames returns the list of currently enabled skill names.
func (s *Store) EnabledNames() []string {
	if s.allOn {
		var names []string
		for _, sk := range s.skills {
			names = append(names, sk.Name)
		}
		return names
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
