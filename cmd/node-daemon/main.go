package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"woehrl01/pod-pacemaker/pkg/podaccessor"
	"woehrl01/pod-pacemaker/pkg/throttler"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	flag "github.com/spf13/pflag"

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
	taintToRemove         = flag.String("taint-to-remove", "pod-pacemaker", "The taint to remove from the node")
	debugLogging          = flag.Bool("debug-logging", false, "Enable debug logging")
	daemonSocket          = flag.String("daemon-socket", "/var/run/pod-pacemaker/pod-pacemaker.sock", "The socket for the daemon")
	skipDaemonSets        = flag.Bool("skip-daemonsets", true, "Skip throttling of daemonsets")
	metricsPort           = flag.Int("metrics-port", 9000, "The port for the metrics server")
	metricsEnabled        = flag.Bool("metrics-enabled", true, "Enable the metrics server")
	trackInflightRequests = flag.Bool("track-inflight-requests", false, "Track inflight requests")
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

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	dynamicThrottlers := throttler.NewDynamicThrottler()

	throttler := throttler.NewAllThrottler(dynamicThrottlers)

	podAccessor := startPodHandler(ctx, clientset, throttler, nodeName, ctx.Done())
	startConfigHandler(config, dynamicThrottlers, nodeName, ctx.Done())
	removeStartupTaint(clientset, nodeName)

	wg := sync.WaitGroup{}
	if *metricsEnabled {
		wg.Add(1)
		go func() {
			defer wg.Done()
			startPrometheusMetricsServer(ctx.Done())
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		startGrpcServer(throttler, Options{
			Socket:                *daemonSocket,
			SkipDaemonSets:        *skipDaemonSets,
			TrackInflightRequests: *trackInflightRequests,
		}, podAccessor, ctx.Done())
		wg.Done()
	}()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	defer func() {
		signal.Stop(c)
		cancel()
	}()
	go func() {
		select {
		case <-c:
			cancel()
		case <-ctx.Done():
		}
	}()

	log.Infof("Node daemon started on node %s", nodeName)

	wg.Wait()
}

func startPodHandler(ctx context.Context, clientset *kubernetes.Clientset, throttler throttler.Throttler, nodeName string, stopper <-chan struct{}) podaccessor.PodAccessor {
	factory := informers.NewSharedInformerFactoryWithOptions(clientset, time.Second*30, informers.WithTweakListOptions(func(options *metav1.ListOptions) {
		options.FieldSelector = fields.OneTermEqualSelector("spec.nodeName", nodeName).String()
	}))

	podInformer := factory.Core().V1().Pods()
	podEventHandler := NewPodEventHandler(throttler, ctx)

	removeOutdated := func() {
		list := podInformer.Informer().GetIndexer().List()
		currentPods := make([]*v1.Pod, len(list))
		for _, pod := range list {
			p := pod.(*v1.Pod)
			if p == nil {
				continue
			}
			currentPods = append(currentPods, p)
		}
		podEventHandler.RemoveOutdatedSlots(currentPods)
	}

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

	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				removeOutdated()
			case <-stopper:
				return
			}
		}
	}()

	return podaccessor.NewLocalPodsAccessor(informer.GetIndexer())
}

func startConfigHandler(config *rest.Config, dynamicThrottlers throttler.DynamicThrottler, nodeName string, stopper <-chan struct{}) {
	gvr := schema.GroupVersionResource{
		Group:    "woehrl.net",
		Version:  "v1alpha",
		Resource: "pacemakerconfigs",
	}

	dynClient := dynamic.NewForConfigOrDie(config)

	dynamicInformerFactory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(dynClient, 0 /*no resync*/, metav1.NamespaceAll, nil)
	informers := dynamicInformerFactory.ForResource(gvr).Informer()

	clientset := kubernetes.NewForConfigOrDie(config)

	handler := NewThrottlerConfigurator(informers, clientset, nodeName, dynamicThrottlers)

	informers.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			handler.Updatethrottlers()
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			handler.Updatethrottlers()
		},
		DeleteFunc: func(obj interface{}) {
			handler.Updatethrottlers()
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

func startPrometheusMetricsServer(stopper <-chan struct{}) {
	srv := &http.Server{
		Addr: fmt.Sprintf(":%d", *metricsPort),
	}

	http.Handle("/metrics", promhttp.Handler())

	go func() {
		<-stopper
		srv.Shutdown(context.Background())
	}()

	log.Fatal(srv.ListenAndServe(), nil)
}
