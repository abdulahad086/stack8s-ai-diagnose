package provider

import (
	"context"
	"fmt"
)

// ── Application / Domain Types ───────────────────────────────────────────────
//
// These domain types isolate the diagnostic system from infrastructure-specific
// client libraries (such as client-go / corev1).

// NodeResources holds normalized Kubernetes node resource quantities.
//
// Semantics:
//   - Capacity: Total physical/virtual resource reported by the Node hardware.
//   - Allocatable: Resource amount Kubernetes makes available for workload scheduling.
//
// Note: Capacity and Allocatable represent Kubernetes configuration/state limits.
// They do NOT represent real-time runtime utilization or "free memory".
type NodeResources struct {
	CPUCapacity        string `json:"cpu_capacity"`
	CPUAllocatable     string `json:"cpu_allocatable"`
	MemoryCapacity     string `json:"memory_capacity"`
	MemoryAllocatable  string `json:"memory_allocatable"`
	StorageCapacity    string `json:"storage_capacity,omitempty"`
	StorageAllocatable string `json:"storage_allocatable,omitempty"`
}

// NodeGPUResources represents GPU capacity, allocatable, and metadata for a Node.
//
// Semantics:
//   - Capacity / Allocatable: Kubernetes resource quantities reported for extended GPU resources (e.g. nvidia.com/gpu).
//   - Note: These quantities represent cluster configuration state, NOT real-time runtime GPU utilization.
type NodeGPUResources struct {
	NodeName           string `json:"node_name"`
	GPUAvailable       bool   `json:"gpu_available"`
	GPUResourceExposed bool   `json:"gpu_resource_exposed"`
	ResourceName       string `json:"resource_name,omitempty"` // e.g. "nvidia.com/gpu", "amd.com/gpu"
	Model              string `json:"model,omitempty"`         // Vendor/model if exposed in labels
	Capacity           string `json:"capacity,omitempty"`     // Total GPU capacity reported by node
	Allocatable        string `json:"allocatable,omitempty"`  // GPU amount allocatable to workloads
}

// NodeSystemInfo holds node operating system and kernel metadata.
type NodeSystemInfo struct {
	KubeletVersion   string `json:"kubelet_version,omitempty"`
	OperatingSystem  string `json:"operating_system,omitempty"`
	Architecture     string `json:"architecture,omitempty"`
	KernelVersion    string `json:"kernel_version,omitempty"`
	ContainerRuntime string `json:"container_runtime,omitempty"`
}

// NodeInfo represents a cluster node.
type NodeInfo struct {
	Name       string         `json:"name"`
	Status     string         `json:"status"` // "Ready", "NotReady", "Unknown"
	CreatedAt  string         `json:"created_at,omitempty"`
	Resources  NodeResources  `json:"resources"`
	SystemInfo NodeSystemInfo `json:"system_info"`
}

// ContainerInfo represents the state and resource requirements of a single container in a pod.
//
// Semantics:
//   - Request: Minimum resource guaranteed/reserved for the container by Kubernetes scheduler.
//   - Limit: Maximum resource cap enforced for the container.
type ContainerInfo struct {
	Name               string `json:"name"`
	Image              string `json:"image"`
	Ready              bool   `json:"ready"`
	RestartCount       int32  `json:"restart_count"`
	State              string `json:"state"` // Human-readable state string
	HasLastTermination bool   `json:"has_last_termination"`
	LastTermReason     string `json:"last_termination_reason,omitempty"`
	LastTermExitCode   int32  `json:"last_termination_exit_code,omitempty"`

	// Resource Requests and Limits (from workload configuration)
	CPURequest    string `json:"cpu_request,omitempty"`
	CPULimit      string `json:"cpu_limit,omitempty"`
	MemoryRequest string `json:"memory_request,omitempty"`
	MemoryLimit   string `json:"memory_limit,omitempty"`
	GPURequest    string `json:"gpu_request,omitempty"`
	GPULimit      string `json:"gpu_limit,omitempty"`
}

// PodInfo represents the status and containers of a single pod.
type PodInfo struct {
	PodName    string          `json:"pod_name"`
	Namespace  string          `json:"namespace"`
	Phase      string          `json:"phase"`
	NodeName   string          `json:"node_name"`
	CreatedAt  string          `json:"created_at"`
	Containers []ContainerInfo `json:"containers"`
}

// EventInfo represents a normalized event associated with a workload.
type EventInfo struct {
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

// ContainerLogInfo represents log output or log availability for a container.
type ContainerLogInfo struct {
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

// ── Application-Level Errors ──────────────────────────────────────────────────

type ProviderError struct {
	Code    string
	Message string
	Err     error
}

func (e *ProviderError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("provider error [%s]: %s (%v)", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("provider error [%s]: %s", e.Code, e.Message)
}

func (e *ProviderError) Unwrap() error {
	return e.Err
}

// Standard Provider Error Codes
const (
	ErrCodeUnavailable      = "PROVIDER_UNAVAILABLE"
	ErrCodeResourceNotFound = "RESOURCE_NOT_FOUND"
	ErrCodeCollectionFailed = "COLLECTION_FAILED"
	ErrCodeAccessDenied     = "ACCESS_DENIED"
)

// ── Infrastructure Provider Interface ────────────────────────────────────────

type InfrastructureProvider interface {
	// Name returns the provider identifier (e.g. "local-kubernetes").
	Name() string

	// ClusterHost returns the host/URL of the cluster host.
	ClusterHost() string

	// GetNodes lists all nodes in the cluster with capacity and status.
	GetNodes(ctx context.Context) ([]NodeInfo, error)

	// GetNode returns detailed information for a specific node by name.
	GetNode(ctx context.Context, name string) (NodeInfo, error)

	// GetNodeGPUResources returns GPU resource capacity, allocatable, and status for a node.
	GetNodeGPUResources(ctx context.Context, name string) (NodeGPUResources, error)

	// GetPods lists all pods in the given namespace with container state & resource limits.
	GetPods(ctx context.Context, namespace string) ([]PodInfo, error)

	// GetPodEvents lists all events for a specific pod.
	GetPodEvents(ctx context.Context, namespace, podName string) ([]EventInfo, error)

	// GetContainerLogs retrieves logs for a container in a pod bounded by tailLines.
	GetContainerLogs(ctx context.Context, namespace, podName, containerName string, tailLines int64) (ContainerLogInfo, error)
}
