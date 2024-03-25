package v1alpha

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
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
	NodeSelector   map[string]string  `json:"nodeSelector"`
	ThrottleConfig NodeThrottleConfig `json:"throttleConfig"`
	Priority       int                `json:"priority"`
}

type NodeThrottleConfig struct {
	// +kubebuilder:validation:Optional
	RateLimit RateLimitConfig `json:"rateLimit"`
	// +kubebuilder:validation:Optional
	MaxConcurrent MaxConcurrentConfig `json:"maxConcurrent"`
	// +kubebuilder:validation:Optional
	CpuThreshold int `json:"cpuThreshold"`
	// +kubebuilder:validation:Optional
	MaxIOLoad int `json:"maxIOLoad"`
}

type RateLimitConfig struct {
	FillFactor int `json:"fillFactor"`
	Burst      int `json:"burst"`
}

type MaxConcurrentConfig struct {
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Optional
	Value int `json:"value,omitempty"`
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Optional
	PerCore int `json:"perCore,omitempty"`
}

func ConvertToPacemakerConfig(un *unstructured.Unstructured) (*PacemakerConfig, error) {
	var config PacemakerConfig
	err := runtime.DefaultUnstructuredConverter.
		FromUnstructured(un.UnstructuredContent(), &config)
	return &config, err
}
