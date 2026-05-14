package server

import (
	"context"
	"fmt"
	"math/rand"
	"sync"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"mab/internal/algo"
	"mab/internal/store"

	pb "mab/gen/mabpb"
)

type Server struct {
	pb.UnimplementedBanditServiceServer

	store store.Store

	mu    sync.Mutex
	locks map[string]*sync.Mutex // per-experiment serialization
	rng   *rand.Rand
}

func New(s store.Store) *Server {
	return &Server{
		store: s,
		locks: map[string]*sync.Mutex{},
		rng:   rand.New(rand.NewSource(rand.Int63())),
	}
}

func (s *Server) lockFor(id string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.locks[id]
	if !ok {
		m = &sync.Mutex{}
		s.locks[id] = m
	}
	return m
}

func (s *Server) Pull(ctx context.Context, req *pb.PullRequest) (*pb.PullResponse, error) {
	if req.GetExperimentId() == "" {
		return nil, status.Error(codes.InvalidArgument, "experiment_id is required")
	}
	a, err := pickAlgo(req.GetAlgoType(), s.rng)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	lk := s.lockFor(req.GetExperimentId())
	lk.Lock()
	defer lk.Unlock()

	state, err := s.store.Load(ctx, req.GetExperimentId())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "load state: %v", err)
	}

	if req.GetHasPrior() {
		state.Update(req.GetChannelChosen(), req.GetValueFromChannel())
	}

	params := paramsFromProto(req.GetParameters())
	next, err := a.Select(state, req.GetCandidateChannels(), params)
	if err != nil {
		// No candidates and no seen arms: if the caller supplied a
		// channel_chosen via has_prior we already learned about it above and
		// would have returned it; otherwise we genuinely don't know any arms.
		return nil, status.Errorf(codes.FailedPrecondition, "select: %v", err)
	}

	if err := s.store.Save(ctx, req.GetExperimentId(), state); err != nil {
		return nil, status.Errorf(codes.Internal, "save state: %v", err)
	}
	return &pb.PullResponse{
		NextChannelToUse: next,
		TotalPulls:       state.TotalPulls,
		ArmPulls:         state.Counts[next],
		ArmMean:          state.Mean(next),
	}, nil
}

func pickAlgo(t pb.AlgoType, rng *rand.Rand) (algo.Algo, error) {
	switch t {
	case pb.AlgoType_ALGO_UCB:
		return algo.UCB{}, nil
	case pb.AlgoType_ALGO_EPSILON_GREEDY:
		return algo.EpsilonGreedy{Rng: rng}, nil
	case pb.AlgoType_ALGO_EPSILON_DECAY:
		return algo.EpsilonDecay{Rng: rng}, nil
	default:
		return nil, fmt.Errorf("unknown algo_type %s", t)
	}
}

func paramsFromProto(p *pb.AlgoParams) algo.Params {
	if p == nil {
		return algo.Params{}
	}
	return algo.Params{
		UCBc:      p.GetUcbC(),
		Epsilon:   p.GetEpsilon(),
		DecayRate: p.GetDecayRate(),
	}
}
