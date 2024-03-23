package types

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +groupName=woehrl.net
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=pacemakerconfigs,scope=Cluster

type ThrottlingConfig struct {
	// TypeMeta is the metadata for the resource, like kind and apiversion
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is the custom resource spec
    Spec   ThrottlingConfigSpec   `json:"spec,omitempty"`
}

type ThrottlingConfigSpec struct {
    Global            GlobalThrottlingConfig   `json:"global"`
    NodeSelectors     []NodeSelector           `json:"nodeSelectors"`
    NamespaceExclusions []string               `json:"namespaceExclusions"`
}

type GlobalThrottlingConfig struct {
    RateLimit         RateLimitConfig `json:"rateLimit"`
    MaxConcurrent     MaxConcurrentConfig `json:"maxConcurrent"`
    CpuThreshold      int         `json:"cpuThreshold"`
}

type NodeSelector struct {
    MatchLabels       map[string]string `json:"matchLabels"`
    RateLimit         RateLimitConfig   `json:"rateLimit"`
    MaxConcurrent     MaxConcurrentConfig `json:"maxConcurrent"`
    CpuThreshold      int           `json:"cpuThreshold"`
}

type RateLimitConfig struct {
    FillFactor        int `json:"fillFactor"`
    Burst             int `json:"burst"`
}

type MaxConcurrentConfig struct {
	// +kubebuilder:validation:Minimum=1
    Value             int `json:"value,omitempty"`
	// +kubebuilder:validation:Minimum=1
    PerCore           int `json:"perCore,omitempty"`
}
