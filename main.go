package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// defaultLogTailLines is the maximum number of recent log lines retrieved per
// container. Bounded to avoid retrieving unbounded log history.
const defaultLogTailLines int64 = 100

// ── Shared Client ─────────────────────────────────────────────────────────────

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
// then calls collectEvents and collectContainerLogs for each pod/container.
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

		// Collect all container names defined in the spec (used for log collection below)
		containerNames := make([]string, 0, len(pod.Spec.Containers))
		for _, c := range pod.Spec.Containers {
			containerNames = append(containerNames, c.Name)
		}

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

		// Feature 4: collect and display recent container logs for each container
		for _, containerName := range containerNames {
			collectContainerLogs(clientset, pod.Namespace, pod.Name, containerName, defaultLogTailLines)
		}
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

// ── Feature 4: Container Logs Collector ──────────────────────────────────────

// collectContainerLogs retrieves the most recent `tailLines` log lines from the
// specified container in the given pod/namespace using the Kubernetes Logs API.
//
// Behaviour:
//   - Logs are bounded to tailLines (default: 100). Never streams indefinitely.
//   - If the container has not started yet (Waiting state), logs are not available
//     and a clear message is printed instead of returning an error.
//   - If the container name does not exist in the pod, the API returns an error
//     which is surfaced as a WARNING (non-fatal).
//   - No Secret values, credentials, tokens, or kubeconfig contents are printed.
func collectContainerLogs(clientset *kubernetes.Clientset, namespace, podName, containerName string, tailLines int64) {
	fmt.Printf("\n    ── Container Logs: %s / %s (last %d lines) ─────────────\n",
		podName, containerName, tailLines)

	logOpts := &corev1.PodLogOptions{
		Container: containerName,
		TailLines: &tailLines,
	}

	req := clientset.CoreV1().Pods(namespace).GetLogs(podName, logOpts)

	stream, err := req.Stream(context.TODO())
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "waiting to start") ||
			strings.Contains(errMsg, "ContainerCreating") {
			fmt.Printf("    No logs available for container: %s (container not yet started)\n", containerName)
		} else {
			fmt.Fprintf(os.Stderr, "    WARNING: could not retrieve logs for %s/%s: %v\n",
				podName, containerName, err)
		}
		return
	}
	defer stream.Close()

	buf, err := io.ReadAll(stream)
	if err != nil {
		fmt.Fprintf(os.Stderr, "    WARNING: error reading log stream for %s/%s: %v\n",
			podName, containerName, err)
		return
	}

	logContent := strings.TrimRight(string(buf), "\n")
	if logContent == "" {
		fmt.Printf("    No logs available for container: %s\n", containerName)
		return
	}

	lines := strings.Split(logContent, "\n")
	fmt.Printf("    Lines retrieved: %d\n", len(lines))
	fmt.Println()
	for _, line := range lines {
		fmt.Printf("    │ %s\n", line)
	}
}

// ── Feature 5: Unified Evidence Package ──────────────────────────────────────
//
// The evidence package normalises the data already collected by Features 2–4
// into a single machine-readable structure keyed by unique evidence IDs (E1, E2 …).
//
// IMPORTANT:
//   - Training data is separate from runtime evidence.
//   - The Evidence Package contains information collected from the actual
//     Kubernetes cluster. Nothing is invented, inferred, or mocked.
//   - No Secret values, credentials, tokens, or kubeconfig contents are included.
//   - Partial failures are represented honestly: if events are unavailable,
//     that is recorded — fake events are never inserted.

// ── Evidence structs ──────────────────────────────────────────────────────────

// EvidenceSource identifies where a piece of evidence came from.
type EvidenceSource string

const (
	SourcePodStatus       EvidenceSource = "pod_status"
	SourceKubernetesEvent EvidenceSource = "kubernetes_event"
	SourceContainerLogs   EvidenceSource = "container_logs"
)

