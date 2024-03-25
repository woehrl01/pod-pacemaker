package throttler

import (
	"container/heap"
	"context"
	"fmt"
	"math"
	"runtime"
	"strconv"
	"sync"
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
	pq            PriorityQueue
	mu            sync.Mutex
	cond          *sync.Cond
	condition     func(int) (bool, error)
	conditionText string
	active        map[string]*Item
}

func NewPriorityThrottler(staticLimit int, perCpu string) *ConcurrencyController {
	limit := staticLimit
	limitType := "static"
	if staticLimit == 0 {
		perCpuFloat, _ := strconv.ParseFloat(perCpu, 64)
		limit = int(math.Ceil(perCpuFloat * float64(runtime.NumCPU())))
		limitType = fmt.Sprintf("perCpu = %s", perCpu)
	}
	c, _ := NewConcurrencyControllerWithDynamicCondition(func(currentLength int) (bool, error) { return currentLength < limit, nil }, fmt.Sprintf("maxConcurrent = %d, %s", limit, limitType))
	return c
}

func NewConcurrencyControllerWithDynamicCondition(condition func(int) (bool, error), conditionText string) (*ConcurrencyController, func()) {
	cc := &ConcurrencyController{
		pq:            make(PriorityQueue, 0),
		condition:     condition,
		conditionText: conditionText,
		active:        make(map[string]*Item),
	}
	cc.cond = sync.NewCond(&cc.mu)
	return cc, func() {
		cc.cond.Broadcast()
	}
}

var _ Throttler = &ConcurrencyController{}

func (cc *ConcurrencyController) String() string {
	return fmt.Sprintf("PriorityThrottler, condition: %s", cc.conditionText)
}

func (cc *ConcurrencyController) AquireSlot(ctx context.Context, slotId string, data Data) error {
	cc.mu.Lock()
	item := &Item{
		value:    slotId,
		priority: data.Priority,
	}
	heap.Push(&cc.pq, item)
	cc.mu.Unlock()

	for {
		cc.mu.Lock()
		active, ok := cc.active[slotId]
		if ctx.Err() != nil { // Context was cancelled.
			if !ok || active == item { // Remove the item if it wasn't activated.
				heap.Remove(&cc.pq, item.index)
				cc.cond.Broadcast()
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
			if cond {
				cc.active[slotId] = item
				cc.mu.Unlock()
				return nil
			}
		}
		cc.mu.Unlock()
		select {
		case <-ctx.Done():
			// The operation was cancelled, clean-up is handled at the beginning of the loop.
		default:
			cc.cond.L.Lock()
			cc.cond.Wait()
			cc.cond.L.Unlock()
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
		cc.cond.Broadcast()
	}
}
