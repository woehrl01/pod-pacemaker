package main

import (
	"io"
	"log"
	"os"
	"path/filepath"
)

func main() {
	// Define the source and target paths for the CNI plugin binary
	sourcePath := "/root/cni-plugin"
	targetDir := "/opt/cni/bin"
	targetPath := filepath.Join(targetDir, "pod-startup-limiter")

	// Ensure the target directory exists
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		log.Fatalf("Failed to create target directory: %v", err)
	}

	// Copy the CNI plugin binary to the target location
	if err := copyFile(sourcePath, targetPath); err != nil {
		log.Fatalf("Failed to copy CNI plugin binary: %v", err)
	}

	// Generate the CNI network configuration file
	configPath := "/etc/cni/net.d/10-pod-startup-limiter.conf"
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
	configContent := `
{
  "cniVersion": "0.3.1",
  "name": "pod-startup-limiter",
  "type": "pod-startup-limiter",
  "capabilities": {
	"io.kubernetes.cri.pod-annotations": true
  }
}
`
	return os.WriteFile(filePath, []byte(configContent), 0644)
}
