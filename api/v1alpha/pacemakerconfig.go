package v1alpha

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +groupName=woehrl.net
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=pacemakerconfigs,scope=Cluster

type PacemakerConfig struct {
	// TypeMeta is the metadata for the resource, like kind and apiversion
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is the custom resource spec
	Spec PacemakerConfigSpec `json:"spec,omitempty"`
}

type PacemakerConfigSpec struct {
	MatchNodeLabels     map[string]string  `json:"matchNodeLabels"`
	ThrottleConfig      NodeThrottleConfig `json:"throttleConfig"`
	NamespaceExclusions []string           `json:"namespaceExclusions"`
	Priority            int                `json:"priority"`
}

type NodeThrottleConfig struct {
	RateLimit     RateLimitConfig     `json:"rateLimit"`
	MaxConcurrent MaxConcurrentConfig `json:"maxConcurrent"`
	CpuThreshold  int                 `json:"cpuThreshold"`
	MaxIOLoad     int                 `json:"maxIOLoad"`
}

type RateLimitConfig struct {
	FillFactor int `json:"fillFactor"`
	Burst      int `json:"burst"`
}

type MaxConcurrentConfig struct {
	// +kubebuilder:validation:Minimum=1
	Value int `json:"value,omitempty"`
	// +kubebuilder:validation:Minimum=1
	PerCore int `json:"perCore,omitempty"`
}
