package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"

	pb "woehrl01/pod-pacemaker/proto"
)

// errDaemonUnreachable means no connection to the daemon could be established, as opposed to a
// reachable daemon that grants no slot in time
var errDaemonUnreachable = errors.New("daemon unreachable")

func WaitForSlot(ctx context.Context, slotName string, config *PluginConf) error {
	conn, err := connect(ctx, config.DaemonSocketPath)
	if err != nil {
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

func isConnectionError(err error) bool {
	if errors.Is(err, errDaemonUnreachable) {
		return true
	}
	// the daemon went away during the Wait call
	errorMsg := err.Error()
	return strings.Contains(errorMsg, "connection refused") ||
		strings.Contains(errorMsg, "error reading from server") ||
		strings.Contains(errorMsg, "connection reset by peer") ||
		strings.Contains(errorMsg, "transport is closing") ||
		strings.Contains(errorMsg, "connection closed") ||
		strings.Contains(errorMsg, "Error while dialing")
}

// connect returns a connection to the daemon once it is ready. It fails with errDaemonUnreachable as
// soon as the attempt fails or ctx ends while connecting, so a deadline hit while connecting is not
// mistaken for a reachable daemon without a free slot. Each call uses a new client, so a daemon that
// comes back is found without the growing reconnect backoff of a long-lived one.
func connect(ctx context.Context, socketPath string) (*grpc.ClientConn, error) {
	conn, err := grpc.NewClient(fmt.Sprintf("unix://%s", socketPath),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}
	conn.Connect()
	for {
		state := conn.GetState()
		switch state {
		case connectivity.Ready:
			return conn, nil
		case connectivity.TransientFailure, connectivity.Shutdown:
			conn.Close()
			return nil, fmt.Errorf("%w: connection %s", errDaemonUnreachable, state)
		}
		if !conn.WaitForStateChange(ctx, state) {
			conn.Close()
			return nil, fmt.Errorf("%w: %v", errDaemonUnreachable, ctx.Err())
		}
	}
}
