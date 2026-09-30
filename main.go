package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/stack8s/stack8s-ai-diagnose/provider"
)

// defaultLogTailLines is the maximum number of recent log lines retrieved per container.
const defaultLogTailLines int64 = 100

// ── Provider Factory ─────────────────────────────────────────────────────────

// getProvider resolves the InfrastructureProvider implementation based on the
// STACK8S_PROVIDER environment variable (defaults to "local-kubernetes").
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
	default:
		fmt.Fprintf(os.Stderr, "ERROR: unknown provider %q. Supported providers: local-kubernetes, stack8s\n", providerType)
		os.Exit(1)
		return nil
	}
}

// ── Feature 1: Node Lister ────────────────────────────────────────────────────

func listNodes(prov provider.InfrastructureProvider) {
	nodes, err := prov.GetNodes(context.TODO())
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: failed to list nodes: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Connected to cluster via provider [%s]: %s\n\n", prov.Name(), prov.ClusterHost())
	fmt.Printf("Nodes (%d):\n", len(nodes))

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 4, ' ', 0)
	fmt.Fprintln(w, "NAME\tSTATUS")
	fmt.Fprintln(w, "----\t------")
	for _, node := range nodes {
		fmt.Fprintf(w, "%s\t%s\n", node.Name, node.Status)
	}
	w.Flush()
}

// ── Feature 2: Pod Status Collector ──────────────────────────────────────────

func collectPods(prov provider.InfrastructureProvider, namespace string) {
	pods, err := prov.GetPods(context.TODO(), namespace)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: failed to list pods in namespace %q: %v\n", namespace, err)
		os.Exit(1)
	}

	fmt.Printf("\n── Pod Status Collector (namespace: %s) ──────────────────────────\n", namespace)

	if len(pods) == 0 {
		fmt.Printf("No pods found in namespace: %s\n", namespace)
		return
	}

	fmt.Printf("Pods found: %d\n", len(pods))

	for i, pod := range pods {
		fmt.Printf("\n[%d/%d] Pod: %s\n", i+1, len(pods), pod.PodName)
		fmt.Printf("    Namespace   : %s\n", pod.Namespace)
		fmt.Printf("    Phase       : %s\n", pod.Phase)
		fmt.Printf("    Created     : %s\n", pod.CreatedAt)
		fmt.Printf("    Node        : %s\n", pod.NodeName)

		for _, c := range pod.Containers {
			fmt.Printf("\n    Container   : %s\n", c.Name)
			fmt.Printf("      Image     : %s\n", c.Image)
			fmt.Printf("      Ready     : %v\n", c.Ready)
			fmt.Printf("      Restarts  : %d\n", c.RestartCount)
			fmt.Printf("      State     : %s\n", c.State)
			if c.HasLastTermination {
				fmt.Printf("      Last Term : reason=%s  exit=%d\n", c.LastTermReason, c.LastTermExitCode)
			}
		}

		// Feature 3: collect and display Kubernetes Events for this pod
		collectEvents(prov, pod.Namespace, pod.PodName)

		// Feature 4: collect and display recent container logs for each container
		for _, c := range pod.Containers {
			collectContainerLogs(prov, pod.Namespace, pod.PodName, c.Name, defaultLogTailLines)
		}
	}
	fmt.Println()
}

// ── Feature 3: Kubernetes Events Collector ────────────────────────────────────