// ContainerEvidenceData holds the normalised status for one container (Feature 2).
type ContainerEvidenceData struct {
	Name               string `json:"name"`
	Image              string `json:"image"`
	Ready              bool   `json:"ready"`
	RestartCount       int32  `json:"restart_count"`
	State              string `json:"state"`
	LastTermReason     string `json:"last_termination_reason,omitempty"`
	LastTermExitCode   int32  `json:"last_termination_exit_code,omitempty"`
	HasLastTermination bool   `json:"has_last_termination"`
}

// PodStatusData holds the normalised pod status (Feature 2).
type PodStatusData struct {
	PodName    string                  `json:"pod_name"`
	Namespace  string                  `json:"namespace"`
	Phase      string                  `json:"phase"`
	NodeName   string                  `json:"node_name"`
	CreatedAt  string                  `json:"created_at"`
	Containers []ContainerEvidenceData `json:"containers"`
}

// KubernetesEventData holds one normalised Kubernetes Event (Feature 3).
type KubernetesEventData struct {
	PodName       string `json:"pod_name"`
	Namespace     string `json:"namespace"`
	Type          string `json:"type"`
	Reason        string `json:"reason"`
	Message       string `json:"message"`
	InvolvedKind  string `json:"involved_object_kind"`
	InvolvedName  string `json:"involved_object_name"`
	Source        string `json:"source_component,omitempty"`
	Count         int32  `json:"count,omitempty"`
	FirstSeen     string `json:"first_seen,omitempty"`
	LastSeen      string `json:"last_seen,omitempty"`
}

// ContainerLogsData holds the normalised container log snapshot (Feature 4).
type ContainerLogsData struct {
	PodName       string   `json:"pod_name"`
	Namespace     string   `json:"namespace"`
	ContainerName string   `json:"container_name"`
	LineLimit     int64    `json:"line_limit"`
	LinesRetrieved int     `json:"lines_retrieved"`
	Lines         []string `json:"lines"`
	// Error fields — distinguish "no logs exist" from "API failed"
	Unavailable   bool   `json:"unavailable"`
	UnavailableReason string `json:"unavailable_reason,omitempty"`
	APIError      bool   `json:"api_error"`
	APIErrorMsg   string `json:"api_error_message,omitempty"`
}

// EvidenceItem is one normalised piece of evidence with a unique ID.
type EvidenceItem struct {
	ID     string         `json:"id"`
	Source EvidenceSource `json:"source"`
	Data   any            `json:"data"`
}

// EventCollectionResult holds the outcome of gathering events for a pod.
// It clearly distinguishes "no events" from "API error".
type EventCollectionResult struct {
	Items      []KubernetesEventData
	APIError   bool
	APIErrorMsg string
}

// PodEvidencePackage is the unified evidence for a single pod.
type PodEvidencePackage struct {
	PodName     string         `json:"pod_name"`
	Namespace   string         `json:"namespace"`
	CollectedAt string         `json:"collected_at"`
	Evidence    []EvidenceItem `json:"evidence"`
}

// EvidencePackage is the top-level output of Feature 5.
type EvidencePackage struct {
	Cluster     string               `json:"cluster"`
	Namespace   string               `json:"namespace"`
	CollectedAt string               `json:"collected_at"`
	Pods        []PodEvidencePackage `json:"pods"`
}

// ── Data-gathering functions (return structs, do not print) ──────────────────

// gatherPodStatus normalises a corev1.Pod into a PodStatusData struct.
func gatherPodStatus(pod corev1.Pod) PodStatusData {
	nodeName := pod.Spec.NodeName
	if nodeName == "" {
		nodeName = "<not yet scheduled>"
	}

	statusMap := make(map[string]corev1.ContainerStatus, len(pod.Status.ContainerStatuses))
	for _, cs := range pod.Status.ContainerStatuses {
		statusMap[cs.Name] = cs
	}

	containers := make([]ContainerEvidenceData, 0, len(pod.Spec.Containers))
	for _, c := range pod.Spec.Containers {
		ced := ContainerEvidenceData{
			Name:  c.Name,
			Image: c.Image,
		}
		if cs, ok := statusMap[c.Name]; ok {
			ced.Ready = cs.Ready
			ced.RestartCount = cs.RestartCount
			ced.State = containerState(cs.State)
			if cs.LastTerminationState.Terminated != nil {
				lt := cs.LastTerminationState.Terminated
				reason := lt.Reason
				if reason == "" {
					reason = "Unknown"
				}
				ced.HasLastTermination = true
				ced.LastTermReason = reason
				ced.LastTermExitCode = lt.ExitCode
			}
		} else {
			ced.State = "no status available"
		}
		containers = append(containers, ced)
	}

	return PodStatusData{
		PodName:    pod.Name,
		Namespace:  pod.Namespace,
		Phase:      string(pod.Status.Phase),
		NodeName:   nodeName,
		CreatedAt:  pod.CreationTimestamp.Format(time.RFC3339),
		Containers: containers,
	}
}

