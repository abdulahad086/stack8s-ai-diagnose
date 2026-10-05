package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/stack8s/stack8s-ai-diagnose/provider"
)

func getProvider() provider.InfrastructureProvider {
	providerType := os.Getenv("STACK8S_PROVIDER")
	if providerType == "" {
		providerType = "local-kubernetes"
	}

	switch strings.ToLower(providerType) {
	case "local-kubernetes", "kubernetes", "k8s":
		prov, err := provider.NewLocalKubernetesProvider()
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
		fmt.Fprintf(os.Stderr, "ERROR: unknown provider %q\n", providerType)
		os.Exit(1)
		return nil
	}
}

func main() {
	toolName := flag.String("tool", "", "Tool name to execute")
	argsJSON := flag.String("args", "{}", "JSON arguments object")
	printTarget := flag.Bool("print-target", false, "Print provider name and cluster host (no credentials), then exit")
	flag.Parse()

	prov := getProvider()

	if *printTarget {
		out, _ := json.Marshal(map[string]string{
			"provider":     prov.Name(),
			"cluster_host": prov.ClusterHost(),
		})
		fmt.Println(string(out))
		return
	}

	if *toolName == "" {
		fmt.Fprintf(os.Stderr, "ERROR: --tool flag is required\n")
		os.Exit(1)
	}

	var args map[string]any
	if err := json.Unmarshal([]byte(*argsJSON), &args); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: invalid JSON args: %v\n", err)
		os.Exit(1)
	}

	call := provider.ToolCall{
		Name:      *toolName,
		Arguments: args,
	}

	dispatcher := provider.NewToolDispatcher(prov)
	res := dispatcher.Execute(context.Background(), call)

	// Report the actual cluster target on stderr for operator verification.
	// This never exposes credentials/tokens.
	fmt.Fprintf(os.Stderr, "provider=%s cluster_host=%s\n", prov.Name(), prov.ClusterHost())

	outBytes, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: failed to serialize tool result: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(string(outBytes))
}
