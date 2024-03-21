package main

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "woehrl01/kubelet-throttler/proto"
)

func WaitForSlot(slotName string, config *PluginConf) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	conn, err := WaitUntilConnected(ctx, config.DaemonPort)
	if err != nil {
		return err
	}
	defer conn.Close()

	c := pb.NewPodLimiterClient(conn)

	r, err := c.Wait(ctx, &pb.WaitRequest{SlotName: slotName, MaxWaitSeconds: config.MaxWaitTimeInSeconds})
	if err != nil {
		return err
	}

	if !r.Success {
		return fmt.Errorf("failed to acquire slot: %s", r.Message)
	}

	return nil
}

func WaitUntilConnected(ctx context.Context, port int32) (*grpc.ClientConn, error) {
	for {
		server := fmt.Sprintf("localhost:%d", port)
		logrus.Infof("Connecting to %s", server)
		conn, err := grpc.Dial(server, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
		if err != nil {
			logrus.Errorf("did not connect: %v", err)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
			time.Sleep(time.Second)
			continue
		}
		return conn, nil
	}
}
