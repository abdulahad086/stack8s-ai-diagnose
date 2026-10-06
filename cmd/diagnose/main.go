package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/stack8s/stack8s-ai-diagnose/provider"
)

func getProvider(provType string) provider.InfrastructureProvider {
	if provType == "" {
		provType = os.Getenv("STACK8S_PROVIDER")
	}
	if provType == "" {
		provType = "local-kubernetes"
	}

	switch strings.ToLower(provType) {
	case "local-kubernetes", "kubernetes", "k8s":
		var prov *provider.LocalKubernetesProvider
		var err error
		if p := os.Getenv("STACK8S_KUBECONFIG_PATH"); p != "" {
			prov, err = provider.NewLocalKubernetesProviderFromPath(p)
		} else {
			prov, err = provider.NewLocalKubernetesProvider()
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: failed to initialize LocalKubernetesProvider: %v\n", err)
			os.Exit(1)
		}
		return prov
	case "stack8s":
		prov, err := provider.NewStack8sProvider()
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: failed to initialize Stack8sProvider: %v\n", err)
			os.Exit(1)
		}
		return prov
	case "fake", "mock":
		return provider.NewFakeProvider()
	default:
		fmt.Fprintf(os.Stderr, "ERROR: unknown provider %q\n", provType)
		os.Exit(1)
		return nil
	}
}

func main() {
	namespaceFlag := flag.String("namespace", "default", "Kubernetes namespace")
	podFlag := flag.String("pod", "", "Target pod name")
	questionFlag := flag.String("question", "", "User diagnostic question")
	checkpointFlag := flag.String("checkpoint", "checkpoints/needle3.safetensors", "Needle 3 F19 model checkpoint path")
	providerFlag := flag.String("provider", "local-kubernetes", "Infrastructure provider (local-kubernetes, fake, stack8s)")

	flag.Parse()

	if *questionFlag == "" && *podFlag != "" {
		*questionFlag = fmt.Sprintf("Why is pod %s failing in namespace %s?", *podFlag, *namespaceFlag)
	} else if *questionFlag == "" {
		fmt.Fprintf(os.Stderr, "ERROR: --question or --pod flag is required\n")
		os.Exit(1)
	}

	prov := getProvider(*providerFlag)
	fmt.Printf("=== stack8s-ai-diagnose — Real-Time Diagnostic Loop (MVP) ===\n")
	fmt.Printf("  Provider   : %s\n", prov.Name())
	fmt.Printf("  Cluster    : %s\n", prov.ClusterHost())
	fmt.Printf("  Namespace  : %s\n", *namespaceFlag)
	if *podFlag != "" {
		fmt.Printf("  Target Pod : %s\n", *podFlag)
	}
	fmt.Printf("  Question   : %s\n", *questionFlag)
	fmt.Printf("  Model      : %s (F19 Needle 3)\n\n", *checkpointFlag)

	// Verify Python virtualenv and Needle checkpoint exist
	pythonPath := filepath.Join("venv", "bin", "python3")
	if _, err := os.Stat(pythonPath); os.IsNotExist(err) {
		pythonPath = "python3"
	}

	if _, err := os.Stat(*checkpointFlag); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "ERROR: Needle 3 checkpoint not found at %s\n", *checkpointFlag)
		os.Exit(1)
	}

	// Invoke python runtime diagnostic loop script
	scriptCode := fmt.Sprintf(`
import sys, json
sys.path.append(".")
from runtime.diagnostic_loop import NeedleDiagnoser, ToolDispatcherBridge

diagnoser = NeedleDiagnoser(%q)
bridge = ToolDispatcherBridge(provider_type=%q)

res = diagnoser.diagnose(%q, bridge=bridge, verbose=True, namespace=%q, pod=%q)
print("\n" + "="*60)
print(res["final_diagnosis"])
print("="*60)
`, *checkpointFlag, *providerFlag, *questionFlag, *namespaceFlag, *podFlag)

	cmd := exec.Command(pythonPath, "-c", scriptCode)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), fmt.Sprintf("STACK8S_PROVIDER=%s", *providerFlag))

	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "\nERROR: Diagnostic loop execution failed: %v\n", err)
		os.Exit(1)
	}
}
