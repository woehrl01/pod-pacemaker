package throttler

import (
	"context"
	"sync"

	"golang.org/x/time/rate"
)

type allThrottler struct {
	dynamic DynamicThrottler
}

type Options struct {
	MaxConcurrency       int
	MaxConcurrencyPerCpu int
	RateLimit            rate.Limit
	RateBurst            int
}

func NewAllThrottler(dynamic DynamicThrottler) Throttler {
	return &allThrottler{
		dynamic: dynamic,
	}
}

var _ Throttler = &allThrottler{}

func (t *allThrottler) String() string {
	return "AllThrottler"
}

// AquireSlot acquires the slot from every throttler of the current set. When the set is replaced
// meanwhile (a PacemakerConfig changed), the waiting in the old set is cancelled and starts over in
// the new one; nobody releases slots in the old set any more.
func (t *allThrottler) AquireSlot(ctx context.Context, slotId string, data Data) error {
	for {
		list, replaced := t.dynamic.snapshot()

		attemptCtx, cancel := context.WithCancel(ctx)
		go func() {
			select {
			case <-replaced:
				cancel()
			case <-attemptCtx.Done():
			}
		}()

		err := acquireAll(attemptCtx, list, slotId, data)
		cancel()

		if err == nil && t.dynamic.isCurrent(replaced) {
			return nil
		}
		if !isClosed(replaced) {
			return err
		}
		// the set was replaced: it no longer counts, the new one has taken over what was active in it
		releaseAll(ctx, list, slotId)
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
}

func acquireAll(ctx context.Context, list []Throttler, slotId string, data Data) error {
	for _, throttle := range list {
		if err := throttle.AquireSlot(ctx, slotId, data); err != nil {
			return err
		}
	}
	return nil
}

func releaseAll(ctx context.Context, list []Throttler, slotId string) {
	for i := len(list) - 1; i >= 0; i-- {
		list[i].ReleaseSlot(ctx, slotId)
	}
}

func isClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// ReleaseSlot releases the slot in the current set; when the set was replaced while releasing, the
// new set may have taken the slot over and releases it too
func (t *allThrottler) ReleaseSlot(ctx context.Context, slotId string) {
	for {
		list, replaced := t.dynamic.snapshot()
		releaseAll(ctx, list, slotId)
		if !isClosed(replaced) {
			return
		}
	}
}

func (t *allThrottler) ActiveSlots() []string {
	list, _ := t.dynamic.snapshot()
	return activeSlots(list)
}

func activeSlots(list []Throttler) []string {
	slots := []string{}
	for _, throttle := range list {
		slots = append(slots, throttle.ActiveSlots()...)
	}
	return slots
}

// slotSeeder is a throttler that keeps track of its slots and can take over the slots granted by the
// set it replaces
type slotSeeder interface {
	SeedSlots(slotIds []string)
}

// grantedSlots are the slots active in every throttler of the set that keeps track of slots. A slot
// active only in some of them belongs to a request still waiting for the others; it starts over in
// the new set and must not count as granted there.
func grantedSlots(list []Throttler) []string {
	counts := map[string]int{}
	trackers := 0
	for _, throttle := range list {
		if _, ok := throttle.(slotSeeder); !ok {
			continue
		}
		trackers++
		for _, slotId := range throttle.ActiveSlots() {
			counts[slotId]++
		}
	}
	slotIds := []string{}
	for slotId, count := range counts {
		if count == trackers {
			slotIds = append(slotIds, slotId)
		}
	}
	return slotIds
}

type dynamicThrottler struct {
	mu               sync.Mutex
	activeThrottlers []Throttler
	// replaced is closed when activeThrottlers is replaced
	replaced chan struct{}
}

func NewDynamicThrottler() DynamicThrottler {
	return &dynamicThrottler{
		activeThrottlers: []Throttler{},
		replaced:         make(chan struct{}),
	}
}

// SetThrottlers replaces the throttler set. The new throttlers take over the slots granted by the old
// set, so that the pods still starting keep counting against the limits; these slots are active in
// the new set, so the pod event handler sees and releases them.
func (t *dynamicThrottler) SetThrottlers(throttlers []Throttler) {
	t.mu.Lock()
	defer t.mu.Unlock()

	slotIds := grantedSlots(t.activeThrottlers)
	for _, throttle := range throttlers {
		if seeder, ok := throttle.(slotSeeder); ok {
			seeder.SeedSlots(slotIds)
		}
	}

	t.activeThrottlers = throttlers
	close(t.replaced)
	t.replaced = make(chan struct{})
}

func (t *dynamicThrottler) GetThrottlers() []Throttler {
	list, _ := t.snapshot()
	return list
}

func (t *dynamicThrottler) snapshot() ([]Throttler, <-chan struct{}) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.activeThrottlers, t.replaced
}

// isCurrent reports whether the set that was current with replaced still is. A slot acquired from a
// set replaced in the meantime was not taken over by the new one.
func (t *dynamicThrottler) isCurrent(replaced <-chan struct{}) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return !isClosed(replaced)
}

type DynamicThrottler interface {
	SetThrottlers(throttlers []Throttler)
	GetThrottlers() []Throttler
	snapshot() ([]Throttler, <-chan struct{})
	isCurrent(replaced <-chan struct{}) bool
}
