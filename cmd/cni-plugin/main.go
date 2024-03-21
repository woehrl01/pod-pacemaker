// Copyright 2017 CNI authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// This is a sample chained plugin that supports multiple CNI versions. It
// parses prevResult according to the cniVersion
package main

import (
	"encoding/json"
	"fmt"
	"os"

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

	DaemonPort           int32 `json:"daemonPort"`
	MaxWaitTimeInSeconds int32 `json:"maxWaitTimeInSeconds"`
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

	if conf.DaemonPort == 0 {
		return nil, fmt.Errorf("daemonPort must be set")
	}

	return &conf, nil
}

func main() {
	skel.PluginMain(cmdAdd, cmdCheck, cmdDel, version.All, bv.BuildString("pod-startup-limiter"))
}

func setupLogging() error {
	filename := "/var/log/pod-startup-limiter.log"
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
	err = WaitForSlot(slotName, conf)
	if err != nil {
		logrus.Errorf("Failed to acquire slot %s: %v", slotName, err)
		return err
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
	if v, ok := conf.RuntimeConfig.PodAnnotations["woehrl.net/skip-throttle"]; ok {
		return v == "true"
	}

	if string(k8sArgs.K8S_POD_NAMESPACE) == "kube-system" {
		return true
	}

	return false
}