// gatherEvents queries Kubernetes Events for a specific pod and returns a
// structured result. Clearly distinguishes "no events" from "API error".
// No fake events are ever inserted.
func gatherEvents(clientset *kubernetes.Clientset, namespace, podName string) EventCollectionResult {
	fieldSelector := fmt.Sprintf(
		"involvedObject.name=%s,involvedObject.namespace=%s,involvedObject.kind=Pod",
		podName, namespace,
	)

	events, err := clientset.CoreV1().Events(namespace).List(context.TODO(), metav1.ListOptions{
		FieldSelector: fieldSelector,
	})
	if err != nil {
		return EventCollectionResult{APIError: true, APIErrorMsg: err.Error()}
	}

	// Sort chronologically
	sorted := make([]corev1.Event, len(events.Items))
	copy(sorted, events.Items)
	sort.Slice(sorted, func(i, j int) bool {
		return eventTimestamp(sorted[i]).Before(eventTimestamp(sorted[j]))
	})

	items := make([]KubernetesEventData, 0, len(sorted))
	for _, ev := range sorted {
		ked := KubernetesEventData{
			PodName:      podName,
			Namespace:    ev.Namespace,
			Type:         ev.Type,
			Reason:       ev.Reason,
			Message:      ev.Message,
			InvolvedKind: ev.InvolvedObject.Kind,
			InvolvedName: ev.InvolvedObject.Name,
			Source:       ev.Source.Component,
			Count:        ev.Count,
		}
		if !ev.FirstTimestamp.IsZero() {
			ked.FirstSeen = ev.FirstTimestamp.Format(time.RFC3339)
		}
		if !ev.LastTimestamp.IsZero() {
			ked.LastSeen = ev.LastTimestamp.Format(time.RFC3339)
		}
		items = append(items, ked)
	}

	return EventCollectionResult{Items: items}
}

// gatherContainerLogs retrieves bounded container logs and returns a structured
// ContainerLogsData. Clearly distinguishes empty logs from API errors.
// No fake log lines are ever inserted. Credentials are never logged.
func gatherContainerLogs(clientset *kubernetes.Clientset, namespace, podName, containerName string, tailLines int64) ContainerLogsData {
	result := ContainerLogsData{
		PodName:       podName,
		Namespace:     namespace,
		ContainerName: containerName,
		LineLimit:     tailLines,
	}

	logOpts := &corev1.PodLogOptions{
		Container: containerName,
		TailLines: &tailLines,
	}

	req := clientset.CoreV1().Pods(namespace).GetLogs(podName, logOpts)
	stream, err := req.Stream(context.TODO())
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "waiting to start") ||
			strings.Contains(errMsg, "ContainerCreating") {
			result.Unavailable = true
			result.UnavailableReason = "container not yet started"
		} else {
			result.APIError = true
			result.APIErrorMsg = errMsg
		}
		return result
	}
	defer stream.Close()

	buf, err := io.ReadAll(stream)
	if err != nil {
		result.APIError = true
		result.APIErrorMsg = fmt.Sprintf("error reading log stream: %v", err)
		return result
	}

	logContent := strings.TrimRight(string(buf), "\n")
	if logContent == "" {
		result.Unavailable = true
		result.UnavailableReason = "no log output available"
		return result
	}

	lines := strings.Split(logContent, "\n")
	result.Lines = lines
	result.LinesRetrieved = len(lines)
	return result
}

