package main

import (
	"context"
	"fmt"
	"time"

	"woehrl01/pod-pacemaker/pkg/throttler"

	log "github.com/sirupsen/logrus"

	v1 "k8s.io/api/core/v1"
)

// failedWaitingReasons are the waiting reasons of a container that cannot start without someone
// changing the pod, e.g. an image that does not exist. A pod waiting for one of them would keep its
// slot until it is deleted.
var failedWaitingReasons = map[string]bool{
	"CrashLoopBackOff":           true,
	"CreateContainerConfigError": true,
	"CreateContainerError":       true,
	"ErrImagePull":               true,
	"ImagePullBackOff":           true,
	"InvalidImageName":           true,
}

type PodEventHandler struct {
	throttler throttler.Throttler
	ctx       context.Context
	// maxSlotHold releases a slot held longer than this, 0 never does
	maxSlotHold time.Duration
	// slotSeen is when the handler first saw a pod holding its slot; the informer calls the
	// handler from a single goroutine
	slotSeen map[string]time.Time
	now      func() time.Time
}

func NewPodEventHandler(throttler throttler.Throttler, ctx context.Context, maxSlotHold time.Duration) *PodEventHandler {
	return &PodEventHandler{
		throttler:   throttler,
		ctx:         ctx,
		maxSlotHold: maxSlotHold,
		slotSeen:    map[string]time.Time{},
		now:         time.Now,
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
		delete(p.slotSeen, slotName)
		return
	}

	seen, ok := p.slotSeen[slotName]
	if !ok {
		seen = p.now()
		p.slotSeen[slotName] = seen
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

	failedState := false // checking if any container, init containers included, has failed
	for _, statuses := range [][]v1.ContainerStatus{pod.Status.InitContainerStatuses, pod.Status.ContainerStatuses} {
		for _, containerStatus := range statuses {
			if containerStatus.State.Waiting != nil && failedWaitingReasons[containerStatus.State.Waiting.Reason] {
				failedState = true
				break
			}
		}
	}

	markedAsDeleted := pod.DeletionTimestamp != nil
	completed := pod.Status.Phase == v1.PodSucceeded || pod.Status.Phase == v1.PodFailed
	heldTooLong := p.maxSlotHold > 0 && p.now().Sub(seen) > p.maxSlotHold

	if allStarted {
		log.WithField("pod", slotName).Debug("Pod is fully started, releasing slot")
		p.releaseSlot(slotName)
	} else if allTerminated || completed {
		log.WithField("pod", slotName).Debug("Pod is completed, releasing slot")
		p.releaseSlot(slotName)
	} else if markedAsDeleted {
		log.WithField("pod", slotName).Debug("Pod is marked as deleted, releasing slot")
		p.releaseSlot(slotName)
	} else if failedState {
		log.WithField("pod", slotName).Debug("Pod has failed, releasing slot")
		p.releaseSlot(slotName)
	} else if heldTooLong {
		log.WithField("pod", slotName).Warnf("Pod holds its slot longer than %s, releasing slot", p.maxSlotHold)
		p.releaseSlot(slotName)
	} else {
		log.WithField("pod", slotName).Debug("Pod is not fully started yet")
	}
}

func (p *PodEventHandler) OnDelete(pod *v1.Pod) {
	p.releaseSlot(buildSlotName(pod))
}

func (p *PodEventHandler) releaseSlot(slotName string) {
	delete(p.slotSeen, slotName)
	p.throttler.ReleaseSlot(p.ctx, slotName)
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
