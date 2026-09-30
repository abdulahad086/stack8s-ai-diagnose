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

// PodVolumeInfo represents a volume declared in a Pod specification.
type PodVolumeInfo struct {
	Name      string `json:"name"`
	Type      string `json:"type"`                 // e.g. "persistentVolumeClaim", "emptyDir", "configMap", "secret", "hostPath", "other"
	ClaimName string `json:"claim_name,omitempty"` // PVC name if Type is "persistentVolumeClaim"
	ReadOnly  bool   `json:"read_only,omitempty"`
}

// EnvVarRef represents a single environment variable whose value is sourced
// from a ConfigMap or Secret reference. Values are NEVER collected.
type EnvVarRef struct {
	ContainerName string `json:"container_name"`
	EnvName       string `json:"env_name"`
	SourceType    string `json:"source_type"`  // "configMapKeyRef", "secretKeyRef", "configMapEnvFrom", "secretEnvFrom"
	ResourceName  string `json:"resource_name"` // ConfigMap or Secret name
	ResourceKey   string `json:"resource_key,omitempty"` // specific key (empty for envFrom)
	Optional      bool   `json:"optional,omitempty"`
}

// ConfigVolumeRef represents a volume in a Pod backed by a ConfigMap or Secret.
type ConfigVolumeRef struct {
	VolumeName   string `json:"volume_name"`
	SourceType   string `json:"source_type"`   // "configMap" or "secret"
	ResourceName string `json:"resource_name"` // ConfigMap or Secret name
	Optional     bool   `json:"optional,omitempty"`
}

// PodInfo represents the status, networking, storage volumes, owner references, and containers of a single pod.
type PodInfo struct {
	PodName              string               `json:"pod_name"`
	Namespace            string               `json:"namespace"`
	Phase                string               `json:"phase"`
	NodeName             string               `json:"node_name"`
	PodIP                string               `json:"pod_ip,omitempty"`
	Labels               map[string]string    `json:"labels,omitempty"`
	CreatedAt            string               `json:"created_at"`
	OwnerReferences      []OwnerReferenceInfo `json:"owner_references,omitempty"`
	Volumes              []PodVolumeInfo      `json:"volumes,omitempty"`
	Containers           []ContainerInfo      `json:"containers"`
	// Feature 15: configuration/identity references
	ServiceAccountName   string               `json:"service_account_name,omitempty"`
	ImagePullSecretNames []string             `json:"image_pull_secret_names,omitempty"`
	EnvRefs              []EnvVarRef          `json:"env_refs,omitempty"`
	ConfigVolumeRefs     []ConfigVolumeRef    `json:"config_volume_refs,omitempty"`
}

// ── Configuration & Identity Domain Types (Feature 15) ──────────────────────

// ConfigMapInfo holds ConfigMap metadata and key names.
// ConfigMap values are intentionally NOT collected.
type ConfigMapInfo struct {
	Name        string   `json:"name"`
	Namespace   string   `json:"namespace"`
	Keys        []string `json:"keys,omitempty"`
	Immutable   bool     `json:"immutable,omitempty"`
	CreatedAt   string   `json:"created_at,omitempty"`
	Unavailable bool     `json:"unavailable,omitempty"`
	Reason      string   `json:"reason,omitempty"`
}

// SecretInfo holds Secret metadata and key names ONLY.
// SECRET VALUES ARE NEVER COLLECTED — not in data, not in stringData, not anywhere.
type SecretInfo struct {
	Name        string   `json:"name"`
	Namespace   string   `json:"namespace"`
	Type        string   `json:"type,omitempty"` // e.g. "Opaque", "kubernetes.io/dockerconfigjson"
	Keys        []string `json:"keys,omitempty"` // key NAMES only — values never collected
	Immutable   bool     `json:"immutable,omitempty"`
	CreatedAt   string   `json:"created_at,omitempty"`
	Unavailable bool     `json:"unavailable,omitempty"`
	Reason      string   `json:"reason,omitempty"`
	// ValuesCollected is always false — included explicitly to document the security boundary.
	ValuesCollected bool `json:"values_collected"`
}

// ServiceAccountInfo holds a Kubernetes ServiceAccount metadata.
type ServiceAccountInfo struct {
	Name                     string   `json:"name"`
	Namespace                string   `json:"namespace"`
	ImagePullSecrets         []string `json:"image_pull_secrets,omitempty"` // secret names referenced by SA
	AutomountServiceAcctToken *bool   `json:"automount_service_account_token,omitempty"`
	CreatedAt                string   `json:"created_at,omitempty"`
	Unavailable              bool     `json:"unavailable,omitempty"`
	Reason                   string   `json:"reason,omitempty"`
}