// ── Evidence package builder ─────────────────────────────────────────────────

// buildEvidencePackage assembles the unified evidence package for all pods in
// the given namespace. Evidence IDs are assigned sequentially (E1, E2, …)
// across all pods in a single run.
func buildEvidencePackage(clientset *kubernetes.Clientset, clusterHost, namespace string) EvidencePackage {
	now := time.Now().UTC().Format(time.RFC3339)

	pkg := EvidencePackage{
		Cluster:     clusterHost,
		Namespace:   namespace,
		CollectedAt: now,
	}

	pods, err := clientset.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: evidence package: failed to list pods: %v\n", err)
		return pkg
	}

	evidenceCounter := 0
	nextID := func() string {
		evidenceCounter++
		return fmt.Sprintf("E%d", evidenceCounter)
	}

	for _, pod := range pods.Items {
		podPkg := PodEvidencePackage{
			PodName:     pod.Name,
			Namespace:   pod.Namespace,
			CollectedAt: now,
		}

		// ── E1-type: pod_status ──────────────────────────────────────────
		podStatus := gatherPodStatus(pod)
		podPkg.Evidence = append(podPkg.Evidence, EvidenceItem{
			ID:     nextID(),
			Source: SourcePodStatus,
			Data:   podStatus,
		})

		// ── E2..N-type: kubernetes_event ─────────────────────────────────
		evResult := gatherEvents(clientset, pod.Namespace, pod.Name)
		if evResult.APIError {
			// Represent the failure honestly — do not fabricate events
			podPkg.Evidence = append(podPkg.Evidence, EvidenceItem{
				ID:     nextID(),
				Source: SourceKubernetesEvent,
				Data: map[string]any{
					"pod_name":  pod.Name,
					"namespace": pod.Namespace,
					"api_error": true,
					"api_error_message": evResult.APIErrorMsg,
				},
			})
		} else {
			for _, ev := range evResult.Items {
				podPkg.Evidence = append(podPkg.Evidence, EvidenceItem{
					ID:     nextID(),
					Source: SourceKubernetesEvent,
					Data:   ev,
				})
			}
		}

		// ── En-type: container_logs ───────────────────────────────────────
		for _, c := range pod.Spec.Containers {
			logs := gatherContainerLogs(clientset, pod.Namespace, pod.Name, c.Name, defaultLogTailLines)
			podPkg.Evidence = append(podPkg.Evidence, EvidenceItem{
				ID:     nextID(),
				Source: SourceContainerLogs,
				Data:   logs,
			})
		}

		pkg.Pods = append(pkg.Pods, podPkg)
	}

	return pkg
}

// printEvidenceSummary prints the human-readable evidence summary section.
func printEvidenceSummary(pkg EvidencePackage) {
	totalEvidence := 0
	for _, p := range pkg.Pods {
		totalEvidence += len(p.Evidence)
	}

	fmt.Println()
	fmt.Println("══════════════════════════════════════════════════════════════")
	fmt.Println("  Unified Evidence Package — Feature 5")
	fmt.Println("══════════════════════════════════════════════════════════════")
	fmt.Printf("  Cluster      : %s\n", pkg.Cluster)
	fmt.Printf("  Namespace    : %s\n", pkg.Namespace)
	fmt.Printf("  Collected At : %s\n", pkg.CollectedAt)
	fmt.Printf("  Pods         : %d\n", len(pkg.Pods))
	fmt.Printf("  Evidence Count: %d\n", totalEvidence)
	fmt.Println()

	for _, p := range pkg.Pods {
		fmt.Printf("  Pod: %s\n", p.PodName)
		for _, ev := range p.Evidence {
			fmt.Printf("    %s → %s\n", ev.ID, ev.Source)
		}
		fmt.Println()
	}

	fmt.Println("── Evidence Package JSON ──────────────────────────────────────")
	jsonBytes, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: failed to serialize evidence package: %v\n", err)
		return
	}
	fmt.Println(string(jsonBytes))

	// Verify JSON round-trips correctly
	var roundTrip EvidencePackage
	if err := json.Unmarshal(jsonBytes, &roundTrip); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: evidence package JSON failed to parse back: %v\n", err)
	}
}

