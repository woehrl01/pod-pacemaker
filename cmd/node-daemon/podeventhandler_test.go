package main

import (
	"context"
	"testing"
	"time"

	"woehrl01/pod-pacemaker/pkg/throttler"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// slotRecorder holds the slots that were acquired and not released yet
type slotRecorder struct {
	slots map[string]bool
}

func (s *slotRecorder) AquireSlot(_ context.Context, slotId string, _ throttler.Data) error {
	s.slots[slotId] = true
	return nil
}
func (s *slotRecorder) ReleaseSlot(_ context.Context, slotId string) { delete(s.slots, slotId) }
func (s *slotRecorder) String() string                               { return "slotRecorder" }
func (s *slotRecorder) ActiveSlots() []string {
	slots := []string{}
	for slot := range s.slots {
		slots = append(slots, slot)
	}
	return slots
}

func waiting(reason string) v1.ContainerStatus {
	return v1.ContainerStatus{Name: "c", State: v1.ContainerState{Waiting: &v1.ContainerStateWaiting{Reason: reason}}}
}

func podWithStatus(status v1.PodStatus) *v1.Pod {
	status.Phase = v1.PodPending
	return &v1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "pod"}, Status: status}
}

func TestOnAddReleasesSlotOfPodThatCannotStart(t *testing.T) {
	imageMissing := podWithStatus(v1.PodStatus{ContainerStatuses: []v1.ContainerStatus{waiting("ImagePullBackOff")}})
	initImageMissing := podWithStatus(v1.PodStatus{
		InitContainerStatuses: []v1.ContainerStatus{waiting("ImagePullBackOff")},
		ContainerStatuses:     []v1.ContainerStatus{waiting("PodInitializing")},
	})
	tests := []struct {
		name        string
		pod         *v1.Pod
		held        time.Duration // how long the pod holds its slot and is in its current state already
		wantRelease bool
	}{
		{"no container statuses yet", podWithStatus(v1.PodStatus{}), 0, false},
		{"container creating", podWithStatus(v1.PodStatus{ContainerStatuses: []v1.ContainerStatus{waiting("ContainerCreating")}}), 0, false},
		{"image pull fails, maybe the registry throttles", imageMissing, time.Minute, false},
		{"image pull keeps failing", imageMissing, imagePullGracePeriod, true},
		{"image pull failed", podWithStatus(v1.PodStatus{ContainerStatuses: []v1.ContainerStatus{waiting("ErrImagePull")}}), imagePullGracePeriod, true},
		{"invalid image name", podWithStatus(v1.PodStatus{ContainerStatuses: []v1.ContainerStatus{waiting("InvalidImageName")}}), 0, true},
		{"crash loop", podWithStatus(v1.PodStatus{ContainerStatuses: []v1.ContainerStatus{waiting("CrashLoopBackOff")}}), 0, true},
		{"init container crash loop", podWithStatus(v1.PodStatus{
			InitContainerStatuses: []v1.ContainerStatus{waiting("CrashLoopBackOff")},
			ContainerStatuses:     []v1.ContainerStatus{waiting("PodInitializing")},
		}), 0, true},
		{"init container image pull fails", initImageMissing, time.Minute, false},
		{"init container image pull keeps failing", initImageMissing, imagePullGracePeriod, true},
		{"init container running", podWithStatus(v1.PodStatus{
			InitContainerStatuses: []v1.ContainerStatus{{Name: "init", State: v1.ContainerState{Running: &v1.ContainerStateRunning{}}}},
			ContainerStatuses:     []v1.ContainerStatus{waiting("PodInitializing")},
		}), 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			slots := &slotRecorder{slots: map[string]bool{"ns/pod": true}}
			handler := NewPodEventHandler(slots, context.Background(), 0)
			now := time.Now()
			handler.now = func() time.Time { return now }
			handler.slotSeen["ns/pod"] = now.Add(-tt.held)
			handler.failingSince["ns/pod"] = now.Add(-tt.held)
			handler.OnAdd(tt.pod)
			if released := !slots.slots["ns/pod"]; released != tt.wantRelease {
				t.Errorf("slot released = %v, want %v", released, tt.wantRelease)
			}
		})
	}
}

func TestOnAddReleasesSlotHeldTooLong(t *testing.T) {
	slots := &slotRecorder{slots: map[string]bool{"ns/pod": true}}
	handler := NewPodEventHandler(slots, context.Background(), 5*time.Minute)
	now := time.Now()
	handler.now = func() time.Time { return now }
	// a pod stuck in a state the handler does not know as failed
	pod := podWithStatus(v1.PodStatus{ContainerStatuses: []v1.ContainerStatus{waiting("SomeNewReason")}})

	handler.OnAdd(pod)
	now = now.Add(4 * time.Minute)
	handler.OnAdd(pod)
	if !slots.slots["ns/pod"] {
		t.Fatal("slot released before maxSlotHold")
	}

	now = now.Add(2 * time.Minute)
	handler.OnAdd(pod)
	if slots.slots["ns/pod"] {
		t.Fatal("slot still held after maxSlotHold")
	}
	if len(handler.slotSeen) != 0 {
		t.Errorf("slotSeen keeps released slots: %v", handler.slotSeen)
	}
}

func TestOnAddKeepsSlotWithoutMaxSlotHold(t *testing.T) {
	slots := &slotRecorder{slots: map[string]bool{"ns/pod": true}}
	handler := NewPodEventHandler(slots, context.Background(), 0)
	now := time.Now()
	handler.now = func() time.Time { return now }
	pod := podWithStatus(v1.PodStatus{ContainerStatuses: []v1.ContainerStatus{waiting("SomeNewReason")}})

	handler.OnAdd(pod)
	now = now.Add(24 * time.Hour)
	handler.OnAdd(pod)
	if !slots.slots["ns/pod"] {
		t.Fatal("slot released although maxSlotHold is disabled")
	}
}

func TestImagePullGraceCountsFromFirstFailure(t *testing.T) {
	slots := &slotRecorder{slots: map[string]bool{"ns/pod": true}}
	handler := NewPodEventHandler(slots, context.Background(), 0)
	now := time.Now()
	handler.now = func() time.Time { return now }
	pulling := podWithStatus(v1.PodStatus{ContainerStatuses: []v1.ContainerStatus{waiting("ContainerCreating")}})
	pullFails := podWithStatus(v1.PodStatus{ContainerStatuses: []v1.ContainerStatus{waiting("ErrImagePull")}})

	// a big image pulls for 3 minutes, then the registry starts to throttle
	handler.OnAdd(pulling)
	now = now.Add(3 * time.Minute)
	handler.OnAdd(pullFails)
	if !slots.slots["ns/pod"] {
		t.Fatal("slot released on the first pull failure")
	}

	// the pull recovers for a moment and fails again: the grace period starts over
	now = now.Add(time.Minute)
	handler.OnAdd(pulling)
	now = now.Add(90 * time.Second)
	handler.OnAdd(pullFails)
	if !slots.slots["ns/pod"] {
		t.Fatal("slot released although the pull failed only now again")
	}

	now = now.Add(imagePullGracePeriod)
	handler.OnAdd(pullFails)
	if slots.slots["ns/pod"] {
		t.Fatal("slot still held after the pull failed for the whole grace period")
	}
	if len(handler.failingSince) != 0 || len(handler.slotSeen) != 0 {
		t.Errorf("released slot left state behind: %v %v", handler.failingSince, handler.slotSeen)
	}
}
