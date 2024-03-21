package main

import (
	"context"
	"os"
	"time"

	flag "github.com/spf13/pflag"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	log "github.com/sirupsen/logrus"
)

var (
	throttlerLimit = flag.Int("throttler-limit", 10, "The maximum number of pods that can start at the same time")
	taintToRemove  = flag.String("taint-to-remove", "pod-limiter", "The taint to remove from the node")
)

func main() {
	flag.Parse()

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

	throttler := NewThrottler(*throttlerLimit)

	startPodHandler(ctx, clientset, throttler, stopper, nodeName)
	removeStartupTaint(clientset, nodeName)
	startGrpcServer(throttler)

	<-stopper
}

func startPodHandler(ctx context.Context, clientset *kubernetes.Clientset, throttler Throttler, stopper chan struct{}, nodeName string) {
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
		log.Println("No taint 'pod-limiter' found on node, no update required.")
		return
	}

	node.Spec.Taints = newTaints

	_, err = clientset.CoreV1().Nodes().Update(context.Background(), node, metav1.UpdateOptions{})
	if err != nil {
		log.Fatalf("Failed to update node %s: %v", nodeName, err)
	}
}
