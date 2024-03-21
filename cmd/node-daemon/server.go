package main

import (
	"context"
	"fmt"
	"net"
	"time"

	log "github.com/sirupsen/logrus"

	pb "woehrl01/kubelet-throttler/proto"

	"google.golang.org/grpc/health"
	healthgrpc "google.golang.org/grpc/health/grpc_health_v1"

	"google.golang.org/grpc"
)

type podLimitService struct {
	pb.UnimplementedPodLimiterServer
	throttler Throttler
}

var _ pb.PodLimiterServer = &podLimitService{}

func NewPodLimitersServer(throttler Throttler) *podLimitService {
	return &podLimitService{
		throttler: throttler,
	}
}

func (s *podLimitService) Wait(ctx context.Context, in *pb.WaitRequest) (*pb.WaitResponse, error) {
	log.Debugf("Received: %v", in.GetSlotName())
	startTime := time.Now()
	maxWait := in.GetMaxWaitSeconds()
	waitCtx, cancel := context.WithTimeout(ctx, time.Duration(maxWait)*time.Second)
	defer cancel()
	if err := s.throttler.AquireSlot(waitCtx, in.GetSlotName()); err != nil {
		log.Infof("Failed to acquire lock: %v", err)
		return &pb.WaitResponse{Success: false, Message: "Failed to acquire lock in time"}, nil
	}
	duration := time.Since(startTime)
	log.WithFields(log.Fields{
		"duration": duration,
		"slot":     in.GetSlotName(),
	}).Info("Acquired slot")
	return &pb.WaitResponse{Success: true, Message: "Waited successfully"}, nil
}

func startGrpcServer(throttler Throttler, port int) {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	healthcheck := health.NewServer()
	healthgrpc.RegisterHealthServer(s, healthcheck)

	service := NewPodLimitersServer(throttler)

	pb.RegisterPodLimiterServer(s, service)

	log.Printf("server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
