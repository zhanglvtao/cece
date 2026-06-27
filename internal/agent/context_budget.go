package agent

import "sort"

const (
	defaultReserveMinTokens = 20000
	defaultReserveRatio     = 0.1
	defaultUnderestimateN   = 32
)

type ReserveBudgetInput struct {
	EstimatedInputTokens int
	RequestedMaxTokens   int
	ModelMaxOutput       int
	ContextWindow        int
	ReserveRatio         float64
	UnderestimateP95     int
}

type ReserveBudget struct {
	EstimatedInputTokens int
	RequestedMaxTokens   int
	ReserveTokens        int
	UnderestimateP95     int
	AvailableMaxTokens   int
	Fits                 bool
}

func ComputeReserveBudget(in ReserveBudgetInput) ReserveBudget {
	ratio := in.ReserveRatio
	if ratio <= 0 {
		ratio = defaultReserveRatio
	}
	reserve := maxInt(
		defaultReserveMinTokens,
		in.ModelMaxOutput,
		int(float64(in.ContextWindow)*ratio),
		in.UnderestimateP95,
	)
	available := in.ContextWindow - in.EstimatedInputTokens - reserve
	if available < 0 {
		available = 0
	}
	return ReserveBudget{
		EstimatedInputTokens: in.EstimatedInputTokens,
		RequestedMaxTokens:   in.RequestedMaxTokens,
		ReserveTokens:        reserve,
		UnderestimateP95:     in.UnderestimateP95,
		AvailableMaxTokens:   available,
		Fits:                 in.EstimatedInputTokens+reserve <= in.ContextWindow,
	}
}

type UnderestimateStats struct {
	limit   int
	samples []int
}

func NewUnderestimateStats(limit int) *UnderestimateStats {
	if limit <= 0 {
		limit = defaultUnderestimateN
	}
	return &UnderestimateStats{limit: limit}
}

func (s *UnderestimateStats) Record(sample int) {
	if s == nil || sample < 0 {
		return
	}
	if len(s.samples) == s.limit {
		copy(s.samples, s.samples[1:])
		s.samples = s.samples[:s.limit-1]
	}
	s.samples = append(s.samples, sample)
}

func (s *UnderestimateStats) P95() int {
	if s == nil || len(s.samples) == 0 {
		return 0
	}
	cp := append([]int(nil), s.samples...)
	sort.Ints(cp)
	idx := (len(cp)*95 - 1) / 100
	if idx < 0 {
		idx = 0
	}
	if idx >= len(cp) {
		idx = len(cp) - 1
	}
	return cp[idx]
}

func maxInt(values ...int) int {
	best := 0
	for _, v := range values {
		if v > best {
			best = v
		}
	}
	return best
}
