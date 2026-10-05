package provider

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLocalProviderHonorsKubeconfigEnv verifies that NewLocalKubernetesProvider
// builds its REST config from the $KUBECONFIG file (not a hardcoded default
// such as https://127.0.0.1:6443).
func TestLocalProviderHonorsKubeconfigEnv(t *testing.T) {
	dir := t.TempDir()
	kubeconfigPath := filepath.Join(dir, "config")
	content := `apiVersion: v1
kind: Config
clusters:
- name: custom-cluster
  cluster:
    server: https://example-cluster.test:6443
contexts:
- name: custom-context
  context:
    cluster: custom-cluster
    user: custom-user
current-context: custom-context
users:
- name: custom-user
  user:
    token: dummy-token
`
	if err := os.WriteFile(kubeconfigPath, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write temp kubeconfig: %v", err)
	}

	t.Setenv("KUBECONFIG", kubeconfigPath)

	p, err := NewLocalKubernetesProvider()
	if err != nil {
		t.Fatalf("NewLocalKubernetesProvider failed: %v", err)
	}
	if got := p.ClusterHost(); got != "https://example-cluster.test:6443" {
		t.Fatalf("ClusterHost() = %q, want %q (must come from $KUBECONFIG)", got, "https://example-cluster.test:6443")
	}
}

// TestLocalProviderNoHardcodedLocalhost is a guard against regressing to a
// hardcoded localhost API server: when KUBECONFIG points elsewhere, the
// provider must never report a localhost target.
func TestLocalProviderNoHardcodedLocalhost(t *testing.T) {
	dir := t.TempDir()
	kubeconfigPath := filepath.Join(dir, "config")
	content := `apiVersion: v1
kind: Config
clusters:
- name: remote
  cluster:
    server: https://remote-stack8s.example:6443
contexts:
- name: remote
  context:
    cluster: remote
    user: u
current-context: remote
users:
- name: u
  user:
    token: dummy
`
	if err := os.WriteFile(kubeconfigPath, []byte(content), 0600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	t.Setenv("KUBECONFIG", kubeconfigPath)

	p, err := NewLocalKubernetesProvider()
	if err != nil {
		t.Fatalf("NewLocalKubernetesProvider failed: %v", err)
	}
	host := p.ClusterHost()
	if host == "https://127.0.0.1:6443" || host == "https://localhost:6443" {
		t.Fatalf("provider target %q must not be localhost when $KUBECONFIG points elsewhere", host)
	}
}
