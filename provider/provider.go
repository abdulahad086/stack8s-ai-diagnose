package provider

import (
	"context"
	"fmt"
)

// ── Application / Domain Types ───────────────────────────────────────────────
//
// These domain types isolate the diagnostic system from infrastructure-specific
// client libraries (such as client-go / corev1).

// NodeInfo represents a cluster node.
type NodeInfo struct {
	Name   string `json:"name"`
	Status string `json:"status"` // e.g. "Ready", "NotReady"
}

// ContainerInfo represents the state of a single container in a pod.
type ContainerInfo struct {
	Name               string `json:"name"`
	Image              string `json:"image"`
	Ready              bool   `json:"ready"`
	RestartCount       int32  `json:"restart_count"`
	State              string `json:"state"` // Human-readable state string
	HasLastTermination bool   `json:"has_last_termination"`
	LastTermReason     string `json:"last_termination_reason,omitempty"`
	LastTermExitCode   int32  `json:"last_termination_exit_code,omitempty"`
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
//
// InfrastructureProvider abstracts the infrastructure query operations needed
// by the stack8s-ai-diagnose engine.
//
// Implementations:
//   - LocalKubernetesProvider: Queries local Kubernetes cluster via client-go.
//   - Stack8sProvider (Future): Will query actual Stack8s infrastructure backend.

type InfrastructureProvider interface {
	// Name returns the provider identifier (e.g. "local-kubernetes").
	Name() string

	// ClusterHost returns the host/URL of the cluster host.
	ClusterHost() string

	// GetNodes lists all nodes in the cluster.
	GetNodes(ctx context.Context) ([]NodeInfo, error)

	// GetPods lists all pods in the given namespace.
	GetPods(ctx context.Context, namespace string) ([]PodInfo, error)

	// GetPodEvents lists all events for a specific pod.
	GetPodEvents(ctx context.Context, namespace, podName string) ([]EventInfo, error)

	// GetContainerLogs retrieves logs for a container in a pod bounded by tailLines.
	GetContainerLogs(ctx context.Context, namespace, podName, containerName string, tailLines int64) (ContainerLogInfo, error)
}
