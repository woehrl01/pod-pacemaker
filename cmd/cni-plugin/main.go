package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"slices"
	"time"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	current "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/cni/pkg/version"
	bv "github.com/containernetworking/plugins/pkg/utils/buildversion"
	"github.com/sirupsen/logrus"
)

type PluginConf struct {
	types.NetConf

	RuntimeConfig *struct {
		PodAnnotations map[string]string `json:"io.kubernetes.cri.pod-annotations"`
	} `json:"runtimeConfig"`

	DaemonSocketPath           string   `json:"daemonSocketPath"`
	MaxWaitTimeInSeconds       int32    `json:"maxWaitTimeInSeconds"`
	NamespaceExclusions        []string `json:"namespaceExclusions"`
	SuccessOnConnectionTimeout bool     `json:"successOnConnectionTimeout"`
	DisableThrottling          bool     `json:"disableThrottling"`
}

type K8sArgs struct {
	types.CommonArgs

	// K8S_POD_NAME is pod's name
	K8S_POD_NAME types.UnmarshallableString

	// K8S_POD_NAMESPACE is pod's namespace
	K8S_POD_NAMESPACE types.UnmarshallableString
}

// parseConfig parses the supplied configuration (and prevResult) from stdin.
func parseConfig(stdin []byte) (*PluginConf, error) {
	conf := PluginConf{}

	if err := json.Unmarshal(stdin, &conf); err != nil {
		return nil, fmt.Errorf("failed to parse network configuration: %v", err)
	}

	if err := version.ParsePrevResult(&conf.NetConf); err != nil {
		return nil, fmt.Errorf("could not parse prevResult: %v", err)
	}

	if len(conf.DaemonSocketPath) == 0 {
		return nil, fmt.Errorf("daemonSocketPath must be set")
	}

	return &conf, nil
}

func main() {
	skel.PluginMainFuncs(skel.CNIFuncs{
		Add:   cmdAdd,
		Del:   cmdDel,
		Check: cmdCheck,
	}, version.All, bv.BuildString("pod-pacemaker"))
}

func setupLogging() error {
	filename := "/var/log/pod-pacemaker.log"
	f, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0755)
	if err != nil {
		return err
	}
	logrus.SetOutput(f)
	return nil
}

// cmdAdd is called for ADD requests
func cmdAdd(args *skel.CmdArgs) error {
	if err := setupLogging(); err != nil {
		return err
	}

	logrus.Infof("Received CNI ADD request with args: %v", args.Args)

	conf, err := parseConfig(args.StdinData)
	if err != nil {
		logrus.Errorf("Failed to parse config: %v", err)
		return err
	}

	result, err := callChain(conf)
	if err != nil {
		logrus.Errorf("Failed to call previous plugin: %v", err)
		return err
	}

	if conf.DisableThrottling {
		logrus.Infof("Throttling disabled")
		return types.PrintResult(result, conf.CNIVersion)
	}

	var k8sArgs K8sArgs
	if err := types.LoadArgs(args.Args, &k8sArgs); err != nil {
		logrus.Errorf("Failed to load K8s args: %v", err)
		return err
	}

	if shouldSkipThrotteling(conf, &k8sArgs) {
		logrus.Infof("Skipping throttling for %s/%s", string(k8sArgs.K8S_POD_NAMESPACE), string(k8sArgs.K8S_POD_NAME))
		return types.PrintResult(result, conf.CNIVersion)
	}

	slotName := fmt.Sprintf("%s/%s", string(k8sArgs.K8S_POD_NAMESPACE), string(k8sArgs.K8S_POD_NAME))
	logrus.Infof("Waiting for slot %s", slotName)

	ctx, totalRequestCancel := context.WithTimeout(context.Background(), time.Second*time.Duration(conf.MaxWaitTimeInSeconds))
	defer totalRequestCancel()

	retries := 5
	for {
		if err := WaitForSlot(ctx, slotName, conf); err != nil {
			if ctx.Err() != nil && isConnectionError(err) && retries > 0 {
				logrus.Warnf("Failed to connect to daemon, retrying: %v", err)
				retries--
				// random backoff
				backoff := time.Duration(rand.Intn(5)) * time.Second
				time.Sleep(backoff)
				continue
			}
			logrus.Errorf("Failed to acquire slot %s: %v", slotName, err)
			return err
		}
		break
	}
	logrus.Infof("Acquired slot %s", slotName)
	return types.PrintResult(result, conf.CNIVersion)
}

func cmdDel(args *skel.CmdArgs) error {
	return nil
}

func cmdCheck(_ *skel.CmdArgs) error {
	return nil
}

func callChain(conf *PluginConf) (*current.Result, error) {
	if conf.PrevResult == nil {
		return nil, fmt.Errorf("must be called as chained plugin")
	}

	prevResult, err := current.GetResult(conf.PrevResult)
	if err != nil {
		return nil, fmt.Errorf("failed to convert prevResult: %v", err)
	}
	return prevResult, nil
}

func shouldSkipThrotteling(conf *PluginConf, k8sArgs *K8sArgs) bool {
	if v, ok := conf.RuntimeConfig.PodAnnotations["pod-pacemaker/skip"]; ok {
		return v == "true"
	}

	if slices.Contains(conf.NamespaceExclusions, string(k8sArgs.K8S_POD_NAMESPACE)) {
		return true
	}

	return false
}
