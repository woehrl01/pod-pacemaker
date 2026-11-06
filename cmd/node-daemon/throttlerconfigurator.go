package main

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
	"woehrl01/pod-pacemaker/api/v1alpha"
	"woehrl01/pod-pacemaker/pkg/throttler"

	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/retry"
)

// apiBackoff makes 6 attempts at a failed API call, about 30s apart in total (1s, 2s, ... 16s)
var apiBackoff = wait.Backoff{Steps: 6, Duration: time.Second, Factor: 2, Jitter: 0.1}

type throttlerConfigurator struct {
	informers           cache.SharedIndexInformer
	clientset           *kubernetes.Clientset
	currentCloseChannel chan struct{}
	lock                sync.Mutex
	nodeName            string
	dynamicThrottlers   throttler.DynamicThrottler
	configured          bool // the throttlers were set at least once
}

func NewThrottlerConfigurator(informer cache.SharedIndexInformer, clientSet *kubernetes.Clientset, nodeName string, dynamicThrottler throttler.DynamicThrottler) *throttlerConfigurator {
	return &throttlerConfigurator{
		informers:           informer,
		clientset:           clientSet,
		currentCloseChannel: make(chan struct{}),
		nodeName:            nodeName,
		dynamicThrottlers:   dynamicThrottler,
	}
}

func (t *throttlerConfigurator) Updatethrottlers() {
	t.lock.Lock()
	defer t.lock.Unlock()

	matchingConfig, err := t.getMatchingConfig()
	if err != nil {
		if !t.configured {
			// nothing to lose yet, and without a restart the node would stay unthrottled until the next change
			log.Fatalf("Failed to select the config: %v", err)
		}
		// a crash would lose the slots held right now, so the current throttlers stay until the next change
		log.Errorf("Failed to select the config, keeping the current throttlers: %v", err)
		return
	}
	t.configured = true

	close(t.currentCloseChannel) // close the current throttlers
	t.currentCloseChannel = make(chan struct{})

	if matchingConfig == nil {
		log.Infof("No matching config found")
		t.dynamicThrottlers.SetThrottlers([]throttler.Throttler{})
		return
	}

	throttlers := []throttler.Throttler{}
	// rate limit first
	if matchingConfig.Spec.ThrottleConfig.RateLimit.FillFactor != "" && matchingConfig.Spec.ThrottleConfig.RateLimit.Burst > 0 {
		throttlers = append(throttlers, throttler.NewRateLimitThrottler(
			matchingConfig.Spec.ThrottleConfig.RateLimit.FillFactor,
			matchingConfig.Spec.ThrottleConfig.RateLimit.Burst,
		))
	}

	// then max concurrent
	if matchingConfig.Spec.ThrottleConfig.MaxConcurrent.Value > 0 || matchingConfig.Spec.ThrottleConfig.MaxConcurrent.PerCore != "" {
		throttlers = append(throttlers, throttler.NewDynamicConcurrencyThrottler(
			matchingConfig.Spec.ThrottleConfig.MaxConcurrent.Value,
			matchingConfig.Spec.ThrottleConfig.MaxConcurrent.PerCore,
		))
	}

	// then load average
	if matchingConfig.Spec.ThrottleConfig.LoadAvg.MaxLoad != "" {
		throttlers = append(throttlers, throttler.NewConcurrencyControllerBasedOnLoadAvg(
			matchingConfig.Spec.ThrottleConfig.LoadAvg.MaxLoad,
			matchingConfig.Spec.ThrottleConfig.LoadAvg.PerCore,
			matchingConfig.Spec.ThrottleConfig.LoadAvg.IncrementBy,
			t.currentCloseChannel,
		))
	}

	// then CPU load
	if matchingConfig.Spec.ThrottleConfig.Cpu.MaxLoad != "" {
		throttlers = append(throttlers, throttler.NewConcurrencyControllerBasedOnCpu(
			matchingConfig.Spec.ThrottleConfig.Cpu.MaxLoad,
			matchingConfig.Spec.ThrottleConfig.Cpu.IncrementBy,
			t.currentCloseChannel,
		))
	}

	// then I/O load
	if matchingConfig.Spec.ThrottleConfig.IO.MaxLoad != "" {
		throttlers = append(throttlers, throttler.NewConcurrencyControllerBasedOnIOLoad(
			matchingConfig.Spec.ThrottleConfig.IO.MaxLoad,
			matchingConfig.Spec.ThrottleConfig.IO.IncrementBy,
			t.currentCloseChannel,
		))
	}

	if len(throttlers) == 0 {
		log.Infof("No throttlers found")
	}

	for _, t := range throttlers {
		log.Infof("Throttler is active: %s", t)
	}

	t.dynamicThrottlers.SetThrottlers(throttlers)
}

func (t *throttlerConfigurator) getMatchingConfig() (*v1alpha.PacemakerConfig, error) {
	allConfigsUnstructured := t.informers.GetStore().List()

	allConfigs := make([]*v1alpha.PacemakerConfig, 0, len(allConfigsUnstructured))
	for _, config := range allConfigsUnstructured {
		unstructured := config.(*unstructured.Unstructured)
		config, err := v1alpha.ConvertToPacemakerConfig(unstructured)
		if err != nil {
			// one broken config must not take down the daemon on every node
			log.Errorf("Skipping config %s, failed to convert it: %v", unstructured.GetName(), err)
			continue
		}
		allConfigs = append(allConfigs, config)
	}

	var node *v1.Node
	err := retry.OnError(apiBackoff, func(error) bool { return true }, func() error {
		var err error
		node, err = t.clientset.CoreV1().Nodes().Get(context.Background(), t.nodeName, metav1.GetOptions{})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get node %s: %w", t.nodeName, err)
	}

	sort.Slice(allConfigs, func(i, j int) bool {
		a := allConfigs[i]
		b := allConfigs[j]
		return a.Spec.Priority > b.Spec.Priority
	})

	var matchingConfig *v1alpha.PacemakerConfig
	for _, config := range allConfigs {
		c := config
		labelSelector := labels.Set(c.Spec.NodeSelector).AsSelector()
		if !labelSelector.Matches(labels.Set(node.Labels)) {
			log.Debugf("Label selector %s does not match node labels %s", labelSelector, node.Labels)
			continue
		}
		log.Infof("Config %s matches node labels", c.Name)
		matchingConfig = c
		break // we only need the highest priority config which matches
	}
	return matchingConfig, nil
}
