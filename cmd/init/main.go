package main

import (
	"encoding/json"
	goflag "flag"
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
	cniBinDir 		  = flag.String("cni-bin-dir", "/opt/cni/bin", "The directory for CNI binaries")
	cniConfigDir 	  = flag.String("cni-config-dir", "/etc/cni/net.d", "The directory for CNI configurations")
)

func main() {
	flag.CommandLine.AddGoFlagSet(goflag.CommandLine)
	if err := flag.Set("logtostderr", "true"); err != nil {
		panic(err)
	}
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
	configPath := fmt.Sprintf("%/10-%s.conf", *cniConfigDir, *cniName)
	if err := generateCNIConfig(configPath); err != nil {
		log.Fatalf("Failed to generate CNI network configuration: %v", err)
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
	config := CniConfig{
		CniVersion: "0.3.1",
		Name:       *cniName,
		Type:       *cniType,
		Capabilities: CniConfigCapabilities{
			PodAnnotations: true,
		},
		DaemonPort:           *daemonPort,
		MaxWaitTimeInSeconds: *maxWaitTimeInSeconds,
	}

	configContent, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filePath, configContent, 0644)
}

type CniConfig struct {
	CniVersion           string                `json:"cniVersion"`
	Name                 string                `json:"name"`
	Type                 string                `json:"type"`
	Capabilities         CniConfigCapabilities `json:"capabilities"`
	DaemonPort           int                   `json:"daemonPort"`
	MaxWaitTimeInSeconds int32                 `json:"maxWaitTimeInSeconds"`
}

type CniConfigCapabilities struct {
	PodAnnotations bool `json:"io.kubernetes.cri.pod-annotations"`
}