// ── Workload & Controller Domain Types (Feature 14) ──────────────────────────

// OwnerReferenceInfo represents an owner reference on a Kubernetes object.
type OwnerReferenceInfo struct {
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	UID        string `json:"uid,omitempty"`
	Controller bool   `json:"controller,omitempty"`
}

// WorkloadCondition represents a condition on a workload controller (e.g. Deployment, StatefulSet, Job).
type WorkloadCondition struct {
	Type               string `json:"type"`
	Status             string `json:"status"` // "True", "False", "Unknown"
	Reason             string `json:"reason,omitempty"`
	Message            string `json:"message,omitempty"`
	LastTransitionTime string `json:"last_transition_time,omitempty"`
}

// DeploymentInfo represents a Kubernetes Deployment status and strategy.
type DeploymentInfo struct {
	Name                string              `json:"name"`
	Namespace           string              `json:"namespace"`
	DesiredReplicas     int32               `json:"desired_replicas"`
	CurrentReplicas     int32               `json:"current_replicas"`
	ReadyReplicas       int32               `json:"ready_replicas"`
	AvailableReplicas   int32               `json:"available_replicas"`
	UpdatedReplicas     int32               `json:"updated_replicas"`
	UnavailableReplicas int32               `json:"unavailable_replicas"`
	ObservedGeneration  int64               `json:"observed_generation"`
	Strategy            string              `json:"strategy,omitempty"` // "RollingUpdate", "Recreate"
	CreatedAt           string              `json:"created_at,omitempty"`
	Conditions          []WorkloadCondition `json:"conditions,omitempty"`
	Unavailable         bool                `json:"unavailable,omitempty"`
	Reason              string              `json:"reason,omitempty"`
}

// ReplicaSetInfo represents a Kubernetes ReplicaSet status.
type ReplicaSetInfo struct {
	Name                 string               `json:"name"`
	Namespace            string               `json:"namespace"`
	DesiredReplicas      int32                `json:"desired_replicas"`
	CurrentReplicas      int32                `json:"current_replicas"`
	ReadyReplicas        int32                `json:"ready_replicas"`
	AvailableReplicas    int32                `json:"available_replicas"`
	FullyLabeledReplicas int32                `json:"fully_labeled_replicas,omitempty"`
	ObservedGeneration   int64                `json:"observed_generation"`
	OwnerReferences      []OwnerReferenceInfo `json:"owner_references,omitempty"`
	Selector             map[string]string    `json:"selector,omitempty"`
	CreatedAt            string               `json:"created_at,omitempty"`
	Unavailable          bool                 `json:"unavailable,omitempty"`
	Reason               string               `json:"reason,omitempty"`
}

// StatefulSetInfo represents a Kubernetes StatefulSet status.
type StatefulSetInfo struct {
	Name               string              `json:"name"`
	Namespace          string              `json:"namespace"`
	DesiredReplicas    int32               `json:"desired_replicas"`
	CurrentReplicas    int32               `json:"current_replicas"`
	ReadyReplicas      int32               `json:"ready_replicas"`
	UpdatedReplicas    int32               `json:"updated_replicas"`
	CurrentRevision    string              `json:"current_revision,omitempty"`
	UpdateRevision     string              `json:"update_revision,omitempty"`
	ServiceName        string              `json:"service_name,omitempty"`
	ObservedGeneration int64               `json:"observed_generation"`
	UpdateStrategy     string              `json:"update_strategy,omitempty"` // "RollingUpdate", "OnDelete"
	CreatedAt          string              `json:"created_at,omitempty"`
	Conditions         []WorkloadCondition `json:"conditions,omitempty"`
	Unavailable        bool                `json:"unavailable,omitempty"`
	Reason             string              `json:"reason,omitempty"`
}

// DaemonSetInfo represents a Kubernetes DaemonSet status.
type DaemonSetInfo struct {
	Name               string `json:"name"`
	Namespace          string `json:"namespace"`
	DesiredScheduled   int32  `json:"desired_scheduled"`
	CurrentScheduled   int32  `json:"current_scheduled"`
	NumberReady        int32  `json:"number_ready"`
	NumberAvailable    int32  `json:"number_available"`
	NumberUnavailable  int32  `json:"number_unavailable"`
	NumberMisscheduled int32  `json:"number_misscheduled"`
	UpdatedScheduled   int32  `json:"updated_scheduled"`
	ObservedGeneration int64  `json:"observed_generation"`
	CreatedAt          string `json:"created_at,omitempty"`
	Unavailable        bool   `json:"unavailable,omitempty"`
	Reason             string `json:"reason,omitempty"`
}

