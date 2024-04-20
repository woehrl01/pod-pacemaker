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

type Item struct {
	value string
}

type ConcurrencyController struct {
	mu              sync.Mutex
	waitOnCondition chan struct{}
	condition       func(int) (bool, error)
	conditionText   string
	onAquire        func()
	activeItems     map[string]*Item
	inflightItems   map[string]*Item
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
		activeItems:     make(map[string]*Item),
		inflightItems:   make(map[string]*Item),
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
	cc.mu.Lock()
	var item *Item
	// If we already have an item for this slot, use it.
	if existing, ok := cc.inflightItems[slotId]; ok {
		item = existing
	} else {
		item := &Item{
			value: slotId,
		}
		cc.inflightItems[slotId] = item
	}
	cc.mu.Unlock()

	for {
		cc.mu.Lock()
		active, ok := cc.activeItems[slotId]
		if ctx.Err() != nil { // Context was cancelled.
			if !ok { // Remove the item if it wasn't activated.
				cc.removeItem(item)
			}
			cc.mu.Unlock()
			return ctx.Err()
		}
		if active == nil {
			cond, err := cc.condition(len(cc.activeItems))
			if err != nil {
				cc.mu.Unlock()
				return err
			}
			if cond { // Item can be activated.
				cc.activeItems[slotId] = item
				cc.onAquire()
				cc.mu.Unlock()
				return nil
			}
		}
		cc.mu.Unlock()
		select {
		case <-cc.waitOnCondition:
		case <-ctx.Done():
		}
	}
}

func (cc *ConcurrencyController) removeItem(item *Item) {
	delete(cc.activeItems, item.value)
	delete(cc.inflightItems, item.value)
	cc.broadcastPossibleConditionChange()
}

func (cc *ConcurrencyController) ReleaseSlot(ctx context.Context, slotId string) {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	if item, ok := cc.activeItems[slotId]; ok {
		cc.removeItem(item)
	}
}
