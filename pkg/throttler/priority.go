package throttler

import (
	"container/heap"
	"context"
	"fmt"
	"math"
	"runtime"
	"strconv"
	"sync"

	"github.com/sirupsen/logrus"
)

type Item struct {
	value    string
	priority int
	index    int
}

type PriorityQueue []*Item

func (pq PriorityQueue) Len() int { return len(pq) }

func (pq PriorityQueue) Less(i, j int) bool {
	return pq[i].priority > pq[j].priority
}

func (pq PriorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}

func (pq *PriorityQueue) Push(x interface{}) {
	n := len(*pq)
	item := x.(*Item)
	item.index = n
	*pq = append(*pq, item)
}

func (pq *PriorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*pq = old[0 : n-1]
	return item
}

type ConcurrencyController struct {
	pq              PriorityQueue
	mu              sync.Mutex
	waitOnCondition chan struct{}
	condition       func(int) (bool, error)
	conditionText   string
	onAquire        func()
	active          map[string]*Item
	currentItems    map[string]*Item
}

type DynamicOptions struct {
	Condition    func(int) (bool, error)
	OnAquire     func()
	ConditionStr string
}

func NewPriorityThrottler(staticLimit int, perCpu string) *ConcurrencyController {
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
		pq:            make(PriorityQueue, 0),
		condition:     options.Condition,
		conditionText: options.ConditionStr,
		onAquire:      options.OnAquire,
		active:        make(map[string]*Item),
		currentItems:  make(map[string]*Item),
	}
	cc.waitOnCondition = make(chan struct{})
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
	if existing, ok := cc.currentItems[slotId]; ok {
		item = existing
	} else {
		item := &Item{
			value:    slotId,
			priority: data.Priority,
		}
		cc.currentItems[slotId] = item
		heap.Push(&cc.pq, item)
	}
	cc.mu.Unlock()

	for {
		cc.mu.Lock()
		active, ok := cc.active[slotId]
		if ctx.Err() != nil { // Context was cancelled.
			if !ok { // Remove the item if it wasn't activated.
				heap.Remove(&cc.pq, item.index)
				delete(cc.currentItems, slotId)
				cc.broadcastPossibleConditionChange()
			}
			cc.mu.Unlock()
			return ctx.Err()
		}
		if active == nil {
			cond, err := cc.condition(len(cc.active))
			if err != nil {
				cc.mu.Unlock()
				return err
			}
			if cond { // Item can be activated.
				cc.active[slotId] = item
				heap.Remove(&cc.pq, item.index)
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

func (cc *ConcurrencyController) ReleaseSlot(ctx context.Context, slotId string) {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	if _, ok := cc.active[slotId]; ok {
		delete(cc.active, slotId)
		if cc.pq.Len() > 0 {
			heap.Pop(&cc.pq)
		}
		cc.broadcastPossibleConditionChange()
	}
}
