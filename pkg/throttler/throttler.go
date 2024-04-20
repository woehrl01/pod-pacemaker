package throttler

import (
	"context"

	v1 "k8s.io/api/core/v1"
)

type Data struct {
	Pod *v1.Pod
}

type Throttler interface {
	AquireSlot(ctx context.Context, slotId string, data Data) error
	ReleaseSlot(ctx context.Context, slotId string)
	String() string
}