// ── Feature 7: Deterministic Diagnosis Engine ───────────────────────────────
//
// The deterministic diagnosis engine analyzes the EvidencePackage (Feature 5)
// and identifies known Kubernetes failure conditions using explicit rules.
//
// Key Principles:
//   - Zero AI / LLM / probabilistic confidence scores used.
//   - Rules must NEVER guess. If evidence is insufficient, returns UNKNOWN.
//   - Every CONFIRMED diagnosis includes the exact EvidenceItem IDs supporting it.
//   - Distinguishes symptoms (e.g. CrashLoopBackOff) from root causes (e.g. OOMKilled).

type DiagnosisStatus string

const (
	DiagnosisConfirmed DiagnosisStatus = "CONFIRMED"
	DiagnosisUnknown   DiagnosisStatus = "UNKNOWN"
)

type DiagnosisCode string

const (
	CodeImagePullFailure    DiagnosisCode = "IMAGE_PULL_FAILURE"
	CodeOOMKilled           DiagnosisCode = "OOM_KILLED"
	CodeNodeNotReady        DiagnosisCode = "NODE_NOT_READY"
	CodeInsufficientMemory  DiagnosisCode = "INSUFFICIENT_MEMORY"
	CodeInsufficientCPU     DiagnosisCode = "INSUFFICIENT_CPU"
	CodeInsufficientGPU     DiagnosisCode = "INSUFFICIENT_GPU"
	CodeDBAuthFailure       DiagnosisCode = "DB_AUTH_FAILURE"
	CodeDBConnectionFailure DiagnosisCode = "DB_CONNECTION_FAILURE"
	CodeUnknown             DiagnosisCode = "UNKNOWN"
)

type PodDiagnosis struct {
	PodName       string          `json:"pod_name"`
	Namespace     string          `json:"namespace"`
	Status        DiagnosisStatus `json:"status"`
	DiagnosisCode DiagnosisCode   `json:"diagnosis_code"`
	Summary       string          `json:"summary"`
	Explanation   string          `json:"explanation"`
	EvidenceIDs   []string        `json:"evidence_ids"`
}

type DiagnosisReport struct {
	Cluster     string         `json:"cluster"`
	Namespace   string         `json:"namespace"`
	DiagnosedAt string         `json:"diagnosed_at"`
	Results     []PodDiagnosis `json:"results"`
}

// dedupeStrings removes duplicate strings from a slice preserving order.
func dedupeStrings(slice []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(slice))
	for _, s := range slice {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}

// asPodStatusData attempts to extract PodStatusData from EvidenceItem.Data.
func asPodStatusData(data any) (PodStatusData, bool) {
	if ps, ok := data.(PodStatusData); ok {
		return ps, true
	}
	if m, ok := data.(map[string]any); ok {
		b, err := json.Marshal(m)
		if err != nil {
			return PodStatusData{}, false
		}
		var ps PodStatusData
		if err := json.Unmarshal(b, &ps); err == nil && ps.PodName != "" {
			return ps, true
		}
	}
	return PodStatusData{}, false
}

// asKubernetesEventData attempts to extract KubernetesEventData from EvidenceItem.Data.
func asKubernetesEventData(data any) (KubernetesEventData, bool) {
	if ev, ok := data.(KubernetesEventData); ok {
		return ev, true
	}
	if m, ok := data.(map[string]any); ok {
		b, err := json.Marshal(m)
		if err != nil {
			return KubernetesEventData{}, false
		}
		var ev KubernetesEventData
		if err := json.Unmarshal(b, &ev); err == nil && ev.Reason != "" {
			return ev, true
		}
	}
	return KubernetesEventData{}, false
}

