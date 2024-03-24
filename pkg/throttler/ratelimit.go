package throttler

import (
	"context"
	"fmt"

	"golang.org/x/time/rate"
)

type RateLimitThrottler struct {
	rate *rate.Limiter
}

func NewRateLimitThrottler(r rate.Limit, burst int) *RateLimitThrottler {
	return &RateLimitThrottler{
		rate: rate.NewLimiter(r, burst),
	}
}

func (t *RateLimitThrottler) AquireSlot(ctx context.Context, slotId string, _ Data) error {
	if err := t.rate.Wait(ctx); err != nil {
		return err
	}
	return nil
}

func (t *RateLimitThrottler) ReleaseSlot(ctx context.Context, slotId string) {
}

func (t *RateLimitThrottler) String() string {
	return fmt.Sprintf("RateLimitThrottler(rate=%v, burst=%d)", t.rate.Limit(), t.rate.Burst())
}
