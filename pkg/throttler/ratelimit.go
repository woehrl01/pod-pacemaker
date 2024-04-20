package throttler

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/time/rate"
)

type RateLimitThrottler struct {
	rate *rate.Limiter
}

func NewRateLimitThrottler(r string, burst int) *RateLimitThrottler {
	dur, err := time.ParseDuration(r)
	if err != nil {
		logrus.Fatalf("failed to parse rate limit duration: %s", r)
	}
	return &RateLimitThrottler{
		rate: rate.NewLimiter(rate.Every(dur), burst),
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

func (t *RateLimitThrottler) ActiveSlots() []string {
	return []string{}
}
