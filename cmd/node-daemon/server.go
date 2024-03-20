package main

import (
	"context"
	"log"
	"net"
	"time"

	pb "woehrl01/kubelet-throttler/proto"

	"google.golang.org/grpc"
)

type podLimitService struct {
	pb.UnimplementedPodLimiterServer
	throttler Throttler
}

func NewPodLimitersServer(throttler Throttler) *podLimitService {
	return &podLimitService{
		throttler: throttler,
	}
}

func (s *podLimitService) Wait(ctx context.Context, in *pb.WaitRequest) (*pb.WaitResponse, error) {
	log.Printf("Received: %v", in.GetContainerId())
	maxWait := in.GetMaxWaitSeconds()
	waitCtx, cancel := context.WithTimeout(ctx, time.Duration(maxWait)*time.Second)
	defer cancel()
	if err := s.throttler.AquireSlot(waitCtx, in.GetContainerId()); err != nil {
		return &pb.WaitResponse{Success: false, Message: "Failed to acquire lock in time"}, nil
	}
	return &pb.WaitResponse{Success: true, Message: "Waited successfully"}, nil
}

func startGrpcServer(throttler Throttler) {
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()

	service := NewPodLimitersServer(throttler)

	pb.RegisterPodLimiterServer(s, service)
	log.Printf("server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
