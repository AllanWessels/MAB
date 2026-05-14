package algo

import (
	"math"
	"sort"
)

type UCB struct{}

func (UCB) Name() string { return "ucb" }

// Select implements UCB1: argmax_i mean_i + c * sqrt(ln(N) / n_i). Arms that
// have never been pulled receive +inf, so each candidate is tried once before
// the bound kicks in. Ties are broken by the lowest arm ID for determinism.
func (UCB) Select(s *State, candidates []int32, p Params) (int32, error) {
	arms := armUniverse(s, candidates)
	if len(arms) == 0 {
		return 0, ErrNoArms
	}
	c := p.UCBc
	if c == 0 {
		c = math.Sqrt2
	}
	sort.Slice(arms, func(i, j int) bool { return arms[i] < arms[j] })

	logN := math.Log(math.Max(1, float64(s.TotalPulls)))
	var best int32
	bestScore := math.Inf(-1)
	for _, arm := range arms {
		n := s.Counts[arm]
		var score float64
		if n == 0 {
			score = math.Inf(1)
		} else {
			score = s.Mean(arm) + c*math.Sqrt(logN/float64(n))
		}
		if score > bestScore {
			bestScore = score
			best = arm
		}
	}
	return best, nil
}
