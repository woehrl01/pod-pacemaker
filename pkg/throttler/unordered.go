package throttler

import (
	"context"

	"github.com/sirupsen/logrus"
	"golang.org/x/sync/semaphore"
)

var _ Throttler = &throttler{}

type throttler struct {
	mapping     map[string]bool
	limit       int
	lock        *semaphore.Weighted
	isBlockedCh chan bool
}

func NewUnorderedThrottler(limit int) Throttler {
	return &throttler{
		mapping:     make(map[string]bool),
		limit:       limit,
		lock:        semaphore.NewWeighted(int64(1)),
		isBlockedCh: make(chan bool, 1),
	}
}

func (t *throttler) AquireSlot(ctx context.Context, slotId string, _ Data) error {
	for {
		if err := t.lock.Acquire(ctx, 1); err != nil {
			return err
		}

		if _, ok := t.mapping[slotId]; ok {
			t.lock.Release(1)
			logrus.Debugf("Slot %s already acquired", slotId)
			return nil
		}

		if len(t.mapping) < t.limit {
			t.mapping[slotId] = true
			t.lock.Release(1)
			logrus.Debugf("Acquiring slot %s", slotId)
			return nil
		}
		t.lock.Release(1)
		logrus.Debugf("Slot %s is blocked", slotId)
		select {
		case <-ctx.Done():
			return nil
		case <-t.isBlockedCh:
		}
	}
}

func (t *throttler) ReleaseSlot(ctx context.Context, slotId string) {
	t.lock.Acquire(ctx, 1)
	defer t.lock.Release(1)
	if _, ok := t.mapping[slotId]; !ok {
		return
	}
	logrus.Debugf("Releasing slot %s", slotId)
	delete(t.mapping, slotId)
	select {
	case t.isBlockedCh <- false:
	default:
	}
}