func collectEvents(prov provider.InfrastructureProvider, namespace, podName string) {
	events, err := prov.GetPodEvents(context.TODO(), namespace, podName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  WARNING: failed to retrieve events for pod %q: %v\n", podName, err)
		return
	}

	fmt.Printf("\n    ── Kubernetes Events for pod: %s ────────────────────────\n", podName)

	if len(events) == 0 {
		fmt.Printf("    No events found for pod: %s\n", podName)
		return
	}

	for idx, ev := range events {
		fmt.Printf("\n    Event %d\n", idx+1)
		fmt.Printf("      Type      : %s\n", ev.Type)
		fmt.Printf("      Reason    : %s\n", ev.Reason)
		fmt.Printf("      Message   : %s\n", ev.Message)
		fmt.Printf("      Object    : %s/%s\n", ev.InvolvedKind, ev.InvolvedName)

		if ev.Source != "" {
			fmt.Printf("      Source    : %s\n", ev.Source)
		}
		if ev.Count > 0 {
			fmt.Printf("      Count     : %d\n", ev.Count)
		}
		if ev.FirstSeen != "" {
			fmt.Printf("      First Seen: %s\n", ev.FirstSeen)
		}
		if ev.LastSeen != "" {
			fmt.Printf("      Last Seen : %s\n", ev.LastSeen)
		}
	}
}

// ── Feature 4: Container Logs Collector ──────────────────────────────────────

func collectContainerLogs(prov provider.InfrastructureProvider, namespace, podName, containerName string, tailLines int64) {
	fmt.Printf("\n    ── Container Logs: %s / %s (last %d lines) ─────────────\n",
		podName, containerName, tailLines)

	logs, err := prov.GetContainerLogs(context.TODO(), namespace, podName, containerName, tailLines)
	if err != nil {
		fmt.Fprintf(os.Stderr, "    WARNING: could not retrieve logs for %s/%s: %v\n", podName, containerName, err)
		return
	}

	if logs.Unavailable {
		fmt.Printf("    No logs available for container: %s (%s)\n", containerName, logs.UnavailableReason)
		return
	}
	if logs.APIError {
		fmt.Fprintf(os.Stderr, "    WARNING: API error retrieving logs for %s/%s: %s\n", podName, containerName, logs.APIErrorMsg)
		return
	}

	if len(logs.Lines) == 0 {
		fmt.Printf("    No logs available for container: %s\n", containerName)
		return
	}

	fmt.Printf("    Lines retrieved: %d\n", len(logs.Lines))
	fmt.Println()
	for _, line := range logs.Lines {
		fmt.Printf("    │ %s\n", line)
	}
}

// ── Feature 5 & 10: Unified Evidence Package ──────────────────────────────────

type EvidenceSource string

const (
	SourcePodStatus       EvidenceSource = "pod_status"
	SourceKubernetesEvent EvidenceSource = "kubernetes_event"
	SourceContainerLogs   EvidenceSource = "container_logs"
	SourceNodeStatus      EvidenceSource = "node_status"
	SourceGPUStatus       EvidenceSource = "gpu_status"
)

type ContainerEvidenceData struct {
	Name               string `json:"name"`
	Image              string `json:"image"`
	Ready              bool   `json:"ready"`
	RestartCount       int32  `json:"restart_count"`
	State              string `json:"state"`
	LastTermReason     string `json:"last_termination_reason,omitempty"`
	LastTermExitCode   int32  `json:"last_termination_exit_code,omitempty"`
	HasLastTermination bool   `json:"has_last_termination"`
	CPURequest         string `json:"cpu_request,omitempty"`
	CPULimit           string `json:"cpu_limit,omitempty"`
	MemoryRequest      string `json:"memory_request,omitempty"`
	MemoryLimit        string `json:"memory_limit,omitempty"`
	GPURequest         string `json:"gpu_request,omitempty"`
	GPULimit           string `json:"gpu_limit,omitempty"`
}

type PodStatusData struct {
	PodName    string                  `json:"pod_name"`
	Namespace  string                  `json:"namespace"`
	Phase      string                  `json:"phase"`
	NodeName   string                  `json:"node_name"`
	CreatedAt  string                  `json:"created_at"`
	Containers []ContainerEvidenceData `json:"containers"`
}

