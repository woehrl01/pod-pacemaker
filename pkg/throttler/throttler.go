package throttler

import "context"

type Data struct {
	Priority int
}

type Throttler interface {
	AquireSlot(ctx context.Context, slotId string, data Data) error
	ReleaseSlot(ctx context.Context, slotId string)
	String() string
}
