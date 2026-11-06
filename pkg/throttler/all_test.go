package throttler

import (
	"context"
	"sync"
	"testing"
	"time"
)

// acquireAsync starts acquiring slotId and reports the result on the returned channel
func acquireAsync(ctx context.Context, th Throttler, slotId string) <-chan error {
	done := make(chan error, 1)
	go func() { done <- th.AquireSlot(ctx, slotId, Data{}) }()
	return done
}

func expectWaiting(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		t.Fatalf("slot acquired while the limit is reached (err = %v)", err)
	case <-time.After(100 * time.Millisecond):
	}
}

func expectGranted(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("AquireSlot() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("slot not granted")
	}
}

func TestConfigChangeKeepsGrantedSlots(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	dynamic := NewDynamicThrottler()
	dynamic.SetThrottlers([]Throttler{NewDynamicConcurrencyThrottler(1, "")})
	all := NewAllThrottler(dynamic)

	expectGranted(t, acquireAsync(ctx, all, "ns/a"))
	waiting := acquireAsync(ctx, all, "ns/b")
	expectWaiting(t, waiting)

	// a PacemakerConfig update builds new throttlers with the same limit
	dynamic.SetThrottlers([]Throttler{NewDynamicConcurrencyThrottler(1, "")})
	expectWaiting(t, waiting) // ns/a still counts
	if slots := all.ActiveSlots(); len(slots) != 1 || slots[0] != "ns/a" {
		t.Fatalf("ActiveSlots() = %v, want [ns/a]", slots)
	}

	// the release goes to the new throttlers and wakes the waiter
	all.ReleaseSlot(ctx, "ns/a")
	expectGranted(t, waiting)
}

func TestConfigChangeWakesWaitersForAHigherLimit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	dynamic := NewDynamicThrottler()
	dynamic.SetThrottlers([]Throttler{NewDynamicConcurrencyThrottler(1, "")})
	all := NewAllThrottler(dynamic)

	expectGranted(t, acquireAsync(ctx, all, "ns/a"))
	waiting := acquireAsync(ctx, all, "ns/b")
	expectWaiting(t, waiting)

	dynamic.SetThrottlers([]Throttler{NewDynamicConcurrencyThrottler(2, "")})
	expectGranted(t, waiting)
}

func TestConfigChangeToNoThrottlers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	dynamic := NewDynamicThrottler()
	dynamic.SetThrottlers([]Throttler{NewDynamicConcurrencyThrottler(1, "")})
	all := NewAllThrottler(dynamic)

	expectGranted(t, acquireAsync(ctx, all, "ns/a"))
	waiting := acquireAsync(ctx, all, "ns/b")
	expectWaiting(t, waiting)

	// the matching config was deleted
	dynamic.SetThrottlers([]Throttler{})
	expectGranted(t, waiting)
}

func TestConcurrentConfigChangesNeverExceedTheLimit(t *testing.T) {
	const limit, pods = 3, 40
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	// two throttlers, so that a replacement also hits requests that passed only the first one
	newSet := func() []Throttler {
		return []Throttler{NewDynamicConcurrencyThrottler(limit+2, ""), NewDynamicConcurrencyThrottler(limit, "")}
	}
	dynamic := NewDynamicThrottler()
	dynamic.SetThrottlers(newSet())
	all := NewAllThrottler(dynamic)

	var mu sync.Mutex
	starting, maxStarting := 0, 0
	stop, stopped := make(chan struct{}), make(chan struct{})
	go func() { // PacemakerConfig updates while pods start
		defer close(stopped)
		for {
			select {
			case <-stop:
				return
			case <-time.After(3 * time.Millisecond):
				dynamic.SetThrottlers(newSet())
			}
		}
	}()

	var wg sync.WaitGroup
	for i := 0; i < pods; i++ {
		wg.Add(1)
		go func(slotId string) {
			defer wg.Done()
			if err := all.AquireSlot(ctx, slotId, Data{}); err != nil {
				t.Errorf("AquireSlot(%s) error = %v", slotId, err)
				return
			}
			mu.Lock()
			starting++
			maxStarting = max(maxStarting, starting)
			mu.Unlock()
			time.Sleep(5 * time.Millisecond)
			mu.Lock()
			starting--
			mu.Unlock()
			all.ReleaseSlot(ctx, slotId)
		}(string(rune('a'+i%26)) + string(rune('0'+i/26)))
	}
	wg.Wait()
	close(stop)
	<-stopped

	if maxStarting > limit {
		t.Errorf("%d pods started at once, limit %d", maxStarting, limit)
	}
	if slots := all.ActiveSlots(); len(slots) != 0 {
		t.Errorf("slots left after all pods released theirs: %v", slots)
	}
}

func TestConfigChangeDoesNotSeedSlotsNobodyReleases(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	dynamic := NewDynamicThrottler()
	// a config with a rate limit only: its slots are not active anywhere, so the pod event handler
	// never releases them
	dynamic.SetThrottlers([]Throttler{NewRateLimitThrottler("1ms", 10)})
	all := NewAllThrottler(dynamic)
	for _, slotId := range []string{"ns/a", "ns/b", "ns/c"} {
		expectGranted(t, acquireAsync(ctx, all, slotId))
	}

	dynamic.SetThrottlers([]Throttler{NewDynamicConcurrencyThrottler(1, "")})
	expectGranted(t, acquireAsync(ctx, all, "ns/d"))
}

func TestTimeoutAcrossConfigChangeLeavesNoSlot(t *testing.T) {
	dynamic := NewDynamicThrottler()
	dynamic.SetThrottlers([]Throttler{NewDynamicConcurrencyThrottler(1, "")})
	all := NewAllThrottler(dynamic)
	expectGranted(t, acquireAsync(context.Background(), all, "ns/a"))

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	waiting := acquireAsync(ctx, all, "ns/b")
	time.Sleep(100 * time.Millisecond)
	dynamic.SetThrottlers([]Throttler{NewDynamicConcurrencyThrottler(1, "")})

	select {
	case err := <-waiting:
		if err == nil {
			t.Fatal("slot granted although the limit was reached until the timeout")
		}
	case <-time.After(time.Second):
		t.Fatal("AquireSlot did not return after its timeout")
	}
	if slots := all.ActiveSlots(); len(slots) != 1 || slots[0] != "ns/a" {
		t.Errorf("ActiveSlots() = %v, want [ns/a]", slots)
	}
}
