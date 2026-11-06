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
	tests := []struct {
		name        string
		pod         *v1.Pod
		wantRelease bool
	}{
		{"container creating", podWithStatus(v1.PodStatus{ContainerStatuses: []v1.ContainerStatus{waiting("ContainerCreating")}}), false},
		{"image does not exist", podWithStatus(v1.PodStatus{ContainerStatuses: []v1.ContainerStatus{waiting("ImagePullBackOff")}}), true},
		{"image pull failed", podWithStatus(v1.PodStatus{ContainerStatuses: []v1.ContainerStatus{waiting("ErrImagePull")}}), true},
		{"invalid image name", podWithStatus(v1.PodStatus{ContainerStatuses: []v1.ContainerStatus{waiting("InvalidImageName")}}), true},
		{"crash loop", podWithStatus(v1.PodStatus{ContainerStatuses: []v1.ContainerStatus{waiting("CrashLoopBackOff")}}), true},
		{"init container crash loop", podWithStatus(v1.PodStatus{
			InitContainerStatuses: []v1.ContainerStatus{waiting("CrashLoopBackOff")},
			ContainerStatuses:     []v1.ContainerStatus{waiting("PodInitializing")},
		}), true},
		{"init container image missing", podWithStatus(v1.PodStatus{
			InitContainerStatuses: []v1.ContainerStatus{waiting("ImagePullBackOff")},
			ContainerStatuses:     []v1.ContainerStatus{waiting("PodInitializing")},
		}), true},
		{"init container running", podWithStatus(v1.PodStatus{
			InitContainerStatuses: []v1.ContainerStatus{{Name: "init", State: v1.ContainerState{Running: &v1.ContainerStateRunning{}}}},
			ContainerStatuses:     []v1.ContainerStatus{waiting("PodInitializing")},
		}), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			slots := &slotRecorder{slots: map[string]bool{"ns/pod": true}}
			NewPodEventHandler(slots, context.Background(), 0).OnAdd(tt.pod)
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
