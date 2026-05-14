package algo

import (
	"math/rand"
	"testing"
)

func TestUCBExploresUnseenArms(t *testing.T) {
	s := NewState()
	s.Update(1, 1.0)
	s.Update(1, 1.0)
	// Arm 2 is unseen but a candidate — UCB should pick it (score = +inf).
	got, err := UCB{}.Select(s, []int32{1, 2}, Params{})
	if err != nil {
		t.Fatal(err)
	}
	if got != 2 {
		t.Fatalf("expected arm 2 (unseen), got %d", got)
	}
}

func TestUCBPrefersHigherMeanOnceAllPulled(t *testing.T) {
	s := NewState()
	for i := 0; i < 10; i++ {
		s.Update(1, 1.0)
		s.Update(2, 0.0)
	}
	got, err := UCB{}.Select(s, []int32{1, 2}, Params{UCBc: 0.1})
	if err != nil {
		t.Fatal(err)
	}
	if got != 1 {
		t.Fatalf("expected arm 1 (higher mean), got %d", got)
	}
}

func TestEpsilonGreedyExploitsAtZeroEps(t *testing.T) {
	s := NewState()
	s.Update(1, 0.0)
	s.Update(2, 1.0)
	eg := EpsilonGreedy{Rng: rand.New(rand.NewSource(42))}
	// eps will default to 0.1; run many trials and check arm 2 wins majority.
	wins := 0
	for i := 0; i < 1000; i++ {
		got, err := eg.Select(s, []int32{1, 2}, Params{Epsilon: 0.0001})
		if err != nil {
			t.Fatal(err)
		}
		if got == 2 {
			wins++
		}
	}
	if wins < 950 {
		t.Fatalf("expected >=950/1000 picks for arm 2, got %d", wins)
	}
}

func TestEpsilonDecayConvergesToGreedy(t *testing.T) {
	s := NewState()
	s.Update(1, 0.0)
	s.Update(2, 1.0)
	s.TotalPulls = 10000 // huge t → eps ~ 0
	ed := EpsilonDecay{Rng: rand.New(rand.NewSource(7))}
	got, err := ed.Select(s, []int32{1, 2}, Params{DecayRate: 1.0})
	if err != nil {
		t.Fatal(err)
	}
	if got != 2 {
		t.Fatalf("expected arm 2 once decayed, got %d", got)
	}
}

func TestErrNoArms(t *testing.T) {
	s := NewState()
	if _, err := (UCB{}).Select(s, nil, Params{}); err != ErrNoArms {
		t.Fatalf("expected ErrNoArms, got %v", err)
	}
}
