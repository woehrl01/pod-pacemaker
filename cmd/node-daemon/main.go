package main

import (
	"context"
	"os"
	"time"

	"woehrl01/pod-pacemaker/api/v1alpha"
	"woehrl01/pod-pacemaker/pkg/throttler"

	flag "github.com/spf13/pflag"
	"golang.org/x/time/rate"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	log "github.com/sirupsen/logrus"
)

var (
	taintToRemove = flag.String("taint-to-remove", "pod-pacemaker", "The taint to remove from the node")
	daemonPort    = flag.Int("daemon-port", 50051, "The port for the node daemon")
	debugLogging  = flag.Bool("debug-logging", false, "Enable debug logging")
)

func main() {
	flag.Parse()
	if *debugLogging {
		log.SetLevel(log.DebugLevel)
	}

	config, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("Failed to get kubernetes config: %v", err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Failed to create kubernetes client: %v", err)
	}
	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		log.Fatal("NODE_NAME environment variable not set")
	}

	ctx := context.Background()

	stopper := make(chan struct{})
	defer close(stopper)

	dynamicThrottlers := throttler.NewDynamicThrottler()

	throttler := throttler.NewAllThrottler(dynamicThrottlers)

	startPodHandler(ctx, clientset, throttler, stopper, nodeName)
	startConfigHandler(config, dynamicThrottlers, stopper)
	removeStartupTaint(clientset, nodeName)
	startGrpcServer(throttler, *daemonPort)

	<-stopper
}

func startPodHandler(ctx context.Context, clientset *kubernetes.Clientset, throttler throttler.Throttler, stopper chan struct{}, nodeName string) {
	factory := informers.NewSharedInformerFactoryWithOptions(clientset, time.Second*30, informers.WithTweakListOptions(func(options *metav1.ListOptions) {
		options.FieldSelector = fields.OneTermEqualSelector("spec.nodeName", nodeName).String()
	}))

	podInformer := factory.Core().V1().Pods()

	podEventHandler := NewPodEventHandler(throttler, ctx)

	informer := podInformer.Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			podEventHandler.OnAdd(obj.(*v1.Pod))
		},
		DeleteFunc: func(obj interface{}) {
			podEventHandler.OnDelete(obj.(*v1.Pod))
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			podEventHandler.OnAdd(newObj.(*v1.Pod))
		},
	})

	go informer.Run(stopper)

	if !cache.WaitForCacheSync(stopper, informer.HasSynced) {
		log.Fatal("Failed to sync")
	}
}

func startConfigHandler(config *rest.Config, dynamicThrottlers throttler.DynamicThrottler, stopper chan struct{}) {
	gvr := schema.GroupVersionResource{
		Group:    "woehrl.net",
		Version:  "v1alpha",
		Resource: "pacemakerconfigs",
	}

	dynClient, err := dynamic.NewForConfig(config)
	if err != nil {
		panic(err)
	}

	dynamicInformerFactory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(dynClient, time.Minute, metav1.NamespaceAll, nil)
	informers := dynamicInformerFactory.ForResource(gvr).Informer()

	updateAllThrottlers := func() {
		allConfigs := informers.GetStore().List()
		var matchingConfig *v1alpha.PacemakerConfig
		for _, config := range allConfigs {
			config := config.(*v1alpha.PacemakerConfig)

			// only select the config for the current node
			matchingConfig = config
			break
		}

		throttlers := make([]throttler.Throttler, 0, len(allConfigs))
		if matchingConfig == nil {
			log.Debug("No matching config found")
			dynamicThrottlers.SetThrottlers(throttlers)
			return
		}

		// rate limit first
		if matchingConfig.Spec.ThrottleConfig.RateLimit.FillFactor > 0 && matchingConfig.Spec.ThrottleConfig.RateLimit.Burst > 0 {
			throttlers = append(throttlers, throttler.NewRateLimitThrottler(rate.Every(time.Second/time.Duration(matchingConfig.Spec.ThrottleConfig.RateLimit.FillFactor)), matchingConfig.Spec.ThrottleConfig.RateLimit.Burst))
		}

		// then max concurrent
		if matchingConfig.Spec.ThrottleConfig.MaxConcurrent.Value > 0 || matchingConfig.Spec.ThrottleConfig.MaxConcurrent.PerCore > 0 {
			throttlers = append(throttlers, throttler.NewPriorityThrottler(matchingConfig.Spec.ThrottleConfig.MaxConcurrent.Value, matchingConfig.Spec.ThrottleConfig.MaxConcurrent.PerCore))
		}

		// then CPU load
		if matchingConfig.Spec.ThrottleConfig.CpuThreshold > 0 {
			throttlers = append(throttlers, throttler.NewConcurrencyControllerBasedOnCpu(float64(matchingConfig.Spec.ThrottleConfig.CpuThreshold)))
		}

		// then I/O load
		if matchingConfig.Spec.ThrottleConfig.MaxIOLoad > 0 {
			throttlers = append(throttlers, throttler.NewConcurrencyControllerBasedOnIOLoad((float64(matchingConfig.Spec.ThrottleConfig.MaxIOLoad))))
		}

		// print debug output which throttlers are active
		for _, t := range throttlers {
			log.Debugf("Throttler %s is active", t)
		}

		dynamicThrottlers.SetThrottlers(throttlers)
	}

	informers.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			updateAllThrottlers()
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			updateAllThrottlers()
		},
		DeleteFunc: func(obj interface{}) {
			updateAllThrottlers()
		},
	})

	go informers.Run(stopper)

	//wait for the initial synchronization of the local cache
	if !cache.WaitForCacheSync(stopper, informers.HasSynced) {
		panic("Failed to sync")
	}
}

func removeStartupTaint(clientset *kubernetes.Clientset, nodeName string) {
	if *taintToRemove == "" {
		log.Println("No taint to remove, no update required.")
		return
	}

	node, err := clientset.CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
	if err != nil {
		log.Fatalf("Failed to get node %s: %v", nodeName, err)
	}

	newTaints := []v1.Taint{}
	for _, taint := range node.Spec.Taints {
		if taint.Key != *taintToRemove {
			newTaints = append(newTaints, taint)
		}
	}

	if len(newTaints) == len(node.Spec.Taints) {
		log.Println("No taint 'pod-pacemaker' found on node, no update required.")
		return
	}

	node.Spec.Taints = newTaints

	_, err = clientset.CoreV1().Nodes().Update(context.Background(), node, metav1.UpdateOptions{})
	if err != nil {
		log.Fatalf("Failed to update node %s: %v", nodeName, err)
	}
}
