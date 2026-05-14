package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	pb "mab/gen/mabpb"
	"mab/internal/server"
	"mab/internal/store"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	addr := getEnv("GRPC_ADDR", ":50051")
	table := getEnv("DDB_TABLE", "bandit_state")
	region := getEnv("AWS_REGION", "us-east-1")
	endpoint := os.Getenv("DDB_ENDPOINT") // empty in real AWS, set for dynamodb-local

	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("local", "local", "")),
	)
	if err != nil {
		log.Fatalf("aws config: %v", err)
	}
	ddb := dynamodb.NewFromConfig(cfg, func(o *dynamodb.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
	})
	st := store.NewDynamo(ddb, table)
	if err := st.EnsureTable(ctx); err != nil {
		log.Fatalf("ensure table %q: %v", table, err)
	}

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen %s: %v", addr, err)
	}
	gs := grpc.NewServer()
	pb.RegisterBanditServiceServer(gs, server.New(st))
	healthpb.RegisterHealthServer(gs, health.NewServer())
	reflection.Register(gs)

	go func() {
		<-ctx.Done()
		log.Println("shutting down")
		gs.GracefulStop()
	}()

	log.Printf("bandit gRPC listening on %s (table=%s endpoint=%q)", addr, table, endpoint)
	if err := gs.Serve(lis); err != nil {
		log.Fatalf("serve: %v", err)
	}
}

func getEnv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