type NodeStatusData struct {
	NodeName     string                  `json:"node_name"`
	NodeAssigned bool                    `json:"node_assigned"`
	Status       string                  `json:"status,omitempty"`
	Resources    *provider.NodeResources  `json:"resources,omitempty"`
	SystemInfo   *provider.NodeSystemInfo `json:"system_info,omitempty"`
	Unavailable  bool                    `json:"unavailable,omitempty"`
	Reason       string                  `json:"reason,omitempty"`
}

type GPUStatusData struct {
	NodeName           string `json:"node_name"`
	NodeAssigned       bool   `json:"node_assigned"`
	GPUAvailable       bool   `json:"gpu_available"`
	GPUResourceExposed bool   `json:"gpu_resource_exposed"`
	ResourceName       string `json:"resource_name,omitempty"`
	GPUCapacity        string `json:"gpu_capacity,omitempty"`
	GPUAllocatable     string `json:"gpu_allocatable,omitempty"`
	GPUModel           string `json:"gpu_model,omitempty"`
	Unavailable        bool   `json:"unavailable,omitempty"`
	Reason             string `json:"reason,omitempty"`
}

type KubernetesEventData struct {
	PodName      string `json:"pod_name"`
	Namespace    string `json:"namespace"`
	Type         string `json:"type"`
	Reason       string `json:"reason"`
	Message      string `json:"message"`
	InvolvedKind string `json:"involved_object_kind"`
	InvolvedName string `json:"involved_object_name"`
	Source       string `json:"source_component,omitempty"`
	Count        int32  `json:"count,omitempty"`
	FirstSeen    string `json:"first_seen,omitempty"`
	LastSeen     string `json:"last_seen,omitempty"`
}

type ContainerLogsData struct {
	PodName           string   `json:"pod_name"`
	Namespace         string   `json:"namespace"`
	ContainerName     string   `json:"container_name"`
	LineLimit         int64    `json:"line_limit"`
	LinesRetrieved    int      `json:"lines_retrieved"`
	Lines             []string `json:"lines"`
	Unavailable       bool     `json:"unavailable"`
	UnavailableReason string   `json:"unavailable_reason,omitempty"`
	APIError          bool     `json:"api_error"`
	APIErrorMsg       string   `json:"api_error_message,omitempty"`
}

type EvidenceItem struct {
	ID     string         `json:"id"`
	Source EvidenceSource `json:"source"`
	Data   any            `json:"data"`
}

type PodEvidencePackage struct {
	PodName     string         `json:"pod_name"`
	Namespace   string         `json:"namespace"`
	CollectedAt string         `json:"collected_at"`
	Evidence    []EvidenceItem `json:"evidence"`
}

type EvidencePackage struct {
	Cluster     string               `json:"cluster"`
	Namespace   string               `json:"namespace"`
	CollectedAt string               `json:"collected_at"`
	Pods        []PodEvidencePackage `json:"pods"`
}

