package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

var (
	errUnreachable = errors.New(`rpc error: code = Unavailable desc = connection error: desc = "transport: Error while dialing: dial unix /var/run/pod-pacemaker/pod-pacemaker.sock: connect: no such file or directory"`)
	errNoSlot      = errors.New("failed to acquire slot: Failed to acquire lock in time")
)

func init() {
	retryBackoff = func() time.Duration { return 10 * time.Millisecond }
}

// waitSequence answers the Wait calls with errs in order and keeps repeating the last one
func waitSequence(errs ...error) func(context.Context, string, *PluginConf) error {
	i := 0
	return func(ctx context.Context, _ string, _ *PluginConf) error {
		err := errs[min(i, len(errs)-1)]
		i++
		if errors.Is(err, context.DeadlineExceeded) {
			<-ctx.Done()
			return ctx.Err()
		}
		return err
	}
}

// the real client against a socket nobody listens on, as on a node where the daemon is not running
func TestAcquireSlotWithoutDaemon(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	conf := &PluginConf{DaemonSocketPath: t.TempDir() + "/pod-pacemaker.sock", SuccessOnConnectionTimeout: true}
	if err := acquireSlot(ctx, "ns/pod", conf, WaitForSlot); err != nil {
		t.Fatalf("acquireSlot() error = %v, want the pod to start without a slot", err)
	}
}

func TestAcquireSlot(t *testing.T) {
	tests := []struct {
		name             string
		successOnTimeout bool
		wait             func(context.Context, string, *PluginConf) error
		wantErr          bool
	}{
		{"granted", true, waitSequence(nil), false},
		{"daemon unreachable, success on timeout", true, waitSequence(errUnreachable), false},
		{"daemon unreachable, no success on timeout", false, waitSequence(errUnreachable), true},
		{"daemon comes back and grants", false, waitSequence(errUnreachable, errUnreachable, nil), false},
		{"daemon reachable but no slot before timeout", true, waitSequence(context.DeadlineExceeded), true},
		{"daemon comes back but no slot before timeout", true, waitSequence(errUnreachable, context.DeadlineExceeded), true},
		{"daemon refuses", true, waitSequence(errNoSlot), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			err := acquireSlot(ctx, "ns/pod", &PluginConf{SuccessOnConnectionTimeout: tt.successOnTimeout}, tt.wait)
			if (err != nil) != tt.wantErr {
				t.Fatalf("acquireSlot() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
