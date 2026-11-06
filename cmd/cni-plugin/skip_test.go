package main

import "testing"

func TestShouldSkipThrottelingWithoutRuntimeConfig(t *testing.T) {
	conf := &PluginConf{NamespaceExclusions: []string{"kube-system"}}
	if shouldSkipThrotteling(conf, &K8sArgs{K8S_POD_NAMESPACE: "default"}) {
		t.Error("pod in a throttled namespace skipped")
	}
	if !shouldSkipThrotteling(conf, &K8sArgs{K8S_POD_NAMESPACE: "kube-system"}) {
		t.Error("pod in an excluded namespace throttled")
	}
}
