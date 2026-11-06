package main

import (
	"context"
	"fmt"
	"time"

	"woehrl01/pod-pacemaker/pkg/throttler"

	log "github.com/sirupsen/logrus"

	v1 "k8s.io/api/core/v1"
)

// imagePullGracePeriod keeps the slot of a pod whose image pull keeps failing for this long, counted
// from the first failure. A pull also
// fails while the registry throttles; releasing the slots right away would start every such pod of
// the node at once as soon as the pulls recover.
const imagePullGracePeriod = 2 * time.Minute

// failedWaitingReasons are the waiting reasons of a container that cannot start without someone
// changing the pod, e.g. an image that does not exist, with how long the slot is kept anyway. A pod
// waiting for one of them would otherwise keep its slot until it is deleted.
var failedWaitingReasons = map[string]time.Duration{
	"CrashLoopBackOff":           0,
	"CreateContainerConfigError": 0,
	"CreateContainerError":       0,
	"InvalidImageName":           0,
	"ErrImagePull":               imagePullGracePeriod,
	"ImagePullBackOff":           imagePullGracePeriod,
}

type PodEventHandler struct {
	throttler throttler.Throttler
	ctx       context.Context
	// maxSlotHold releases a slot held longer than this, 0 never does
	maxSlotHold time.Duration
	// slotSeen is when the handler first saw a pod holding its slot, failingSince when it first saw
	// it in a failed state with a grace period; the informer calls the handler from a single goroutine
	slotSeen     map[string]time.Time
	failingSince map[string]time.Time
	now          func() time.Time
}

func NewPodEventHandler(throttler throttler.Throttler, ctx context.Context, maxSlotHold time.Duration) *PodEventHandler {
	return &PodEventHandler{
		throttler:    throttler,
		ctx:          ctx,
		maxSlotHold:  maxSlotHold,
		slotSeen:     map[string]time.Time{},
		failingSince: map[string]time.Time{},
		now:          time.Now,
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
		delete(p.failingSince, slotName)
		return
	}

	seen, ok := p.slotSeen[slotName]
	if !ok {
		seen = p.now()
		p.slotSeen[slotName] = seen
	}

	// a pod without container statuses yet is neither started nor terminated
	hasStatuses := len(pod.Status.ContainerStatuses) > 0

	allStarted := hasStatuses // checking if all containers are started
	for _, containerStatus := range pod.Status.ContainerStatuses {
		if containerStatus.Started == nil || !*containerStatus.Started {
			allStarted = false
			break
		}
	}

	allTerminated := hasStatuses // checking if all containers are terminated, e.g. pod is done for jobs
	for _, containerStatus := range pod.Status.ContainerStatuses {
		if containerStatus.State.Terminated == nil {
			allTerminated = false
			break
		}
	}

	failedState := false // checking if any container, init containers included, has failed
	failingWithGrace := false
	for _, statuses := range [][]v1.ContainerStatus{pod.Status.InitContainerStatuses, pod.Status.ContainerStatuses} {
		for _, containerStatus := range statuses {
			if containerStatus.State.Waiting == nil {
				continue
			}
			grace, ok := failedWaitingReasons[containerStatus.State.Waiting.Reason]
			if !ok {
				continue
			}
			if grace == 0 {
				failedState = true
				continue
			}
			failingWithGrace = true
			since, ok := p.failingSince[slotName]
			if !ok {
				since = p.now()
				p.failingSince[slotName] = since
			}
			if p.now().Sub(since) >= grace {
				failedState = true
			}
		}
	}
	if !failingWithGrace { // e.g. the image pull recovered
		delete(p.failingSince, slotName)
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
	delete(p.failingSince, slotName)
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
