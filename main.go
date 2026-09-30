package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"text/tabwriter"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// buildClient resolves the kubeconfig and returns a REST config + typed clientset.
// It respects the $KUBECONFIG environment variable, falling back to ~/.kube/config.
// Credentials and tokens are never printed.
func buildClient() (*rest.Config, *kubernetes.Clientset) {
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: cannot determine home directory: %v\n", err)
			os.Exit(1)
		}
		kubeconfig = filepath.Join(home, ".kube", "config")
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: failed to load kubeconfig from %s: %v\n", kubeconfig, err)
		os.Exit(1)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: failed to create Kubernetes client: %v\n", err)
		os.Exit(1)
	}

	return config, clientset
}

// ── Feature 1: Node Lister ────────────────────────────────────────────────────

func listNodes(clientset *kubernetes.Clientset, host string) {
	nodes, err := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: failed to list nodes: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Connected to Kubernetes cluster via: %s\n\n", host)
	fmt.Printf("Nodes (%d):\n", len(nodes.Items))

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 4, ' ', 0)
	fmt.Fprintln(w, "NAME\tSTATUS")
	fmt.Fprintln(w, "----\t------")
	for _, node := range nodes.Items {
		status := "Unknown"
		for _, cond := range node.Status.Conditions {
			if cond.Type == "Ready" {
				if cond.Status == "True" {
					status = "Ready"
				} else {
					status = "NotReady"
				}
				break
			}
		}
		fmt.Fprintf(w, "%s\t%s\n", node.Name, status)
	}
	w.Flush()
}

// ── Feature 2: Pod Status Collector ──────────────────────────────────────────

// containerState returns a human-readable string for the current container state.
func containerState(state corev1.ContainerState) string {
	switch {
	case state.Running != nil:
		return fmt.Sprintf("Running (started: %s)", state.Running.StartedAt.Format("2006-01-02 15:04:05"))
	case state.Waiting != nil:
		reason := state.Waiting.Reason
		if reason == "" {
			reason = "Unknown"
		}
		msg := state.Waiting.Message
		if msg != "" {
			return fmt.Sprintf("Waiting (%s: %s)", reason, msg)
		}
		return fmt.Sprintf("Waiting (%s)", reason)
	case state.Terminated != nil:
		return fmt.Sprintf("Terminated (reason: %s, exit: %d)",
			state.Terminated.Reason, state.Terminated.ExitCode)
	default:
		return "Unknown"
	}
}

// collectPods retrieves all pods from the given namespace, prints their status,
// and then calls collectEvents for each pod.
// No Secret values, environment variables, passwords, tokens, or certificates are printed.
func collectPods(clientset *kubernetes.Clientset, namespace string) {
	pods, err := clientset.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: failed to list pods in namespace %q: %v\n", namespace, err)
		os.Exit(1)
	}

	fmt.Printf("\n── Pod Status Collector (namespace: %s) ──────────────────────────\n", namespace)

	if len(pods.Items) == 0 {
		fmt.Printf("No pods found in namespace: %s\n", namespace)
		return
	}

	fmt.Printf("Pods found: %d\n", len(pods.Items))

	for i, pod := range pods.Items {
		fmt.Printf("\n[%d/%d] Pod: %s\n", i+1, len(pods.Items), pod.Name)
		fmt.Printf("    Namespace   : %s\n", pod.Namespace)
		fmt.Printf("    Phase       : %s\n", string(pod.Status.Phase))
		fmt.Printf("    Created     : %s\n", pod.CreationTimestamp.Format("2006-01-02 15:04:05 UTC"))

		nodeName := pod.Spec.NodeName
		if nodeName == "" {
			nodeName = "<not yet scheduled>"
		}
		fmt.Printf("    Node        : %s\n", nodeName)

		if len(pod.Status.ContainerStatuses) == 0 {
			// Pod may be pending with no status yet
			for _, c := range pod.Spec.Containers {
				fmt.Printf("\n    Container   : %s\n", c.Name)
				fmt.Printf("      Image     : %s\n", c.Image)
				fmt.Printf("      Ready     : false\n")
				fmt.Printf("      State     : Pending (no status yet)\n")
			}
		} else {
			// Build a map: containerName → ContainerStatus for O(1) lookup
			statusMap := make(map[string]corev1.ContainerStatus, len(pod.Status.ContainerStatuses))
			for _, cs := range pod.Status.ContainerStatuses {
				statusMap[cs.Name] = cs
			}

			for _, c := range pod.Spec.Containers {
				fmt.Printf("\n    Container   : %s\n", c.Name)
				fmt.Printf("      Image     : %s\n", c.Image)

				cs, ok := statusMap[c.Name]
				if !ok {
					fmt.Printf("      Ready     : unknown\n")
					fmt.Printf("      State     : no status available\n")
					continue
				}

				fmt.Printf("      Ready     : %v\n", cs.Ready)
				fmt.Printf("      Restarts  : %d\n", cs.RestartCount)
				fmt.Printf("      State     : %s\n", containerState(cs.State))

				// Last termination info (if available)
				if cs.LastTerminationState.Terminated != nil {
					lt := cs.LastTerminationState.Terminated
					reason := lt.Reason
					if reason == "" {
						reason = "Unknown"
					}
					fmt.Printf("      Last Term : reason=%s  exit=%d\n", reason, lt.ExitCode)
				}
			}
		}

		// Feature 3: collect and display Kubernetes Events for this pod
		collectEvents(clientset, pod.Namespace, pod.Name)
	}
	fmt.Println()
}

