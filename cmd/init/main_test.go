package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const ownConfig = `{"cniVersion": "0.4.0", "name": "pod-pacemaker", "plugins": [{"name": "pod-pacemaker", "type": "pod-pacemaker"}]}`

func awsConfig(logLevel string) string {
	return `{"cniVersion": "0.4.0", "name": "aws-cni", "plugins": [{"name": "aws-cni", "type": "aws-cni", "pluginLogLevel": "` + logLevel + `"}, {"type": "portmap"}]}`
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestMergeTwoConfigsFollowsPrimary(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, "10-aws.conflist")
	own := filepath.Join(dir, "20-pod-pacemaker.conflist")
	merged := filepath.Join(dir, "00-merged-pod-pacemaker.conflist")
	writeFile(t, primary, awsConfig("DEBUG"))
	writeFile(t, own, ownConfig)

	if changed, err := mergeTwoConfigs(primary, own, merged); err != nil || !changed {
		t.Fatalf("first merge: changed = %v, err = %v, want a new merged file", changed, err)
	}
	if changed, err := mergeTwoConfigs(primary, own, merged); err != nil || changed {
		t.Fatalf("merge without changes: changed = %v, err = %v, want the merged file untouched", changed, err)
	}

	// aws-node rewrites its configuration on start
	writeFile(t, primary, awsConfig("INFO"))
	if changed, err := mergeTwoConfigs(primary, own, merged); err != nil || !changed {
		t.Fatalf("merge after the primary changed: changed = %v, err = %v, want an update", changed, err)
	}

	content, err := os.ReadFile(merged)
	if err != nil {
		t.Fatal(err)
	}
	got := string(content)
	if !strings.Contains(got, `"pluginLogLevel": "INFO"`) || strings.Contains(got, `"DEBUG"`) {
		t.Errorf("merged file does not follow the primary configuration:\n%s", got)
	}
	if strings.Index(got, `"aws-cni"`) > strings.Index(got, `"pod-pacemaker"`) {
		t.Errorf("pod-pacemaker must be chained after the primary plugins:\n%s", got)
	}
	if _, err := os.Stat(merged + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temporary file left behind: %v", err)
	}
}

func TestMergeTwoConfigsKeepsMergedOnBrokenPrimary(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, "10-aws.conflist")
	own := filepath.Join(dir, "20-pod-pacemaker.conflist")
	merged := filepath.Join(dir, "00-merged-pod-pacemaker.conflist")
	writeFile(t, primary, awsConfig("DEBUG"))
	writeFile(t, own, ownConfig)
	if _, err := mergeTwoConfigs(primary, own, merged); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(merged)

	// caught while the primary CNI rewrites its file
	writeFile(t, primary, `{"cniVersion": "0.4.0", "plug`)
	if _, err := mergeTwoConfigs(primary, own, merged); err == nil {
		t.Fatal("merge of a partial primary configuration succeeded, want an error")
	}
	after, _ := os.ReadFile(merged)
	if string(before) != string(after) {
		t.Errorf("merged file changed after a failed merge")
	}
}

// every write in the CNI configuration directory makes containerd reload the network configuration,
// which blocks the CRI Status call while pod network setups wait for a slot; a restart of the init
// container with the same flags must not write
func TestGenerateCNIConfigWritesOnlyOnChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "20-pod-pacemaker.conflist")

	if changed, err := generateCNIConfig(path); err != nil || !changed {
		t.Fatalf("first generation: changed = %v, err = %v, want a new file", changed, err)
	}
	stat, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	before := stat.ModTime()
	time.Sleep(10 * time.Millisecond)

	if changed, err := generateCNIConfig(path); err != nil || changed {
		t.Fatalf("generation with the same flags: changed = %v, err = %v, want the file untouched", changed, err)
	}
	if stat, err = os.Stat(path); err != nil || !stat.ModTime().Equal(before) {
		t.Errorf("file rewritten without a change: mtime %v -> %v, err = %v", before, stat.ModTime(), err)
	}

	saved := *maxWaitTimeInSeconds
	t.Cleanup(func() { *maxWaitTimeInSeconds = saved })
	*maxWaitTimeInSeconds = saved - 1
	if changed, err := generateCNIConfig(path); err != nil || !changed {
		t.Fatalf("generation after a flag change: changed = %v, err = %v, want an update", changed, err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temporary file left behind: %v", err)
	}
}
