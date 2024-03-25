package main

import (
	"context"
	"fmt"

	"woehrl01/pod-pacemaker/pkg/throttler"

	v1 "k8s.io/api/core/v1"
)

type PodEventHandler struct {
	throttler throttler.Throttler
	ctx       context.Context
}

func NewPodEventHandler(throttler throttler.Throttler, ctx context.Context) *PodEventHandler {
	return &PodEventHandler{
		throttler: throttler,
		ctx:       ctx,
	}
}

func (p *PodEventHandler) OnAdd(pod *v1.Pod) {
	allStarted := true
	for _, containerStatus := range pod.Status.ContainerStatuses {
		if containerStatus.Started == nil || !*containerStatus.Started {
			allStarted = false
			break
		}
	}
	if allStarted {
		p.throttler.ReleaseSlot(p.ctx, buildSlotName(pod))
	}
}

func (p *PodEventHandler) OnDelete(pod *v1.Pod) {
	p.throttler.ReleaseSlot(p.ctx, buildSlotName(pod))
}

func buildSlotName(pod *v1.Pod) string {
	return fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
}
