package main

import (
	"context"
	"fmt"
	"net"
	"time"

	"woehrl01/pod-pacemaker/pkg/podaccessor"
	"woehrl01/pod-pacemaker/pkg/throttler"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	pb "woehrl01/pod-pacemaker/proto"

	"google.golang.org/grpc/health"
	healthgrpc "google.golang.org/grpc/health/grpc_health_v1"

	"google.golang.org/grpc"
)

var (
	waitTimeHistogram = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "pod_pacemaker_wait_duration_seconds",
		Help:    "Duration of wait requests",
		Buckets: prometheus.ExponentialBucketsRange(0.1, 60, 5),
	})
	podNotFoundCounter = promauto.NewCounter(prometheus.CounterOpts{
		Name: "pod_pacemaker_pod_not_found",
		Help: "Pod not found",
	})
	waitFailedCounter = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "pod_pacemaker_wait_failed",
		Help: "Wait failed",
	}, []string{"reason"})
)

type podLimitService struct {
	pb.UnimplementedPodLimiterServer
	throttler   throttler.Throttler
	podAccessor podaccessor.PodAccessor
	options     Options
}

type Options struct {
	Port           int
	SkipDaemonSets bool
}

var _ pb.PodLimiterServer = &podLimitService{}

func NewPodLimitersServer(throttler throttler.Throttler, podAccessor podaccessor.PodAccessor, o Options) *podLimitService {
	return &podLimitService{
		throttler:   throttler,
		podAccessor: podAccessor,
		options:     o,
	}
}

func (s *podLimitService) Wait(ctx context.Context, in *pb.WaitRequest) (*pb.WaitResponse, error) {
	log.Debugf("Received: %v", in.GetSlotName())
	startTime := time.Now()

	var pod *corev1.Pod
	wait.PollUntilContextCancel(ctx, 500*time.Millisecond, true, func(ctx context.Context) (bool, error) {
		p, err := s.podAccessor.GetPodByKey(in.GetSlotName())
		if err != nil {
			podNotFoundCounter.Inc()
			return false, nil
		}
		if p == nil {
			podNotFoundCounter.Inc()
			return false, nil
		}
		pod = p
		return true, nil
	})
	if pod == nil {
		log.Infof("Failed to get pod: %v", in.GetSlotName())
		waitFailedCounter.WithLabelValues("pod_not_found").Inc()
		return &pb.WaitResponse{Success: false, Message: "Failed to get pod"}, nil
	}

	data := throttler.Data{
		Pod: pod,
	}

	if s.options.SkipDaemonSets && pod.ObjectMeta.OwnerReferences != nil && len(pod.ObjectMeta.OwnerReferences) > 0 && pod.ObjectMeta.OwnerReferences[0].Kind == "DaemonSet" {
		log.Infof("Skipping daemonset: %v", pod.ObjectMeta.Name)
		return &pb.WaitResponse{Success: true, Message: "Skipped daemonset"}, nil
	}

	if err := s.throttler.AquireSlot(ctx, in.GetSlotName(), data); err != nil {
		log.Infof("Failed to acquire lock: %v", err)
		waitFailedCounter.WithLabelValues("failed_to_acquire_lock").Inc()
		return &pb.WaitResponse{Success: false, Message: "Failed to acquire lock in time"}, nil
	}
	duration := time.Since(startTime)
	log.WithFields(log.Fields{
		"duration": duration,
		"slot":     in.GetSlotName(),
	}).Info("Acquired slot")

	waitTimeHistogram.Observe(duration.Seconds())

	return &pb.WaitResponse{Success: true, Message: "Waited successfully"}, nil
}

func startGrpcServer(throttler throttler.Throttler, o Options, podAccessor podaccessor.PodAccessor) {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", o.Port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	healthcheck := health.NewServer()
	healthgrpc.RegisterHealthServer(s, healthcheck)

	service := NewPodLimitersServer(throttler, podAccessor, o)

	pb.RegisterPodLimiterServer(s, service)

	log.Printf("server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