// buildEvidencePackage assembles unified evidence package consuming InfrastructureProvider.
func buildEvidencePackage(prov provider.InfrastructureProvider, namespace string) EvidencePackage {
	now := time.Now().UTC().Format(time.RFC3339)

	pkg := EvidencePackage{
		Cluster:     prov.ClusterHost(),
		Namespace:   namespace,
		CollectedAt: now,
	}

	pods, err := prov.GetPods(context.TODO(), namespace)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: evidence package: failed to list pods: %v\n", err)
		return pkg
	}

	evidenceCounter := 0
	nextID := func() string {
		evidenceCounter++
		return fmt.Sprintf("E%d", evidenceCounter)
	}

	for _, pod := range pods {
		podPkg := PodEvidencePackage{
			PodName:     pod.PodName,
			Namespace:   pod.Namespace,
			CollectedAt: now,
		}

		// ── E1-type: pod_status ──────────────────────────────────────────
		containers := make([]ContainerEvidenceData, 0, len(pod.Containers))
		for _, c := range pod.Containers {
			containers = append(containers, ContainerEvidenceData{
				Name:               c.Name,
				Image:              c.Image,
				Ready:              c.Ready,
				RestartCount:       c.RestartCount,
				State:              c.State,
				LastTermReason:     c.LastTermReason,
				LastTermExitCode:   c.LastTermExitCode,
				HasLastTermination: c.HasLastTermination,
				CPURequest:         c.CPURequest,
				CPULimit:           c.CPULimit,
				MemoryRequest:      c.MemoryRequest,
				MemoryLimit:        c.MemoryLimit,
				GPURequest:         c.GPURequest,
				GPULimit:           c.GPULimit,
			})
		}
		podStatus := PodStatusData{
			PodName:    pod.PodName,
			Namespace:  pod.Namespace,
			Phase:      pod.Phase,
			NodeName:   pod.NodeName,
			CreatedAt:  pod.CreatedAt,
			Containers: containers,
		}
		podPkg.Evidence = append(podPkg.Evidence, EvidenceItem{
			ID:     nextID(),
			Source: SourcePodStatus,
			Data:   podStatus,
		})

		// ── E_node-type: node_status (Feature 10) ─────────────────────────
		if pod.NodeName != "" && pod.NodeName != "<not yet scheduled>" && pod.NodeName != "<none>" {
			nodeInfo, err := prov.GetNode(context.TODO(), pod.NodeName)
			if err != nil {
				podPkg.Evidence = append(podPkg.Evidence, EvidenceItem{
					ID:     nextID(),
					Source: SourceNodeStatus,
					Data: NodeStatusData{
						NodeName:     pod.NodeName,
						NodeAssigned: true,
						Unavailable:  true,
						Reason:       err.Error(),
					},
				})
			} else {
				podPkg.Evidence = append(podPkg.Evidence, EvidenceItem{
					ID:     nextID(),
					Source: SourceNodeStatus,
					Data: NodeStatusData{
						NodeName:     nodeInfo.Name,
						NodeAssigned: true,
						Status:       nodeInfo.Status,
						Resources:    &nodeInfo.Resources,
						SystemInfo:   &nodeInfo.SystemInfo,
					},
				})
			}
		} else {
			podPkg.Evidence = append(podPkg.Evidence, EvidenceItem{
				ID:     nextID(),
				Source: SourceNodeStatus,
				Data: NodeStatusData{
					NodeName:     "<none>",
					NodeAssigned: false,
					Reason:       "pod has no assigned node (unscheduled)",
				},
			})
		}

		// ── E_gpu-type: gpu_status (Feature 11) ───────────────────────────
		if pod.NodeName != "" && pod.NodeName != "<not yet scheduled>" && pod.NodeName != "<none>" {
			gpuInfo, err := prov.GetNodeGPUResources(context.TODO(), pod.NodeName)
			if err != nil {
				podPkg.Evidence = append(podPkg.Evidence, EvidenceItem{
					ID:     nextID(),
					Source: SourceGPUStatus,
					Data: GPUStatusData{
						NodeName:           pod.NodeName,
						NodeAssigned:       true,
						GPUAvailable:       false,
						GPUResourceExposed: false,
						Unavailable:        true,
						Reason:             err.Error(),
					},
				})
			} else {
				podPkg.Evidence = append(podPkg.Evidence, EvidenceItem{
					ID:     nextID(),
					Source: SourceGPUStatus,
					Data: GPUStatusData{
						NodeName:           gpuInfo.NodeName,
						NodeAssigned:       true,
						GPUAvailable:       gpuInfo.GPUAvailable,
						GPUResourceExposed: gpuInfo.GPUResourceExposed,
						ResourceName:       gpuInfo.ResourceName,
						GPUCapacity:        gpuInfo.Capacity,
						GPUAllocatable:     gpuInfo.Allocatable,
						GPUModel:           gpuInfo.Model,
					},
				})
			}
		} else {
			podPkg.Evidence = append(podPkg.Evidence, EvidenceItem{
				ID:     nextID(),
				Source: SourceGPUStatus,
				Data: GPUStatusData{
					NodeName:           "<none>",
					NodeAssigned:       false,
					GPUAvailable:       false,
					GPUResourceExposed: false,
					Reason:             "pod has no assigned node (unscheduled)",
				},
			})
		}

		// ── E2..N-type: kubernetes_event ─────────────────────────────────
		events, err := prov.GetPodEvents(context.TODO(), pod.Namespace, pod.PodName)
		if err != nil {
			podPkg.Evidence = append(podPkg.Evidence, EvidenceItem{
				ID:     nextID(),
				Source: SourceKubernetesEvent,
				Data: map[string]any{
					"pod_name":          pod.PodName,
					"namespace":         pod.Namespace,
					"api_error":         true,
					"api_error_message": err.Error(),
				},
			})
		} else {
			for _, ev := range events {
				ked := KubernetesEventData{
					PodName:      ev.PodName,
					Namespace:    ev.Namespace,
					Type:         ev.Type,
					Reason:       ev.Reason,
					Message:      ev.Message,
					InvolvedKind: ev.InvolvedKind,
					InvolvedName: ev.InvolvedName,
					Source:       ev.Source,
					Count:        ev.Count,
					FirstSeen:    ev.FirstSeen,
					LastSeen:     ev.LastSeen,
				}
				podPkg.Evidence = append(podPkg.Evidence, EvidenceItem{
					ID:     nextID(),
					Source: SourceKubernetesEvent,
					Data:   ked,
				})
			}
		}

		// ── En-type: container_logs ───────────────────────────────────────
		for _, c := range pod.Containers {
			logInfo, _ := prov.GetContainerLogs(context.TODO(), pod.Namespace, pod.PodName, c.Name, defaultLogTailLines)
			cld := ContainerLogsData{
				PodName:           logInfo.PodName,
				Namespace:         logInfo.Namespace,
				ContainerName:     logInfo.ContainerName,
				LineLimit:         logInfo.LineLimit,
				LinesRetrieved:    logInfo.LinesRetrieved,
				Lines:             logInfo.Lines,
				Unavailable:       logInfo.Unavailable,
				UnavailableReason: logInfo.UnavailableReason,
				APIError:          logInfo.APIError,
				APIErrorMsg:       logInfo.APIErrorMsg,
			}
			podPkg.Evidence = append(podPkg.Evidence, EvidenceItem{
				ID:     nextID(),
				Source: SourceContainerLogs,
				Data:   cld,
			})
		}

		pkg.Pods = append(pkg.Pods, podPkg)
	}

	return pkg
}


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

	var roundTrip EvidencePackage
	if err := json.Unmarshal(jsonBytes, &roundTrip); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: evidence package JSON failed to parse back: %v\n", err)
	}
}

// ── Feature 7: Deterministic Diagnosis Engine ───────────────────────────────

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

func diagnosePod(podPkg PodEvidencePackage) PodDiagnosis {
	// Rule 1: OOM_KILLED
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

	// Rule 2: IMAGE_PULL_FAILURE
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

	// Rule 3: NODE_NOT_READY
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

	// Default: UNKNOWN
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
	prov := getProvider()

	// Feature 1: list nodes (human-readable)
	listNodes(prov)

	// Feature 2 + 3 + 4: pod status, events, logs (human-readable)
	collectPods(prov, "default")

	// Feature 5: build and print unified evidence package
	pkg := buildEvidencePackage(prov, "default")
	printEvidenceSummary(pkg)

	// Feature 7: deterministic diagnosis engine
	diagReport := diagnoseEvidence(pkg)
	printDiagnosisSummary(diagReport)

	// Feature 8: diagnostic dataset builder
	generateDatasets()
	total, splitCounts, emptyAns, unknownDiag, realCount, reasoningCount := validateDatasets()
	printDatasetReport(total, splitCounts, emptyAns, unknownDiag, realCount, reasoningCount)
}
