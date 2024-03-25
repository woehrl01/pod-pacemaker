package main

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"k8s.io/apimachinery/pkg/util/wait"

	pb "woehrl01/pod-pacemaker/proto"
)

func WaitForSlot(slotName string, config *PluginConf) error {
	ctx, totalRequestCancel := context.WithTimeout(context.Background(), time.Second*time.Duration(config.MaxWaitTimeInSeconds))
	defer totalRequestCancel()

	conn, err := WaitUntilConnected(ctx, config.DaemonPort)
	if err != nil {
		if config.SuccessOnConnectionTimeout {
			logrus.Warnf("Failed to connect to daemon, but successOnConnectionTimeout is set, so returning success")
			return nil
		}
		return err
	}
	defer conn.Close()

	c := pb.NewPodLimiterClient(conn)

	r, err := c.Wait(ctx, &pb.WaitRequest{SlotName: slotName})
	if err != nil {
		return err
	}

	if !r.Success {
		return fmt.Errorf("failed to acquire slot: %s", r.Message)
	}

	return nil
}

func WaitUntilConnected(ctx context.Context, port int32) (*grpc.ClientConn, error) {
	server := fmt.Sprintf("localhost:%d", port)
	var conn *grpc.ClientConn
	err := wait.PollUntilContextCancel(ctx, time.Second, true, func(ctx context.Context) (bool, error) {
		c, err := grpc.DialContext(ctx, server,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err != nil {
			logrus.Warnf("Failed to connect to daemon: %v", err)
			return false, nil
		}
		conn = c
		return true, nil
	})
	return conn, err
}
