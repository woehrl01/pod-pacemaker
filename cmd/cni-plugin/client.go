package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"google.golang.org/grpc"

	pb "woehrl01/kubelet-throttler/proto"
)

func Wait(containerID string, maxWaitSeconds int32) error {
	conn, err := grpc.Dial("localhost:50051", grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()
	c := pb.NewPodLimiterClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r, err := c.Wait(ctx, &pb.WaitRequest{ContainerId: containerID, MaxWaitSeconds: maxWaitSeconds})
	if err != nil {
		return err
	}

	if !r.Success {
		return fmt.Errorf("failed to acquire semaphore")
	}

	return nil
}
