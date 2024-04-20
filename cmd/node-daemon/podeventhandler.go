package main

import (
	"context"
	"fmt"

	"woehrl01/pod-pacemaker/pkg/throttler"

	log "github.com/sirupsen/logrus"

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

func (p *PodEventHandler) RemoveOutdatedSlots(currentPods []*v1.Pod) {
	activeSlots := p.throttler.ActiveSlots()

	currentPodNames := make(map[string]bool, len(currentPods))
	for _, pod := range currentPods {
		currentPodNames[buildSlotName(pod)] = true
	}

	for _, slot := range activeSlots {
		if _, ok := currentPodNames[slot]; ok {
			continue
		}
		log.WithField("slot", slot).Info("Removing outdated slot")
		p.throttler.ReleaseSlot(p.ctx, slot)
	}
}
