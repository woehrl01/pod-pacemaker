package main

import (
	"encoding/json"
	"fmt"

	flag "github.com/spf13/pflag"

	"io"
	"log"
	"os"
	"path/filepath"
)

var (
	cniName              = flag.String("cni-name", "pod-startup-limiter", "The name of the CNI plugin")
	cniType              = flag.String("cni-type", "pod-startup-limiter", "The type of the CNI plugin")
	daemonPort           = flag.Int("daemon-port", 50051, "The port for the node daemon")
	maxWaitTimeInSeconds = flag.Int32("max-wait-time-in-seconds", 10, "The maximum wait time in seconds")
	cniBinDir            = flag.String("cni-bin-dir", "/opt/cni/bin", "The directory for CNI binaries")
	cniConfigDir         = flag.String("cni-config-dir", "/etc/cni/net.d", "The directory for CNI configurations")
	primaryConfigName    = flag.String("primary-config-name", "10-aws.conflist", "The name of the primary CNI configuration file")
	mergedConfigName     = flag.String("merged-config-name", "00-merged-pod-startup-limiter.conflist", "The name of the merged CNI configuration file")
)

func main() {
	flag.Parse()

	// Define the source and target paths for the CNI plugin binary
	sourcePath := "/root/cni-plugin"
	targetDir := *cniBinDir
	targetPath := filepath.Join(targetDir, *cniName)

	// Ensure the target directory exists
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		log.Fatalf("Failed to create target directory: %v", err)
	}

	// Copy the CNI plugin binary to the target location
	if err := copyFile(sourcePath, targetPath); err != nil {
		log.Fatalf("Failed to copy CNI plugin binary: %v", err)
	}

	// Generate the CNI network configuration file
	configPath := fmt.Sprintf("%s/20-%s.conflist", *cniConfigDir, *cniName)
	if err := generateCNIConfig(configPath); err != nil {
		log.Fatalf("Failed to generate CNI network configuration: %v", err)
	}

	// Merge the CNI network configuration file with the existing configuration
	// This is necessary to ensure that the CNI plugin is added to the list of plugins
	// that are executed when a pod is started
	primaryConfigPath := fmt.Sprintf("%s/%s", *cniConfigDir, *primaryConfigName)
	mergedConfigPath := fmt.Sprintf("%s/%s", *cniConfigDir, *mergedConfigName)
	if err := mergeTwoConfigs(configPath, primaryConfigPath, mergedConfigPath); err != nil {
		log.Fatalf("Failed to merge CNI network configuration: %v", err)
	}

	log.Println("CNI plugin setup completed successfully.")
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	sourceFileStat, err := os.Stat(src)
	if err != nil {
		return err
	}

	if !sourceFileStat.Mode().IsRegular() {
		return os.ErrInvalid
	}

	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	destination, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destination.Close()
	if _, err := io.Copy(destination, source); err != nil {
		return err
	}

	return os.Chmod(dst, sourceFileStat.Mode())
}

// generateCNIConfig creates a CNI network configuration file
func generateCNIConfig(filePath string) error {
	config := CniConfigList{
		CniVersion:   "0.4.0",
		Name:         *cniName,
		DisableCheck: true,
		Plugins: []CniPlugin{
			{
				Name: *cniName,
				Type: *cniType,
				Capabilities: CniConfigCapabilities{
					PodAnnotations: true,
				},
				DaemonPort:           *daemonPort,
				MaxWaitTimeInSeconds: *maxWaitTimeInSeconds,
			},
		},
	}

	configContent, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filePath, configContent, 0644)
}

type CniConfigList struct {
	CniVersion   string      `json:"cniVersion"`
	Name         string      `json:"name"`
	DisableCheck bool        `json:"disableCheck"`
	Plugins      []CniPlugin `json:"plugins"`
}

type CniPlugin struct {
	Name                 string                `json:"name"`
	Type                 string                `json:"type"`
	Capabilities         CniConfigCapabilities `json:"capabilities"`
	DaemonPort           int                   `json:"daemonPort"`
	MaxWaitTimeInSeconds int32                 `json:"maxWaitTimeInSeconds"`
}

type CniConfigCapabilities struct {
	PodAnnotations bool `json:"io.kubernetes.cri.pod-annotations"`
}

func mergeTwoConfigs(configFile1, configFile2, outputFile string) error {
	// Read the contents of the two configuration files
	config1, err := os.ReadFile(configFile1)
	if err != nil {
		return err
	}

	config2, err := os.ReadFile(configFile2)
	if err != nil {
		return err
	}

	// decode the JSON data
	var config1Data map[string]interface{}
	var config2Data map[string]interface{}
	if err := json.Unmarshal(config1, &config1Data); err != nil {
		return err
	}
	if err := json.Unmarshal(config2, &config2Data); err != nil {
		return err
	}

	// check if both have "plugins" key
	if _, ok := config1Data["plugins"]; !ok {
		return fmt.Errorf("config1 does not have 'plugins' key")
	}

	if _, ok := config2Data["plugins"]; !ok {
		return fmt.Errorf("config2 does not have 'plugins' key")
	}

	// merge the "plugins" key
	plugins1 := config1Data["plugins"].([]interface{})
	plugins2 := config2Data["plugins"].([]interface{})
	mergedPlugins := append(plugins1, plugins2...)

	// update the "plugins" key
	config1Data["plugins"] = mergedPlugins

	// encode the JSON data
	mergedConfig, err := json.MarshalIndent(config1Data, "", "  ")
	if err != nil {
		return err
	}

	// write the merged configuration to the output file
	if err := os.WriteFile(outputFile, mergedConfig, 0644); err != nil {
		return err
	}

	return nil
}
