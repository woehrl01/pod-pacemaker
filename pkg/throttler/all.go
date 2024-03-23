package throttler

import (
	"context"

	"golang.org/x/time/rate"
	"k8s.io/client-go/kubernetes"
)

type allThrottler struct {
	throttleList []Throttler
}

type Options struct {
	MaxConcurrency int
	MaxConcurrencyPerCpu int
	RateLimit      rate.Limit
	RateBurst      int
}

func NewAllThrottler(clientset *kubernetes.Clientset, o *Options) Throttler {
	throttleList := []Throttler{
		NewRateLimitThrottler(o.RateLimit, o.RateBurst),
		NewPriorityThrottler(o.MaxConcurrency, o.MaxConcurrencyPerCpu),
	}

	return &allThrottler{
		throttleList: throttleList,
	}
}

var _ Throttler = &allThrottler{}

func (t *allThrottler) AquireSlot(ctx context.Context, slotId string, data Data) error {
	for _, throttle := range t.throttleList {
		if err := throttle.AquireSlot(ctx, slotId, data); err != nil {
			return err
		}
	}
	return nil
}

func (t *allThrottler) ReleaseSlot(ctx context.Context, slotId string) {
	for i := len(t.throttleList) - 1; i >= 0; i-- {
		throttle := t.throttleList[i]
		throttle.ReleaseSlot(ctx, slotId)
	}
}
