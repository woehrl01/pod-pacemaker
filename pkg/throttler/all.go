package throttler

import (
	"context"

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

func (t *allThrottler) AquireSlot(ctx context.Context, slotId string, data Data) error {
	list := t.dynamic.GetThrottlers()

	for _, throttle := range list {
		if err := throttle.AquireSlot(ctx, slotId, data); err != nil {
			return err
		}
	}
	return nil
}

func (t *allThrottler) ReleaseSlot(ctx context.Context, slotId string) {
	list := t.dynamic.GetThrottlers()

	for i := len(list) - 1; i >= 0; i-- {
		throttle := list[i]
		throttle.ReleaseSlot(ctx, slotId)
	}
}

type dynamicThrottler struct {
	activeThrottlers []Throttler
}

func NewDynamicThrottler() DynamicThrottler {
	return &dynamicThrottler{
		activeThrottlers: []Throttler{},
	}
}

func (t *dynamicThrottler) SetThrottlers(throttlers []Throttler) {
	t.activeThrottlers = throttlers
}

func (t *dynamicThrottler) GetThrottlers() []Throttler {
	return t.activeThrottlers
}

type DynamicThrottler interface {
	SetThrottlers(throttlers []Throttler)
	GetThrottlers() []Throttler
}
