package podaccessor

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
)

type PodAccessor interface {
	GetPodByKey(key string) (*v1.Pod, error)
}

type localPodsAccessor struct {
	podLister cache.Indexer
}

func NewLocalPodsAccessor(podLister cache.Indexer) PodAccessor {
	return &localPodsAccessor{
		podLister: podLister,
	}
}

func (l *localPodsAccessor) GetPodByKey(key string) (*v1.Pod, error) {
	obj, exists, err := l.podLister.GetByKey(key)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}
	return obj.(*v1.Pod), nil
}
