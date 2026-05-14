package algo

import "errors"

// State is the per-experiment posterior the algorithms read and update.
// Arm IDs are arbitrary int32 channel identifiers; the map grows lazily.
type State struct {
	TotalPulls int64
	Counts     map[int32]int64
	Sums       map[int32]float64
}

func NewState() *State {
	return &State{Counts: map[int32]int64{}, Sums: map[int32]float64{}}
}

func (s *State) Mean(arm int32) float64 {
	c := s.Counts[arm]
	if c == 0 {
		return 0
	}
	return s.Sums[arm] / float64(c)
}

// Update records a reward for the given arm.
func (s *State) Update(arm int32, reward float64) {
	s.Counts[arm]++
	s.Sums[arm] += reward
	s.TotalPulls++
}

// Params is the union of algorithm hyperparameters. Each algo reads only
// the fields it needs; zero values fall back to algo-specific defaults.
type Params struct {
	UCBc      float64
	Epsilon   float64
	DecayRate float64
}

// Algo selects the next arm given current state, candidate arm IDs, and params.
// Candidates may be empty — in that case the algo must pick from arms it has
// already seen. If neither candidates nor seen arms exist, ErrNoArms is returned.
type Algo interface {
	Select(s *State, candidates []int32, p Params) (int32, error)
	Name() string
}

var ErrNoArms = errors.New("no candidate or known arms to select from")

// armUniverse returns the set of arms the selector should consider. When
// candidates is non-empty it wins; otherwise we fall back to arms we've
// already observed.
func armUniverse(s *State, candidates []int32) []int32 {
	if len(candidates) > 0 {
		return candidates
	}
	out := make([]int32, 0, len(s.Counts))
	for arm := range s.Counts {
		out = append(out, arm)
	}
	return out
}