// JobInfo represents a Kubernetes Job status.
type JobInfo struct {
	Name            string               `json:"name"`
	Namespace       string               `json:"namespace"`
	Completions     int32                `json:"completions,omitempty"`
	Parallelism     int32                `json:"parallelism,omitempty"`
	Active          int32                `json:"active"`
	Succeeded       int32                `json:"succeeded"`
	Failed          int32                `json:"failed"`
	StartTime       string               `json:"start_time,omitempty"`
	CompletionTime  string               `json:"completion_time,omitempty"`
	BackoffLimit    int32                `json:"backoff_limit,omitempty"`
	OwnerReferences []OwnerReferenceInfo `json:"owner_references,omitempty"`
	CreatedAt       string               `json:"created_at,omitempty"`
	Conditions      []WorkloadCondition  `json:"conditions,omitempty"`
	Unavailable     bool                 `json:"unavailable,omitempty"`
	Reason          string               `json:"reason,omitempty"`
}

// CronJobInfo represents a Kubernetes CronJob status.
type CronJobInfo struct {
	Name               string `json:"name"`
	Namespace          string `json:"namespace"`
	Schedule           string `json:"schedule"`
	Suspend            bool   `json:"suspend"`
	ActiveJobsCount    int    `json:"active_jobs_count"`
	LastScheduleTime   string `json:"last_schedule_time,omitempty"`
	LastSuccessfulTime string `json:"last_successful_time,omitempty"`
	ConcurrencyPolicy  string `json:"concurrency_policy,omitempty"` // "Allow", "Forbid", "Replace"
	CreatedAt          string `json:"created_at,omitempty"`
	Unavailable        bool   `json:"unavailable,omitempty"`
	Reason             string `json:"reason,omitempty"`
}

// ── Storage Domain Types (Feature 13) ────────────────────────────────────────

// PersistentVolumeClaimInfo represents a Kubernetes PersistentVolumeClaim (PVC).
type PersistentVolumeClaimInfo struct {
	Name             string   `json:"name"`
	Namespace        string   `json:"namespace"`
	Phase            string   `json:"phase"`                       // "Bound", "Pending", "Lost", "NotFound"
	StorageClassName string   `json:"storage_class,omitempty"`
	VolumeName       string   `json:"volume_name,omitempty"`       // Bound PV name
	RequestedStorage string   `json:"requested_storage,omitempty"` // e.g. "1Gi"
	AccessModes      []string `json:"access_modes,omitempty"`      // e.g. ["ReadWriteOnce"]
	VolumeMode       string   `json:"volume_mode,omitempty"`       // e.g. "Filesystem"
	CreatedAt        string   `json:"created_at,omitempty"`
	Unavailable      bool     `json:"unavailable,omitempty"`
	Reason           string   `json:"reason,omitempty"`
}

// PersistentVolumeInfo represents a Kubernetes PersistentVolume (PV).
type PersistentVolumeInfo struct {
	Name             string   `json:"name"`
	Phase            string   `json:"phase"`                       // "Bound", "Available", "Released", "Failed"
	Capacity         string   `json:"capacity,omitempty"`          // e.g. "1Gi"
	StorageClassName string   `json:"storage_class,omitempty"`
	AccessModes      []string `json:"access_modes,omitempty"`      // e.g. ["ReadWriteOnce"]
	VolumeMode       string   `json:"volume_mode,omitempty"`       // e.g. "Filesystem"
	ClaimRef         string   `json:"claim_ref,omitempty"`         // e.g. "default/my-data"
	ReclaimPolicy    string   `json:"reclaim_policy,omitempty"`    // e.g. "Delete", "Retain"
	PluginSource     string   `json:"plugin_source,omitempty"`     // e.g. "hostPath", "csi", "local"
}

// StorageClassInfo represents a Kubernetes StorageClass.
type StorageClassInfo struct {
	Name                 string `json:"name"`
	Provisioner          string `json:"provisioner"`
	ReclaimPolicy        string `json:"reclaim_policy,omitempty"`        // e.g. "Delete", "Retain"
	VolumeBindingMode    string `json:"volume_binding_mode,omitempty"`   // e.g. "Immediate", "WaitForFirstConsumer"
	AllowVolumeExpansion bool   `json:"allow_volume_expansion,omitempty"`
	IsDefault            bool   `json:"is_default,omitempty"`
}

// ── Network & Service Domain Types (Feature 12) ─────────────────────────────

// ServicePortInfo represents a single port exposed by a Kubernetes Service.
type ServicePortInfo struct {
	Name       string `json:"name,omitempty"`
	Protocol   string `json:"protocol"` // "TCP", "UDP", "SCTP"
	Port       int32  `json:"port"`     // Service Port
	TargetPort string `json:"target_port"` // Target Port (number or name)
	NodePort   int32  `json:"node_port,omitempty"` // NodePort if Type is NodePort or LoadBalancer
}