// diagnosePod evaluates deterministic rules against a single pod's evidence.
//
// Rule Precedence Logic:
//  1. OOM_KILLED: Causal evidence (last termination state OOMKilled or OOMKilled event).
//     Takes precedence over generic CrashLoopBackOff/BackOff states.
//  2. IMAGE_PULL_FAILURE: Container state ErrImagePull/ImagePullBackOff or image pull events.
//  3. NODE_NOT_READY: Explicit NodeNotReady event evidence.
//  4. Default: UNKNOWN (insufficient evidence or symptom-only states like CrashLoopBackOff/Pending).
func diagnosePod(podPkg PodEvidencePackage) PodDiagnosis {
	// ── Rule 1: OOM_KILLED ───────────────────────────────────────────────────
	var oomEvIDs []string
	for _, ev := range podPkg.Evidence {
		if ev.Source == SourcePodStatus {
			if ps, ok := asPodStatusData(ev.Data); ok {
				for _, c := range ps.Containers {
					if c.HasLastTermination && strings.EqualFold(c.LastTermReason, "OOMKilled") {
						oomEvIDs = append(oomEvIDs, ev.ID)
						break
					}
					if strings.Contains(c.State, "OOMKilled") {
						oomEvIDs = append(oomEvIDs, ev.ID)
						break
					}
				}
			}
		} else if ev.Source == SourceKubernetesEvent {
			if ke, ok := asKubernetesEventData(ev.Data); ok {
				if strings.EqualFold(ke.Reason, "OOMKilled") ||
					strings.Contains(ke.Message, "OOMKilled") ||
					strings.Contains(ke.Message, "Out of memory: Kill process") {
					oomEvIDs = append(oomEvIDs, ev.ID)
				}
			}
		}
	}
	if len(oomEvIDs) > 0 {
		return PodDiagnosis{
			PodName:       podPkg.PodName,
			Namespace:     podPkg.Namespace,
			Status:        DiagnosisConfirmed,
			DiagnosisCode: CodeOOMKilled,
			Summary:       "Container process killed due to memory limit exhaustion (OOMKilled).",
			Explanation:   "Kubernetes or kernel OOM killer terminated the container process because it exceeded its allocated memory limit.",
			EvidenceIDs:   dedupeStrings(oomEvIDs),
		}
	}

	// ── Rule 2: IMAGE_PULL_FAILURE ──────────────────────────────────────────
	var imgEvIDs []string
	for _, ev := range podPkg.Evidence {
		if ev.Source == SourcePodStatus {
			if ps, ok := asPodStatusData(ev.Data); ok {
				for _, c := range ps.Containers {
					if strings.Contains(c.State, "ErrImagePull") || strings.Contains(c.State, "ImagePullBackOff") {
						imgEvIDs = append(imgEvIDs, ev.ID)
						break
					}
				}
			}
		} else if ev.Source == SourceKubernetesEvent {
			if ke, ok := asKubernetesEventData(ev.Data); ok {
				reason := ke.Reason
				msg := ke.Message
				if reason == "ErrImagePull" || reason == "ImagePullBackOff" ||
					(reason == "Failed" && (strings.Contains(msg, "Failed to pull image") || strings.Contains(msg, "pull access denied") || strings.Contains(msg, "Back-off pulling image"))) {
					imgEvIDs = append(imgEvIDs, ev.ID)
				}
			}
		}
	}
	if len(imgEvIDs) > 0 {
		return PodDiagnosis{
			PodName:       podPkg.PodName,
			Namespace:     podPkg.Namespace,
			Status:        DiagnosisConfirmed,
			DiagnosisCode: CodeImagePullFailure,
			Summary:       "Container image could not be pulled from registry.",
			Explanation:   "Kubernetes reports that the container image could not be pulled and the container is in a waiting state with ErrImagePull / ImagePullBackOff.",
			EvidenceIDs:   dedupeStrings(imgEvIDs),
		}
	}

	// ── Rule 3: NODE_NOT_READY ───────────────────────────────────────────────
	var nodeEvIDs []string
	for _, ev := range podPkg.Evidence {
		if ev.Source == SourceKubernetesEvent {
			if ke, ok := asKubernetesEventData(ev.Data); ok {
				if ke.Reason == "NodeNotReady" || strings.Contains(ke.Message, "NodeNotReady") {
					nodeEvIDs = append(nodeEvIDs, ev.ID)
				}
			}
		}
	}
	if len(nodeEvIDs) > 0 {
		return PodDiagnosis{
			PodName:       podPkg.PodName,
			Namespace:     podPkg.Namespace,
			Status:        DiagnosisConfirmed,
			DiagnosisCode: CodeNodeNotReady,
			Summary:       "Target node is in NotReady condition.",
			Explanation:   "Kubernetes event indicates that the node hosting or assigned to the pod is not ready.",
			EvidenceIDs:   dedupeStrings(nodeEvIDs),
		}
	}

	// ── Default: UNKNOWN ─────────────────────────────────────────────────────
	return PodDiagnosis{
		PodName:       podPkg.PodName,
		Namespace:     podPkg.Namespace,
		Status:        DiagnosisUnknown,
		DiagnosisCode: CodeUnknown,
		Summary:       "Available evidence is insufficient to establish a supported root cause.",
		Explanation:   "The available evidence (pod status, events, container logs) does not establish a supported root cause.",
		EvidenceIDs:   []string{},
	}
}