// ── Feature 3: Kubernetes Events Collector ────────────────────────────────────

// eventTimestamp returns the best available timestamp for sorting.
// Prefers LastTimestamp; falls back to FirstTimestamp; zero time if neither set.
func eventTimestamp(e corev1.Event) time.Time {
	if !e.LastTimestamp.IsZero() {
		return e.LastTimestamp.Time
	}
	if !e.FirstTimestamp.IsZero() {
		return e.FirstTimestamp.Time
	}
	return time.Time{}
}

// collectEvents retrieves all Kubernetes Events for a specific pod (by name) in
// the given namespace, then prints them in chronological order.
//
// Filtering uses the Kubernetes API field selector involvedObject.name=<podName>
// so only events that directly reference the pod are returned — not unrelated
// cluster-wide events. No Secret values, credentials or tokens are printed.
func collectEvents(clientset *kubernetes.Clientset, namespace, podName string) {
	fieldSelector := fmt.Sprintf(
		"involvedObject.name=%s,involvedObject.namespace=%s,involvedObject.kind=Pod",
		podName, namespace,
	)

	events, err := clientset.CoreV1().Events(namespace).List(context.TODO(), metav1.ListOptions{
		FieldSelector: fieldSelector,
	})
	if err != nil {
		// Non-fatal: report and continue so other output is not lost
		fmt.Fprintf(os.Stderr, "  WARNING: failed to retrieve events for pod %q: %v\n", podName, err)
		return
	}

	fmt.Printf("\n    ── Kubernetes Events for pod: %s ────────────────────────\n", podName)

	if len(events.Items) == 0 {
		fmt.Printf("    No events found for pod: %s\n", podName)
		return
	}

	// Sort events chronologically (oldest → newest)
	sorted := make([]corev1.Event, len(events.Items))
	copy(sorted, events.Items)
	sort.Slice(sorted, func(i, j int) bool {
		return eventTimestamp(sorted[i]).Before(eventTimestamp(sorted[j]))
	})

	for idx, ev := range sorted {
		fmt.Printf("\n    Event %d\n", idx+1)
		fmt.Printf("      Type      : %s\n", ev.Type)
		fmt.Printf("      Reason    : %s\n", ev.Reason)
		fmt.Printf("      Message   : %s\n", ev.Message)

		// Involved object
		fmt.Printf("      Object    : %s/%s\n", ev.InvolvedObject.Kind, ev.InvolvedObject.Name)

		// Source component (may be empty in newer Event API versions)
		if ev.Source.Component != "" {
			fmt.Printf("      Source    : %s\n", ev.Source.Component)
		}

		// Count
		if ev.Count > 0 {
			fmt.Printf("      Count     : %d\n", ev.Count)
		}

		// Timestamps — only print if non-zero
		if !ev.FirstTimestamp.IsZero() {
			fmt.Printf("      First Seen: %s\n", ev.FirstTimestamp.Format("2006-01-02 15:04:05 UTC"))
		}
		if !ev.LastTimestamp.IsZero() {
			fmt.Printf("      Last Seen : %s\n", ev.LastTimestamp.Format("2006-01-02 15:04:05 UTC"))
		}
	}
}

// ── Entry Point ───────────────────────────────────────────────────────────────

func main() {
	config, clientset := buildClient()

	// Feature 1: list nodes
	listNodes(clientset, config.Host)

	// Feature 2: collect pod status from the default namespace
	// Feature 3: collectEvents is called inside collectPods for each pod
	collectPods(clientset, "default")
}
