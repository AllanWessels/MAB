package algo

import (
	"math/rand"
	"sort"
)

type EpsilonGreedy struct {
	Rng *rand.Rand
}

func (EpsilonGreedy) Name() string { return "epsilon_greedy" }

func (e EpsilonGreedy) Select(s *State, candidates []int32, p Params) (int32, error) {
	arms := armUniverse(s, candidates)
	if len(arms) == 0 {
		return 0, ErrNoArms
	}
	eps := p.Epsilon
	if eps == 0 {
		eps = 0.1
	}
	r := e.Rng
	if r == nil {
		r = rand.New(rand.NewSource(rand.Int63()))
	}
	sort.Slice(arms, func(i, j int) bool { return arms[i] < arms[j] })

	if r.Float64() < eps {
		return arms[r.Intn(len(arms))], nil
	}
	return greedyPick(s, arms), nil
}

// greedyPick returns the arm with the highest empirical mean. Unseen arms are
// treated as having mean 0 — callers wanting forced exploration should rely on
// epsilon. Ties go to the lowest arm ID.
func greedyPick(s *State, arms []int32) int32 {
	var best int32 = arms[0]
	bestMean := s.Mean(best)
	for _, arm := range arms[1:] {
		m := s.Mean(arm)
		if m > bestMean {
			bestMean = m
			best = arm
		}
	}
	return best
}
