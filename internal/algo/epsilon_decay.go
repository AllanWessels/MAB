package algo

import (
	"math"
	"math/rand"
	"sort"
)

type EpsilonDecay struct {
	Rng *rand.Rand
}

func (EpsilonDecay) Name() string { return "epsilon_decay" }

// Select uses epsilon_t = min(1, decay_rate / (total_pulls + 1)). The +1 keeps
// the first call from dividing by zero and from being a guaranteed explore.
func (e EpsilonDecay) Select(s *State, candidates []int32, p Params) (int32, error) {
	arms := armUniverse(s, candidates)
	if len(arms) == 0 {
		return 0, ErrNoArms
	}
	k := p.DecayRate
	if k == 0 {
		k = 1.0
	}
	r := e.Rng
	if r == nil {
		r = rand.New(rand.NewSource(rand.Int63()))
	}
	sort.Slice(arms, func(i, j int) bool { return arms[i] < arms[j] })

	eps := math.Min(1.0, k/float64(s.TotalPulls+1))
	if r.Float64() < eps {
		return arms[r.Intn(len(arms))], nil
	}
	return greedyPick(s, arms), nil
}
