package main

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"

	pb "woehrl01/pod-pacemaker/proto"
)

// fakeDaemon grants the slot when grant is set and otherwise holds the request until it is cancelled
type fakeDaemon struct {
	pb.UnimplementedPodLimiterServer
	grant bool
}

func (d *fakeDaemon) Wait(ctx context.Context, _ *pb.WaitRequest) (*pb.WaitResponse, error) {
	if d.grant {
		return &pb.WaitResponse{Success: true}, nil
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func startDaemon(t *testing.T, daemon *fakeDaemon) string {
	t.Helper()
	socket := t.TempDir() + "/pod-pacemaker.sock"
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	pb.RegisterPodLimiterServer(server, daemon)
	go server.Serve(listener)
	t.Cleanup(server.Stop)
	return socket
}

func TestAcquireSlotWithDaemon(t *testing.T) {
	tests := []struct {
		name    string
		grant   bool
		wantErr bool
	}{
		{"daemon grants the slot", true, false},
		// must not be mistaken for an unreachable daemon, the pod would start without a slot
		{"daemon has no free slot until the deadline", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			conf := &PluginConf{DaemonSocketPath: startDaemon(t, &fakeDaemon{grant: tt.grant}), SuccessOnConnectionTimeout: true}
			err := acquireSlot(ctx, "ns/pod", conf, WaitForSlot)
			if (err != nil) != tt.wantErr {
				t.Fatalf("acquireSlot() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
