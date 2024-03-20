package main

import (
	"context"
	"os"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	log "github.com/sirupsen/logrus"
)

func main() {
	ctx := context.Background()
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

	// create the pod watcher
	factory := informers.NewSharedInformerFactoryWithOptions(clientset, time.Second*30, informers.WithTweakListOptions(func(options *metav1.ListOptions) {
		options.FieldSelector = fields.OneTermEqualSelector("spec.nodeName", nodeName).String()
	}))

	podInformer := factory.Core().V1().Pods()

	throttler := NewThrottler(10)

	informer := podInformer.Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			handlePod(ctx, throttler, obj.(*v1.Pod))
		},
		DeleteFunc: func(obj interface{}) {
			handlePod(ctx, throttler, obj.(*v1.Pod))
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			handlePod(ctx, throttler, newObj.(*v1.Pod))
		},
	})

	stopper := make(chan struct{})
	defer close(stopper)
	go informer.Run(stopper)

	// wait for the initial synchronization of the local cache
	if !cache.WaitForCacheSync(stopper, informer.HasSynced) {
		log.Fatal("Failed to sync")
	}

	// remove the taint from the node
	removeStartupTaint(clientset, nodeName)

	startGrpcServer(throttler)

	// wait forever
	<-stopper
}

func handlePod(ctx context.Context, throttler Throttler, pod *v1.Pod) {
	for _, containerStatus := range pod.Status.ContainerStatuses {
		if containerStatus.Started != nil && *containerStatus.Started {
			throttler.ReleaseSlot(ctx, containerStatus.ContainerID)
		} else {
			throttler.FillSlot(ctx, containerStatus.ContainerID)
		}
	}
}

func removeStartupTaint(clientset *kubernetes.Clientset, nodeName string) {
	// Get the node object
	node, err := clientset.CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
	if err != nil {
		log.Fatalf("Failed to get node %s: %v", nodeName, err)
	}

	// Remove the taint
	newTaints := []v1.Taint{}
	for _, taint := range node.Spec.Taints {
		if taint.Key != "pod-limiter" {
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
