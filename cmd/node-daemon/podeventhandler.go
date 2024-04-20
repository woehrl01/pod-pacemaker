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
	activeSlots := p.throttler.ActiveSlots()
	slotName := buildSlotName(pod)
	hasSlot := false
	for _, slot := range activeSlots {
		if slot == slotName {
			hasSlot = true
			break
		}
	}

	if !hasSlot { // nothing to do as it has no slot
		return
	}

	allStarted := true // checking if all containers are started
	for _, containerStatus := range pod.Status.ContainerStatuses {
		if containerStatus.Started == nil || !*containerStatus.Started {
			allStarted = false
			break
		}
	}

	allTerminated := true // checking if all containers are terminated, e.g. pod is done for jobs
	for _, containerStatus := range pod.Status.ContainerStatuses {
		if containerStatus.State.Terminated == nil {
			allTerminated = false
			break
		}
	}

	completed := pod.Status.Phase == v1.PodSucceeded || pod.Status.Phase == v1.PodFailed

	if allStarted || allTerminated || completed {
		log.WithField("pod", slotName).Debug("Pod is fully started, releasing slot")
		p.throttler.ReleaseSlot(p.ctx, slotName)
	} else {
		log.WithField("pod", slotName).Debug("Pod is not fully started yet")
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
		log.WithField("slot", slot).Warn("Removing outdated slot")
		p.throttler.ReleaseSlot(p.ctx, slot)
	}
}
