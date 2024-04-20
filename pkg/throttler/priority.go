package throttler

import (
	"context"
	"fmt"
	"math"
	"runtime"
	"strconv"
	"sync"

	"github.com/sirupsen/logrus"
)

type ConcurrencyController struct {
	mu              sync.Mutex
	waitOnCondition chan struct{}
	condition       func(int) (bool, error)
	conditionText   string
	onAquire        func()
	activeItems     map[string]bool
}

type DynamicOptions struct {
	Condition    func(int) (bool, error)
	OnAquire     func()
	ConditionStr string
}

func NewDynamicConcurrencyThrottler(staticLimit int, perCpu string) *ConcurrencyController {
	limit := staticLimit
	limitType := "static"
	if staticLimit == 0 && perCpu != "" {
		perCpuFloat, _ := strconv.ParseFloat(perCpu, 64)
		limit = int(math.Ceil(perCpuFloat * float64(runtime.NumCPU())))
		limitType = fmt.Sprintf("perCpu = %s", perCpu)
	}

	if limit < 1 {
		logrus.Warnf("Concurrency limit is too low, setting to 1")
		limit = 1
	}

	c, _ := NewConcurrencyControllerWithDynamicCondition(
		&DynamicOptions{
			Condition:    func(currentLength int) (bool, error) { return currentLength < limit, nil },
			OnAquire:     func() {},
			ConditionStr: fmt.Sprintf("maxConcurrent = %d, %s", limit, limitType),
		},
	)
	return c
}

func NewConcurrencyControllerWithDynamicCondition(options *DynamicOptions) (*ConcurrencyController, func()) {
	cc := &ConcurrencyController{
		condition:       options.Condition,
		conditionText:   options.ConditionStr,
		onAquire:        options.OnAquire,
		activeItems:     make(map[string]bool),
		waitOnCondition: make(chan struct{}),
	}
	return cc, func() {
		cc.mu.Lock()
		defer cc.mu.Unlock()
		cc.broadcastPossibleConditionChange()
	}
}

var _ Throttler = &ConcurrencyController{}

func (cc *ConcurrencyController) String() string {
	return fmt.Sprintf("PriorityThrottler, condition: %s", cc.conditionText)
}

func (cc *ConcurrencyController) broadcastPossibleConditionChange() {
	// Broadcast to all waiting goroutines. This needs be called with the lock held.
	close(cc.waitOnCondition)
	cc.waitOnCondition = make(chan struct{})
}

func (cc *ConcurrencyController) AquireSlot(ctx context.Context, slotId string, data Data) error {
	for {
		if done, err := func() (bool, error) {
			cc.mu.Lock()
			defer cc.mu.Unlock()
			_, isActive := cc.activeItems[slotId]
			if ctx.Err() != nil { // Context was cancelled.
				if !isActive { // Remove the item if it wasn't activated.
					cc.removeItem(slotId)
				}
				return true, ctx.Err()
			}
			if isActive { // Item is already active.
				return true, nil
			} else { // Item is not active.
				cond, err := cc.condition(len(cc.activeItems))
				if err != nil {
					return true, err
				}
				if cond { // Item can be activated.
					cc.activeItems[slotId] = true
					cc.onAquire()
					return true, nil
				}
			}
			return false, nil
		}(); done {
			return err
		}

		select {
		case <-cc.waitOnCondition:
		case <-ctx.Done():
		}
	}
}

func (cc *ConcurrencyController) removeItem(slotId string) {
	delete(cc.activeItems, slotId)
	cc.broadcastPossibleConditionChange()
}

func (cc *ConcurrencyController) ReleaseSlot(ctx context.Context, slotId string) {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	cc.removeItem(slotId)
}

func (cc *ConcurrencyController) ActiveSlots() []string {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	var slots []string
	for slot := range cc.activeItems {
		slots = append(slots, slot)
	}
	return slots
}
