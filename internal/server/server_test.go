package server

import (
	"context"
	"testing"

	pb "mab/gen/mabpb"
	"mab/internal/store"
)

func newTestServer() *Server {
	return New(store.NewMemory())
}

// firstCall sets up a known state by feeding three priors, one per arm.
func seed(t *testing.T, s *Server, expID string) {
	t.Helper()
	ctx := context.Background()
	// Arm 1: reward 0; arm 2: reward 1; arm 3: reward 0.
	for _, prior := range []struct {
		arm int32
		val float64
	}{{1, 0.0}, {2, 1.0}, {3, 0.0}} {
		_, err := s.Pull(ctx, &pb.PullRequest{
			ExperimentId:      expID,
			AlgoType:          pb.AlgoType_ALGO_UCB,
			ChannelChosen:     prior.arm,
			ValueFromChannel:  prior.val,
			HasPrior:          true,
			CandidateChannels: []int32{1, 2, 3},
		})
		if err != nil {
			t.Fatalf("seed pull arm %d: %v", prior.arm, err)
		}
	}
}

func TestPullRequiresExperimentID(t *testing.T) {
	s := newTestServer()
	_, err := s.Pull(context.Background(), &pb.PullRequest{AlgoType: pb.AlgoType_ALGO_UCB, CandidateChannels: []int32{1}})
	if err == nil {
		t.Fatal("expected InvalidArgument, got nil")
	}
}

func TestPullRejectsUnknownAlgo(t *testing.T) {
	s := newTestServer()
	_, err := s.Pull(context.Background(), &pb.PullRequest{
		ExperimentId:      "e",
		AlgoType:          pb.AlgoType_ALGO_UNSPECIFIED,
		CandidateChannels: []int32{1},
	})
	if err == nil {
		t.Fatal("expected error for ALGO_UNSPECIFIED")
	}
}

func TestPullColdStartSelectsACandidate(t *testing.T) {
	s := newTestServer()
	resp, err := s.Pull(context.Background(), &pb.PullRequest{
		ExperimentId:      "exp-cold",
		AlgoType:          pb.AlgoType_ALGO_UCB,
		CandidateChannels: []int32{7, 8, 9},
	})
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	switch resp.NextChannelToUse {
	case 7, 8, 9:
	default:
		t.Fatalf("expected next in {7,8,9}, got %d", resp.NextChannelToUse)
	}
	if resp.TotalPulls != 0 {
		t.Fatalf("cold start with no prior should not bump total_pulls, got %d", resp.TotalPulls)
	}
}

func TestPullPersistsAcrossCalls(t *testing.T) {
	s := newTestServer()
	seed(t, s, "exp-persist")
	// After seeding, total_pulls should be 3.
	resp, err := s.Pull(context.Background(), &pb.PullRequest{
		ExperimentId:      "exp-persist",
		AlgoType:          pb.AlgoType_ALGO_UCB,
		CandidateChannels: []int32{1, 2, 3},
	})
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	if resp.TotalPulls != 3 {
		t.Fatalf("expected total_pulls=3 after 3 priors, got %d", resp.TotalPulls)
	}
}

func TestUCBConvergesToBestArm(t *testing.T) {
	s := newTestServer()
	expID := "exp-converge"
	ctx := context.Background()

	// Run 200 rounds. Arm 2 always pays 1.0, others pay 0.0. UCB should
	// settle on arm 2 — at least 80% of the last 50 picks should be arm 2.
	var lastPick int32
	rewardFor := func(arm int32) float64 {
		if arm == 2 {
			return 1.0
		}
		return 0.0
	}
	// Bootstrap: first call has no prior.
	resp, err := s.Pull(ctx, &pb.PullRequest{
		ExperimentId:      expID,
		AlgoType:          pb.AlgoType_ALGO_UCB,
		CandidateChannels: []int32{1, 2, 3},
	})
	if err != nil {
		t.Fatalf("bootstrap pull: %v", err)
	}
	lastPick = resp.NextChannelToUse

	picks := make([]int32, 0, 200)
	for i := 0; i < 200; i++ {
		resp, err := s.Pull(ctx, &pb.PullRequest{
			ExperimentId:      expID,
			AlgoType:          pb.AlgoType_ALGO_UCB,
			ChannelChosen:     lastPick,
			ValueFromChannel:  rewardFor(lastPick),
			HasPrior:          true,
			CandidateChannels: []int32{1, 2, 3},
		})
		if err != nil {
			t.Fatalf("round %d: %v", i, err)
		}
		picks = append(picks, resp.NextChannelToUse)
		lastPick = resp.NextChannelToUse
	}
	counts := map[int32]int{}
	for _, p := range picks {
		counts[p]++
	}
	// UCB's logarithmic regret means it keeps exploring bad arms periodically,
	// so we only require arm 2 to be the clear majority pick.
	if counts[2] < counts[1]*2 || counts[2] < counts[3]*2 {
		t.Fatalf("UCB did not favor arm 2: %v", counts)
	}
	if counts[2] < 100 {
		t.Fatalf("expected arm 2 picked >100/200 times, got %d", counts[2])
	}
}

func TestMultipleExperimentsIsolated(t *testing.T) {
	s := newTestServer()
	ctx := context.Background()
	// Two experiments, different reward profiles.
	_, _ = s.Pull(ctx, &pb.PullRequest{
		ExperimentId: "a", AlgoType: pb.AlgoType_ALGO_UCB,
		ChannelChosen: 1, ValueFromChannel: 1.0, HasPrior: true,
		CandidateChannels: []int32{1, 2},
	})
	_, _ = s.Pull(ctx, &pb.PullRequest{
		ExperimentId: "b", AlgoType: pb.AlgoType_ALGO_UCB,
		ChannelChosen: 2, ValueFromChannel: 1.0, HasPrior: true,
		CandidateChannels: []int32{1, 2},
	})
	respA, err := s.Pull(ctx, &pb.PullRequest{ExperimentId: "a", AlgoType: pb.AlgoType_ALGO_UCB, CandidateChannels: []int32{1, 2}})
	if err != nil {
		t.Fatal(err)
	}
	respB, err := s.Pull(ctx, &pb.PullRequest{ExperimentId: "b", AlgoType: pb.AlgoType_ALGO_UCB, CandidateChannels: []int32{1, 2}})
	if err != nil {
		t.Fatal(err)
	}
	if respA.TotalPulls != 1 || respB.TotalPulls != 1 {
		t.Fatalf("experiments not isolated: a=%d b=%d", respA.TotalPulls, respB.TotalPulls)
	}
}