// EndpointAddressInfo represents an IP endpoint address backing a Service.
type EndpointAddressInfo struct {
	IP        string `json:"ip"`
	NodeName  string `json:"node_name,omitempty"`
	PodName   string `json:"pod_name,omitempty"`
	TargetRef string `json:"target_ref,omitempty"` // e.g. "Pod/test-nginx"
	Ready     bool   `json:"ready"`
}

// EndpointPortInfo represents a port exposed on an endpoint address.
type EndpointPortInfo struct {
	Name     string `json:"name,omitempty"`
	Port     int32  `json:"port"`
	Protocol string `json:"protocol"`
}

// EndpointSubsetInfo represents a subset of addresses and ports for a Service.
type EndpointSubsetInfo struct {
	Addresses         []EndpointAddressInfo `json:"addresses"`
	Ports             []EndpointPortInfo    `json:"ports"`
	ReadyAddressCount int                   `json:"ready_address_count"`
	NotReadyAddrCount int                   `json:"not_ready_address_count"`
}

// ServiceInfo represents a Kubernetes Service and its backing endpoints.
type ServiceInfo struct {
	Name        string               `json:"name"`
	Namespace   string               `json:"namespace"`
	Type        string               `json:"type"` // "ClusterIP", "NodePort", "LoadBalancer", "ExternalName"
	ClusterIP   string               `json:"cluster_ip,omitempty"`
	ExternalIPs []string             `json:"external_ips,omitempty"`
	Selector    map[string]string    `json:"selector,omitempty"`
	CreatedAt   string               `json:"created_at,omitempty"`
	Ports       []ServicePortInfo    `json:"ports"`
	Subsets     []EndpointSubsetInfo `json:"subsets,omitempty"`
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

	// GetServices lists all services in the given namespace with ports, selectors, and endpoints.
	GetServices(ctx context.Context, namespace string) ([]ServiceInfo, error)

	// GetService returns detailed information for a specific service by name.
	GetService(ctx context.Context, namespace, name string) (ServiceInfo, error)

	// GetPersistentVolumeClaim returns detailed information for a PVC in a namespace.
	GetPersistentVolumeClaim(ctx context.Context, namespace, name string) (PersistentVolumeClaimInfo, error)

	// GetPersistentVolume returns detailed information for a PV by name.
	GetPersistentVolume(ctx context.Context, name string) (PersistentVolumeInfo, error)

	// GetStorageClass returns detailed information for a StorageClass by name.
	GetStorageClass(ctx context.Context, name string) (StorageClassInfo, error)

	// GetStorageClasses lists all StorageClasses in the cluster.
	GetStorageClasses(ctx context.Context) ([]StorageClassInfo, error)

	// GetDeployment returns detailed information for a Deployment by namespace and name.
	GetDeployment(ctx context.Context, namespace, name string) (DeploymentInfo, error)

	// GetReplicaSet returns detailed information for a ReplicaSet by namespace and name.
	GetReplicaSet(ctx context.Context, namespace, name string) (ReplicaSetInfo, error)

	// GetStatefulSet returns detailed information for a StatefulSet by namespace and name.
	GetStatefulSet(ctx context.Context, namespace, name string) (StatefulSetInfo, error)

	// GetDaemonSet returns detailed information for a DaemonSet by namespace and name.
	GetDaemonSet(ctx context.Context, namespace, name string) (DaemonSetInfo, error)

	// GetJob returns detailed information for a Job by namespace and name.
	GetJob(ctx context.Context, namespace, name string) (JobInfo, error)

	// GetCronJob returns detailed information for a CronJob by namespace and name.
	GetCronJob(ctx context.Context, namespace, name string) (CronJobInfo, error)

	// GetPodEvents lists all events for a specific pod.
	GetPodEvents(ctx context.Context, namespace, podName string) ([]EventInfo, error)

	// GetContainerLogs retrieves logs for a container in a pod bounded by tailLines.
	GetContainerLogs(ctx context.Context, namespace, podName, containerName string, tailLines int64) (ContainerLogInfo, error)

	// GetConfigMap returns ConfigMap metadata and key names (NOT values) for a ConfigMap in a namespace.
	GetConfigMap(ctx context.Context, namespace, name string) (ConfigMapInfo, error)

	// GetSecretMetadata returns Secret metadata and key names ONLY. Secret values are NEVER returned.
	GetSecretMetadata(ctx context.Context, namespace, name string) (SecretInfo, error)

	// GetServiceAccount returns ServiceAccount metadata for a given namespace and name.
	GetServiceAccount(ctx context.Context, namespace, name string) (ServiceAccountInfo, error)
}
