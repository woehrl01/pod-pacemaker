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
	// Configures a rate limiting strategy for concurrent pod starts
	RateLimit RateLimitConfig `json:"rateLimit"`
	// +kubebuilder:validation:Optional
	// Configures a limitting strategy based on the maximum number of concurrent pod starts
	MaxConcurrent MaxConcurrentConfig `json:"maxConcurrent"`
	// +kubebuilder:validation:Optional
	// Configures a limitting strategy based on the CPU load of the node
	Cpu Cpu `json:"cpu"`
	// +kubebuilder:validation:Optional
	// Configures a limitting strategy based on the IO load of the node
	IO IO `json:"io"`
}

type RateLimitConfig struct {
	// +kubebuilder:validation:Pattern=`^[0-9]+(µs|ns|ms|s|m|h)$`
	// Sets the fill factor of the rate limiter in time.Duration format (e.g. "100ms" for 10 requests per second)
	FillFactor string `json:"fillFactor"`
	// +kubebuilder:validation:Minimum=1
	// Sets the maximum number of requests that can be made in a given time frame
	Burst int `json:"burst"`
}

type Cpu struct {
	// +kubebuilder:validation:Pattern=`^[0-9]+(\.[0-9]+)?$`
	// Sets the limit of CPU load percentage that should not be exceeded
	MaxLoad string `json:"maxLoad"`
	// +kubebuilder:validation:Pattern=`^[0-9]+(\.[0-9]+)?$`
	// +kubebuilder:validation:Optional
	// Sets the increment by which the CPU load will be increased by a starting pod until the next measurement refresh
	IncrementBy string `json:"incrementBy"`
}

type IO struct {
	// +kubebuilder:validation:Pattern=`^[0-9]+(\.[0-9]+)?$`
	// Sets the limit of IO load percentage that should not be exceeded
	MaxLoad string `json:"maxLoad"`
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Pattern=`^[0-9]+(\.[0-9]+)?$`
	// Sets the increment by which the IO load will be increased by a starting pod until the next measurement refresh
	IncrementBy string `json:"incrementBy"`
}

type MaxConcurrentConfig struct {
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Optional
	// Sets the maximum number of concurrent pod starts in total. Has precedence over perCore
	Value int `json:"value,omitempty"`
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Pattern=`^[0-9]+(\.[0-9]+)?$`
	// Sets the maximum number of concurrent pod starts per CPU core
	PerCore string `json:"perCore,omitempty"`
}

func ConvertToPacemakerConfig(un *unstructured.Unstructured) (*PacemakerConfig, error) {
	var config PacemakerConfig
	err := runtime.DefaultUnstructuredConverter.
		FromUnstructured(un.UnstructuredContent(), &config)
	return &config, err
}