// diagnoseEvidence evaluates deterministic rules against all pods in the EvidencePackage.
func diagnoseEvidence(pkg EvidencePackage) DiagnosisReport {
	now := time.Now().UTC().Format(time.RFC3339)
	report := DiagnosisReport{
		Cluster:     pkg.Cluster,
		Namespace:   pkg.Namespace,
		DiagnosedAt: now,
		Results:     make([]PodDiagnosis, 0, len(pkg.Pods)),
	}

	for _, podPkg := range pkg.Pods {
		diag := diagnosePod(podPkg)
		report.Results = append(report.Results, diag)
	}

	return report
}

// printDiagnosisSummary prints human-readable diagnosis output to CLI.
func printDiagnosisSummary(report DiagnosisReport) {
	fmt.Println()
	fmt.Println("══════════════════════════════════════════════════════════════")
	fmt.Println("  Deterministic Diagnosis — Feature 7")
	fmt.Println("══════════════════════════════════════════════════════════════")
	fmt.Printf("  Cluster      : %s\n", report.Cluster)
	fmt.Printf("  Namespace    : %s\n", report.Namespace)
	fmt.Printf("  Diagnosed At : %s\n", report.DiagnosedAt)
	fmt.Printf("  Pods Analyzed: %d\n", len(report.Results))
	fmt.Println()

	for _, res := range report.Results {
		fmt.Printf("  Pod: %s\n", res.PodName)
		fmt.Printf("    Status        : %s\n", res.Status)
		fmt.Printf("    Diagnosis Code: %s\n", res.DiagnosisCode)
		fmt.Printf("    Summary       : %s\n", res.Summary)
		fmt.Printf("    Explanation   : %s\n", res.Explanation)
		if len(res.EvidenceIDs) > 0 {
			fmt.Printf("    Evidence IDs  : %s\n", strings.Join(res.EvidenceIDs, ", "))
		} else {
			fmt.Printf("    Evidence IDs  : none (insufficient evidence for root cause)\n")
		}
		fmt.Println()
	}

	fmt.Println("── Diagnosis Report JSON ──────────────────────────────────────")
	jsonBytes, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: failed to serialize diagnosis report: %v\n", err)
		return
	}
	fmt.Println(string(jsonBytes))
}

// ── Entry Point ───────────────────────────────────────────────────────────────

func main() {
	config, clientset := buildClient()

	// Feature 1: list nodes (human-readable)
	listNodes(clientset, config.Host)

	// Feature 2 + 3 + 4: pod status, events, logs (human-readable)
	collectPods(clientset, "default")

	// Feature 5: build and print unified evidence package
	pkg := buildEvidencePackage(clientset, config.Host, "default")
	printEvidenceSummary(pkg)

	// Feature 7: deterministic diagnosis engine
	diagReport := diagnoseEvidence(pkg)
	printDiagnosisSummary(diagReport)
}

