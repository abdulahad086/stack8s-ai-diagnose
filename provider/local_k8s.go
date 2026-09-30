package provider

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// LocalKubernetesProvider implements InfrastructureProvider using k8s.io/client-go.
// All client-go types (corev1, metav1, rest.Config) are strictly isolated inside this struct/file.
type LocalKubernetesProvider struct {
	config    *rest.Config
	clientset *kubernetes.Clientset
}

// NewLocalKubernetesProvider initializes a REST config and typed clientset
// respecting $KUBECONFIG or falling back to ~/.kube/config.
func NewLocalKubernetesProvider() (*LocalKubernetesProvider, error) {
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, &ProviderError{
				Code:    ErrCodeUnavailable,
				Message: "cannot determine home directory for kubeconfig",
				Err:     err,
			}
		}
		kubeconfig = filepath.Join(home, ".kube", "config")
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, &ProviderError{
			Code:    ErrCodeUnavailable,
			Message: fmt.Sprintf("failed to load kubeconfig from %s", kubeconfig),
			Err:     err,
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, &ProviderError{
			Code:    ErrCodeUnavailable,
			Message: "failed to create Kubernetes client",
			Err:     err,
		}
	}

	return &LocalKubernetesProvider{
		config:    config,
		clientset: clientset,
	}, nil
}

func (p *LocalKubernetesProvider) Name() string {
	return "local-kubernetes"
}

func (p *LocalKubernetesProvider) ClusterHost() string {
	if p.config != nil {
		return p.config.Host
	}
	return "unknown-cluster"
}

// mapNodeInfo converts a raw corev1.Node into a domain NodeInfo object.
func mapNodeInfo(node corev1.Node) NodeInfo {
	status := "Unknown"
	for _, cond := range node.Status.Conditions {
		if cond.Type == corev1.NodeReady {
			switch cond.Status {
			case corev1.ConditionTrue:
				status = "Ready"
			case corev1.ConditionFalse:
				status = "NotReady"
			case corev1.ConditionUnknown:
				status = "Unknown"
			}
			break
		}
	}

	res := NodeResources{}
	if cpuCap, ok := node.Status.Capacity[corev1.ResourceCPU]; ok {
		res.CPUCapacity = cpuCap.String()
	}
	if cpuAlloc, ok := node.Status.Allocatable[corev1.ResourceCPU]; ok {
		res.CPUAllocatable = cpuAlloc.String()
	}
	if memCap, ok := node.Status.Capacity[corev1.ResourceMemory]; ok {
		res.MemoryCapacity = memCap.String()
	}
	if memAlloc, ok := node.Status.Allocatable[corev1.ResourceMemory]; ok {
		res.MemoryAllocatable = memAlloc.String()
	}
	if storageCap, ok := node.Status.Capacity[corev1.ResourceEphemeralStorage]; ok {
		res.StorageCapacity = storageCap.String()
	}
	if storageAlloc, ok := node.Status.Allocatable[corev1.ResourceEphemeralStorage]; ok {
		res.StorageAllocatable = storageAlloc.String()
	}

	sysInfo := NodeSystemInfo{
		KubeletVersion:   node.Status.NodeInfo.KubeletVersion,
		OperatingSystem:  node.Status.NodeInfo.OperatingSystem,
		Architecture:     node.Status.NodeInfo.Architecture,
		KernelVersion:    node.Status.NodeInfo.KernelVersion,
		ContainerRuntime: node.Status.NodeInfo.ContainerRuntimeVersion,
	}

	var createdAt string
	if !node.CreationTimestamp.IsZero() {
		createdAt = node.CreationTimestamp.Format(time.RFC3339)
	}

	return NodeInfo{
		Name:       node.Name,
		Status:     status,
		CreatedAt:  createdAt,
		Resources:  res,
		SystemInfo: sysInfo,
	}
}

// GetNodes retrieves all cluster nodes and maps them to domain NodeInfo.
func (p *LocalKubernetesProvider) GetNodes(ctx context.Context) ([]NodeInfo, error) {
	nodes, err := p.clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, &ProviderError{
			Code:    ErrCodeCollectionFailed,
			Message: "failed to list nodes",
			Err:     err,
		}
	}

	result := make([]NodeInfo, 0, len(nodes.Items))
	for _, node := range nodes.Items {
		result = append(result, mapNodeInfo(node))
	}
	return result, nil
}

// GetNode retrieves detailed information for a specific node by name.
func (p *LocalKubernetesProvider) GetNode(ctx context.Context, name string) (NodeInfo, error) {
	if name == "" || name == "<not yet scheduled>" || name == "<none>" {
		return NodeInfo{}, &ProviderError{
			Code:    ErrCodeResourceNotFound,
			Message: fmt.Sprintf("invalid node name %q", name),
		}
	}

	node, err := p.clientset.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return NodeInfo{}, &ProviderError{
			Code:    ErrCodeResourceNotFound,
			Message: fmt.Sprintf("node %q not found", name),
			Err:     err,
		}
	}
	return mapNodeInfo(*node), nil
}

// mapNodeGPUResources converts raw corev1.Node GPU capacity and allocatable extended resources into domain NodeGPUResources.
func mapNodeGPUResources(node corev1.Node) NodeGPUResources {
	res := NodeGPUResources{
		NodeName:           node.Name,
		GPUAvailable:       false,
		GPUResourceExposed: false,
	}

	gpuKeys := []string{"nvidia.com/gpu", "amd.com/gpu", "intel.com/gpu", "gpu.intel.com/i915"}
	var matchedResource corev1.ResourceName
	var found bool

	// Search node status capacity for GPU extended resource keys
	for k := range node.Status.Capacity {
		kStr := string(k)
		for _, gKey := range gpuKeys {
			if strings.EqualFold(kStr, gKey) {
				matchedResource = k
				found = true
				break
			}
		}
		if found {
			break
		}
		if strings.Contains(strings.ToLower(kStr), "gpu") {
			matchedResource = k
			found = true
			break
		}
	}

	if found {
		res.GPUResourceExposed = true
		res.ResourceName = string(matchedResource)

		if capQty, ok := node.Status.Capacity[matchedResource]; ok {
			res.Capacity = capQty.String()
		}
		if allocQty, ok := node.Status.Allocatable[matchedResource]; ok {
			res.Allocatable = allocQty.String()
			if !allocQty.IsZero() && allocQty.Value() > 0 {
				res.GPUAvailable = true
			}
		}

		// Read GPU product/model from node labels if exposed by device plugin or Node Feature Discovery (NFD)
		for lKey, lVal := range node.Labels {
			lKeyLower := strings.ToLower(lKey)
			if strings.Contains(lKeyLower, "gpu.product") ||
				strings.Contains(lKeyLower, "gpu.machine") ||
				strings.Contains(lKeyLower, "gpu.family") ||
				strings.Contains(lKeyLower, "gpu.model") {
				res.Model = lVal
				break
			}
		}
		if res.Model == "" {
			res.Model = "unknown"
		}
	}

	return res
}

// GetNodeGPUResources returns GPU resource capacity, allocatable, and status for a specific node.
func (p *LocalKubernetesProvider) GetNodeGPUResources(ctx context.Context, name string) (NodeGPUResources, error) {
	if name == "" || name == "<not yet scheduled>" || name == "<none>" {
		return NodeGPUResources{
			NodeName:           name,
			GPUAvailable:       false,
			GPUResourceExposed: false,
		}, nil
	}

	node, err := p.clientset.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return NodeGPUResources{
			NodeName:           name,
			GPUAvailable:       false,
			GPUResourceExposed: false,
		}, &ProviderError{
			Code:    ErrCodeResourceNotFound,
			Message: fmt.Sprintf("node %q not found for GPU query", name),
			Err:     err,
		}
	}

	return mapNodeGPUResources(*node), nil
}

// containerStateString formats a corev1.ContainerState into a domain string.
func containerStateString(state corev1.ContainerState) string {
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

// GetPods lists all pods in the namespace and maps them to domain PodInfo.
func (p *LocalKubernetesProvider) GetPods(ctx context.Context, namespace string) ([]PodInfo, error) {
	pods, err := p.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, &ProviderError{
			Code:    ErrCodeCollectionFailed,
			Message: fmt.Sprintf("failed to list pods in namespace %q", namespace),
			Err:     err,
		}
	}

	result := make([]PodInfo, 0, len(pods.Items))
	for _, pod := range pods.Items {
		nodeName := pod.Spec.NodeName
		if nodeName == "" {
			nodeName = "<not yet scheduled>"
		}

		statusMap := make(map[string]corev1.ContainerStatus, len(pod.Status.ContainerStatuses))
		for _, cs := range pod.Status.ContainerStatuses {
			statusMap[cs.Name] = cs
		}

		containers := make([]ContainerInfo, 0, len(pod.Spec.Containers))
		for _, c := range pod.Spec.Containers {
			ci := ContainerInfo{
				Name:  c.Name,
				Image: c.Image,
			}

			// Extract container resource requests and limits
			if cpuReq, ok := c.Resources.Requests[corev1.ResourceCPU]; ok {
				ci.CPURequest = cpuReq.String()
			}
			if cpuLim, ok := c.Resources.Limits[corev1.ResourceCPU]; ok {
				ci.CPULimit = cpuLim.String()
			}
			if memReq, ok := c.Resources.Requests[corev1.ResourceMemory]; ok {
				ci.MemoryRequest = memReq.String()
			}
			if memLim, ok := c.Resources.Limits[corev1.ResourceMemory]; ok {
				ci.MemoryLimit = memLim.String()
			}

			// Extract GPU requests and limits from extended resources
			for rName, qty := range c.Resources.Requests {
				rStr := string(rName)
				if strings.Contains(strings.ToLower(rStr), "gpu") {
					ci.GPURequest = fmt.Sprintf("%s: %s", rStr, qty.String())
					break
				}
			}
			for rName, qty := range c.Resources.Limits {
				rStr := string(rName)
				if strings.Contains(strings.ToLower(rStr), "gpu") {
					ci.GPULimit = fmt.Sprintf("%s: %s", rStr, qty.String())
					break
				}
			}

			if cs, ok := statusMap[c.Name]; ok {
				ci.Ready = cs.Ready
				ci.RestartCount = cs.RestartCount
				ci.State = containerStateString(cs.State)
				if cs.LastTerminationState.Terminated != nil {
					lt := cs.LastTerminationState.Terminated
					reason := lt.Reason
					if reason == "" {
						reason = "Unknown"
					}
					ci.HasLastTermination = true
					ci.LastTermReason = reason
					ci.LastTermExitCode = lt.ExitCode
				}
			} else {
				ci.State = "no status available"
			}
			containers = append(containers, ci)
		}

		var volumes []PodVolumeInfo
		for _, v := range pod.Spec.Volumes {
			volInfo := PodVolumeInfo{Name: v.Name}
			switch {
			case v.PersistentVolumeClaim != nil:
				volInfo.Type = "persistentVolumeClaim"
				volInfo.ClaimName = v.PersistentVolumeClaim.ClaimName
				volInfo.ReadOnly = v.PersistentVolumeClaim.ReadOnly
			case v.EmptyDir != nil:
				volInfo.Type = "emptyDir"
			case v.ConfigMap != nil:
				volInfo.Type = "configMap"
			case v.Secret != nil:
				volInfo.Type = "secret"
			case v.HostPath != nil:
				volInfo.Type = "hostPath"
			case v.Projected != nil:
				volInfo.Type = "projected"
			default:
				volInfo.Type = "other"
			}
			volumes = append(volumes, volInfo)
		}

		var ownerRefs []OwnerReferenceInfo
		for _, o := range pod.OwnerReferences {
			ctrl := false
			if o.Controller != nil {
				ctrl = *o.Controller
			}
			ownerRefs = append(ownerRefs, OwnerReferenceInfo{
				Kind:       o.Kind,
				Name:       o.Name,
				UID:        string(o.UID),
				Controller: ctrl,
			})
		}

		// ── Feature 15: configuration/identity references ─────────────────
		// Collect env references (configMapKeyRef, secretKeyRef, envFrom)
		var envRefs []EnvVarRef
		for _, c := range pod.Spec.Containers {
			// Individual env vars sourced from ConfigMap or Secret
			for _, e := range c.Env {
				if e.ValueFrom == nil {
					continue
				}
				if e.ValueFrom.ConfigMapKeyRef != nil {
					opt := false
					if e.ValueFrom.ConfigMapKeyRef.Optional != nil {
						opt = *e.ValueFrom.ConfigMapKeyRef.Optional
					}
					envRefs = append(envRefs, EnvVarRef{
						ContainerName: c.Name,
						EnvName:       e.Name,
						SourceType:    "configMapKeyRef",
						ResourceName:  e.ValueFrom.ConfigMapKeyRef.Name,
						ResourceKey:   e.ValueFrom.ConfigMapKeyRef.Key,
						Optional:      opt,
					})
				}
				if e.ValueFrom.SecretKeyRef != nil {
					opt := false
					if e.ValueFrom.SecretKeyRef.Optional != nil {
						opt = *e.ValueFrom.SecretKeyRef.Optional
					}
					envRefs = append(envRefs, EnvVarRef{
						ContainerName: c.Name,
						EnvName:       e.Name,
						SourceType:    "secretKeyRef",
						ResourceName:  e.ValueFrom.SecretKeyRef.Name,
						ResourceKey:   e.ValueFrom.SecretKeyRef.Key,
						Optional:      opt,
					})
				}
			}
			// envFrom sources
			for _, ef := range c.EnvFrom {
				if ef.ConfigMapRef != nil {
					opt := false
					if ef.ConfigMapRef.Optional != nil {
						opt = *ef.ConfigMapRef.Optional
					}
					envRefs = append(envRefs, EnvVarRef{
						ContainerName: c.Name,
						EnvName:       "(envFrom)",
						SourceType:    "configMapEnvFrom",
						ResourceName:  ef.ConfigMapRef.Name,
						Optional:      opt,
					})
				}
				if ef.SecretRef != nil {
					opt := false
					if ef.SecretRef.Optional != nil {
						opt = *ef.SecretRef.Optional
					}
					envRefs = append(envRefs, EnvVarRef{
						ContainerName: c.Name,
						EnvName:       "(envFrom)",
						SourceType:    "secretEnvFrom",
						ResourceName:  ef.SecretRef.Name,
						Optional:      opt,
					})
				}
			}
		}

		// Collect ConfigMap and Secret volume references
		var configVolRefs []ConfigVolumeRef
		for _, v := range pod.Spec.Volumes {
			if v.ConfigMap != nil {
				opt := false
				if v.ConfigMap.Optional != nil {
					opt = *v.ConfigMap.Optional
				}
				configVolRefs = append(configVolRefs, ConfigVolumeRef{
					VolumeName:   v.Name,
					SourceType:   "configMap",
					ResourceName: v.ConfigMap.Name,
					Optional:     opt,
				})
			}
			if v.Secret != nil {
				opt := false
				if v.Secret.Optional != nil {
					opt = *v.Secret.Optional
				}
				configVolRefs = append(configVolRefs, ConfigVolumeRef{
					VolumeName:   v.Name,
					SourceType:   "secret",
					ResourceName: v.Secret.SecretName,
					Optional:     opt,
				})
			}
		}

		// Collect imagePullSecret names
		var ipsNames []string
		for _, ips := range pod.Spec.ImagePullSecrets {
			ipsNames = append(ipsNames, ips.Name)
		}

		result = append(result, PodInfo{
			PodName:              pod.Name,
			Namespace:            pod.Namespace,
			Phase:                string(pod.Status.Phase),
			NodeName:             nodeName,
			PodIP:                pod.Status.PodIP,
			Labels:               pod.Labels,
			CreatedAt:            pod.CreationTimestamp.Format(time.RFC3339),
			OwnerReferences:      ownerRefs,
			Volumes:              volumes,
			Containers:           containers,
			ServiceAccountName:   pod.Spec.ServiceAccountName,
			ImagePullSecretNames: ipsNames,
			EnvRefs:              envRefs,
			ConfigVolumeRefs:     configVolRefs,
		})
	}

	return result, nil
}

func accessModesToStrings(modes []corev1.PersistentVolumeAccessMode) []string {
	res := make([]string, 0, len(modes))
	for _, m := range modes {
		res = append(res, string(m))
	}
	return res
}

func mapPVCInfo(pvc corev1.PersistentVolumeClaim) PersistentVolumeClaimInfo {
	scName := ""
	if pvc.Spec.StorageClassName != nil {
		scName = *pvc.Spec.StorageClassName
	}

	reqStorage := ""
	if qty, ok := pvc.Spec.Resources.Requests[corev1.ResourceStorage]; ok {
		reqStorage = qty.String()
	}

	volMode := ""
	if pvc.Spec.VolumeMode != nil {
		volMode = string(*pvc.Spec.VolumeMode)
	}

	var createdAt string
	if !pvc.CreationTimestamp.IsZero() {
		createdAt = pvc.CreationTimestamp.Format(time.RFC3339)
	}

	return PersistentVolumeClaimInfo{
		Name:             pvc.Name,
		Namespace:        pvc.Namespace,
		Phase:            string(pvc.Status.Phase),
		StorageClassName: scName,
		VolumeName:       pvc.Spec.VolumeName,
		RequestedStorage: reqStorage,
		AccessModes:      accessModesToStrings(pvc.Spec.AccessModes),
		VolumeMode:       volMode,
		CreatedAt:        createdAt,
	}
}

func mapPVInfo(pv corev1.PersistentVolume) PersistentVolumeInfo {
	scName := pv.Spec.StorageClassName

	capStorage := ""
	if qty, ok := pv.Spec.Capacity[corev1.ResourceStorage]; ok {
		capStorage = qty.String()
	}

	volMode := ""
	if pv.Spec.VolumeMode != nil {
		volMode = string(*pv.Spec.VolumeMode)
	}

	claimRef := ""
	if pv.Spec.ClaimRef != nil {
		claimRef = fmt.Sprintf("%s/%s", pv.Spec.ClaimRef.Namespace, pv.Spec.ClaimRef.Name)
	}

	pluginSource := "other"
	switch {
	case pv.Spec.HostPath != nil:
		pluginSource = "hostPath"
	case pv.Spec.CSI != nil:
		pluginSource = "csi"
	case pv.Spec.Local != nil:
		pluginSource = "local"
	case pv.Spec.NFS != nil:
		pluginSource = "nfs"
	}

	return PersistentVolumeInfo{
		Name:             pv.Name,
		Phase:            string(pv.Status.Phase),
		Capacity:         capStorage,
		StorageClassName: scName,
		AccessModes:      accessModesToStrings(pv.Spec.AccessModes),
		VolumeMode:       volMode,
		ClaimRef:         claimRef,
		ReclaimPolicy:    string(pv.Spec.PersistentVolumeReclaimPolicy),
		PluginSource:     pluginSource,
	}
}

func mapStorageClassInfo(sc storagev1.StorageClass) StorageClassInfo {
	reclaimPolicy := ""
	if sc.ReclaimPolicy != nil {
		reclaimPolicy = string(*sc.ReclaimPolicy)
	}

	bindingMode := ""
	if sc.VolumeBindingMode != nil {
		bindingMode = string(*sc.VolumeBindingMode)
	}

	allowExpansion := false
	if sc.AllowVolumeExpansion != nil {
		allowExpansion = *sc.AllowVolumeExpansion
	}

	isDefault := false
	if sc.Annotations != nil {
		if sc.Annotations["storageclass.kubernetes.io/is-default-class"] == "true" ||
			sc.Annotations["storageclass.beta.kubernetes.io/is-default-class"] == "true" {
			isDefault = true
		}
	}

	return StorageClassInfo{
		Name:                 sc.Name,
		Provisioner:          sc.Provisioner,
		ReclaimPolicy:        reclaimPolicy,
		VolumeBindingMode:    bindingMode,
		AllowVolumeExpansion: allowExpansion,
		IsDefault:            isDefault,
	}
}

// GetPersistentVolumeClaim returns detailed information for a PVC in a namespace.
func (p *LocalKubernetesProvider) GetPersistentVolumeClaim(ctx context.Context, namespace, name string) (PersistentVolumeClaimInfo, error) {
	pvc, err := p.clientset.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return PersistentVolumeClaimInfo{
			Name:        name,
			Namespace:   namespace,
			Phase:       "NotFound",
			Unavailable: true,
			Reason:      err.Error(),
		}, &ProviderError{
			Code:    ErrCodeResourceNotFound,
			Message: fmt.Sprintf("pvc %q not found in namespace %q", name, namespace),
			Err:     err,
		}
	}
	return mapPVCInfo(*pvc), nil
}

// GetPersistentVolume returns detailed information for a PV by name.
func (p *LocalKubernetesProvider) GetPersistentVolume(ctx context.Context, name string) (PersistentVolumeInfo, error) {
	pv, err := p.clientset.CoreV1().PersistentVolumes().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return PersistentVolumeInfo{
			Name:  name,
			Phase: "NotFound",
		}, &ProviderError{
			Code:    ErrCodeResourceNotFound,
			Message: fmt.Sprintf("pv %q not found", name),
			Err:     err,
		}
	}
	return mapPVInfo(*pv), nil
}

// GetStorageClass returns detailed information for a StorageClass by name.
func (p *LocalKubernetesProvider) GetStorageClass(ctx context.Context, name string) (StorageClassInfo, error) {
	sc, err := p.clientset.StorageV1().StorageClasses().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return StorageClassInfo{
			Name: name,
		}, &ProviderError{
			Code:    ErrCodeResourceNotFound,
			Message: fmt.Sprintf("storage class %q not found", name),
			Err:     err,
		}
	}
	return mapStorageClassInfo(*sc), nil
}

// GetStorageClasses lists all StorageClasses in the cluster.
func (p *LocalKubernetesProvider) GetStorageClasses(ctx context.Context) ([]StorageClassInfo, error) {
	scs, err := p.clientset.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, &ProviderError{
			Code:    ErrCodeCollectionFailed,
			Message: "failed to list storage classes",
			Err:     err,
		}
	}
	result := make([]StorageClassInfo, 0, len(scs.Items))
	for _, sc := range scs.Items {
		result = append(result, mapStorageClassInfo(sc))
	}
	return result, nil
}

func stringValue(sp *string) string {
	if sp == nil {
		return ""
	}
	return *sp
}

// mapServiceInfo converts raw corev1.Service and optional corev1.Endpoints into domain ServiceInfo.
func mapServiceInfo(service corev1.Service, endpoints *corev1.Endpoints) ServiceInfo {
	ports := make([]ServicePortInfo, 0, len(service.Spec.Ports))
	for _, p := range service.Spec.Ports {
		ports = append(ports, ServicePortInfo{
			Name:       p.Name,
			Protocol:   string(p.Protocol),
			Port:       p.Port,
			TargetPort: p.TargetPort.String(),
			NodePort:   p.NodePort,
		})
	}

	var subsets []EndpointSubsetInfo
	if endpoints != nil && len(endpoints.Subsets) > 0 {
		subsets = make([]EndpointSubsetInfo, 0, len(endpoints.Subsets))
		for _, sub := range endpoints.Subsets {
			addrs := make([]EndpointAddressInfo, 0, len(sub.Addresses)+len(sub.NotReadyAddresses))
			for _, a := range sub.Addresses {
				targetRef := ""
				podName := ""
				if a.TargetRef != nil {
					targetRef = fmt.Sprintf("%s/%s", a.TargetRef.Kind, a.TargetRef.Name)
					if a.TargetRef.Kind == "Pod" {
						podName = a.TargetRef.Name
					}
				}
				addrs = append(addrs, EndpointAddressInfo{
					IP:        a.IP,
					NodeName:  stringValue(a.NodeName),
					PodName:   podName,
					TargetRef: targetRef,
					Ready:     true,
				})
			}
			for _, a := range sub.NotReadyAddresses {
				targetRef := ""
				podName := ""
				if a.TargetRef != nil {
					targetRef = fmt.Sprintf("%s/%s", a.TargetRef.Kind, a.TargetRef.Name)
					if a.TargetRef.Kind == "Pod" {
						podName = a.TargetRef.Name
					}
				}
				addrs = append(addrs, EndpointAddressInfo{
					IP:        a.IP,
					NodeName:  stringValue(a.NodeName),
					PodName:   podName,
					TargetRef: targetRef,
					Ready:     false,
				})
			}

			epPorts := make([]EndpointPortInfo, 0, len(sub.Ports))
			for _, p := range sub.Ports {
				epPorts = append(epPorts, EndpointPortInfo{
					Name:     p.Name,
					Port:     p.Port,
					Protocol: string(p.Protocol),
				})
			}

			subsets = append(subsets, EndpointSubsetInfo{
				Addresses:         addrs,
				Ports:             epPorts,
				ReadyAddressCount: len(sub.Addresses),
				NotReadyAddrCount: len(sub.NotReadyAddresses),
			})
		}
	}

	var createdAt string
	if !service.CreationTimestamp.IsZero() {
		createdAt = service.CreationTimestamp.Format(time.RFC3339)
	}

	return ServiceInfo{
		Name:        service.Name,
		Namespace:   service.Namespace,
		Type:        string(service.Spec.Type),
		ClusterIP:   service.Spec.ClusterIP,
		ExternalIPs: service.Spec.ExternalIPs,
		Selector:    service.Spec.Selector,
		CreatedAt:   createdAt,
		Ports:       ports,
		Subsets:     subsets,
	}
}

// GetServices lists all services in the namespace and maps them to domain ServiceInfo.
func (p *LocalKubernetesProvider) GetServices(ctx context.Context, namespace string) ([]ServiceInfo, error) {
	services, err := p.clientset.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, &ProviderError{
			Code:    ErrCodeCollectionFailed,
			Message: fmt.Sprintf("failed to list services in namespace %q", namespace),
			Err:     err,
		}
	}

	endpointsList, _ := p.clientset.CoreV1().Endpoints(namespace).List(ctx, metav1.ListOptions{})
	epMap := make(map[string]*corev1.Endpoints)
	if endpointsList != nil {
		for i := range endpointsList.Items {
			epMap[endpointsList.Items[i].Name] = &endpointsList.Items[i]
		}
	}

	result := make([]ServiceInfo, 0, len(services.Items))
	for _, svc := range services.Items {
		ep := epMap[svc.Name]
		result = append(result, mapServiceInfo(svc, ep))
	}
	return result, nil
}

// GetService returns detailed information for a specific service by name.
func (p *LocalKubernetesProvider) GetService(ctx context.Context, namespace, name string) (ServiceInfo, error) {
	svc, err := p.clientset.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return ServiceInfo{}, &ProviderError{
			Code:    ErrCodeResourceNotFound,
			Message: fmt.Sprintf("service %q not found in namespace %q", name, namespace),
			Err:     err,
		}
	}
	ep, _ := p.clientset.CoreV1().Endpoints(namespace).Get(ctx, name, metav1.GetOptions{})
	return mapServiceInfo(*svc, ep), nil
}

// eventTimestamp returns best available event timestamp.
func eventTimestamp(e corev1.Event) time.Time {
	if !e.LastTimestamp.IsZero() {
		return e.LastTimestamp.Time
	}
	if !e.FirstTimestamp.IsZero() {
		return e.FirstTimestamp.Time
	}
	return time.Time{}
}

// GetPodEvents retrieves Kubernetes Events filtered by involvedObject.name=<podName>.
func (p *LocalKubernetesProvider) GetPodEvents(ctx context.Context, namespace, podName string) ([]EventInfo, error) {
	fieldSelector := fmt.Sprintf(
		"involvedObject.name=%s,involvedObject.namespace=%s,involvedObject.kind=Pod",
		podName, namespace,
	)

	events, err := p.clientset.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
		FieldSelector: fieldSelector,
	})
	if err != nil {
		return nil, &ProviderError{
			Code:    ErrCodeCollectionFailed,
			Message: fmt.Sprintf("failed to retrieve events for pod %q", podName),
			Err:     err,
		}
	}

	sorted := make([]corev1.Event, len(events.Items))
	copy(sorted, events.Items)
	sort.Slice(sorted, func(i, j int) bool {
		return eventTimestamp(sorted[i]).Before(eventTimestamp(sorted[j]))
	})

	result := make([]EventInfo, 0, len(sorted))
	for _, ev := range sorted {
		ei := EventInfo{
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
			ei.FirstSeen = ev.FirstTimestamp.Format(time.RFC3339)
		}
		if !ev.LastTimestamp.IsZero() {
			ei.LastSeen = ev.LastTimestamp.Format(time.RFC3339)
		}
		result = append(result, ei)
	}

	return result, nil
}

// GetContainerLogs retrieves bounded container logs.
func (p *LocalKubernetesProvider) GetContainerLogs(ctx context.Context, namespace, podName, containerName string, tailLines int64) (ContainerLogInfo, error) {
	result := ContainerLogInfo{
		PodName:       podName,
		Namespace:     namespace,
		ContainerName: containerName,
		LineLimit:     tailLines,
	}

	logOpts := &corev1.PodLogOptions{
		Container: containerName,
		TailLines: &tailLines,
	}

	req := p.clientset.CoreV1().Pods(namespace).GetLogs(podName, logOpts)
	stream, err := req.Stream(ctx)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "waiting to start") || strings.Contains(errMsg, "ContainerCreating") {
			result.Unavailable = true
			result.UnavailableReason = "container not yet started"
		} else {
			result.APIError = true
			result.APIErrorMsg = errMsg
		}
		return result, nil
	}
	defer stream.Close()

	buf, err := io.ReadAll(stream)
	if err != nil {
		result.APIError = true
		result.APIErrorMsg = fmt.Sprintf("error reading log stream: %v", err)
		return result, nil
	}

	logContent := strings.TrimRight(string(buf), "\n")
	if logContent == "" {
		result.Unavailable = true
		result.UnavailableReason = "no log output available"
		return result, nil
	}

	lines := strings.Split(logContent, "\n")
	result.Lines = lines
	result.LinesRetrieved = len(lines)
	return result, nil
}

// ── Workload & Controller Provider Methods (Feature 14) ──────────────────────

func mapDeploymentInfo(d appsv1.Deployment) DeploymentInfo {
	strategy := string(d.Spec.Strategy.Type)
	var conds []WorkloadCondition
	for _, c := range d.Status.Conditions {
		conds = append(conds, WorkloadCondition{
			Type:               string(c.Type),
			Status:             string(c.Status),
			Reason:             c.Reason,
			Message:            c.Message,
			LastTransitionTime: c.LastTransitionTime.Format(time.RFC3339),
		})
	}

	desired := int32(1)
	if d.Spec.Replicas != nil {
		desired = *d.Spec.Replicas
	}

	return DeploymentInfo{
		Name:                d.Name,
		Namespace:           d.Namespace,
		DesiredReplicas:     desired,
		CurrentReplicas:     d.Status.Replicas,
		ReadyReplicas:       d.Status.ReadyReplicas,
		AvailableReplicas:   d.Status.AvailableReplicas,
		UpdatedReplicas:     d.Status.UpdatedReplicas,
		UnavailableReplicas: d.Status.UnavailableReplicas,
		ObservedGeneration:  d.Status.ObservedGeneration,
		Strategy:            strategy,
		CreatedAt:           d.CreationTimestamp.Format(time.RFC3339),
		Conditions:          conds,
	}
}

func (p *LocalKubernetesProvider) GetDeployment(ctx context.Context, namespace, name string) (DeploymentInfo, error) {
	d, err := p.clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return DeploymentInfo{Name: name, Namespace: namespace, Unavailable: true, Reason: err.Error()}, &ProviderError{
			Code:    ErrCodeResourceNotFound,
			Message: fmt.Sprintf("deployment %q not found in namespace %q", name, namespace),
			Err:     err,
		}
	}
	return mapDeploymentInfo(*d), nil
}

func mapReplicaSetInfo(rs appsv1.ReplicaSet) ReplicaSetInfo {
	var ownerRefs []OwnerReferenceInfo
	for _, o := range rs.OwnerReferences {
		ctrl := false
		if o.Controller != nil {
			ctrl = *o.Controller
		}
		ownerRefs = append(ownerRefs, OwnerReferenceInfo{
			Kind:       o.Kind,
			Name:       o.Name,
			UID:        string(o.UID),
			Controller: ctrl,
		})
	}

	desired := int32(1)
	if rs.Spec.Replicas != nil {
		desired = *rs.Spec.Replicas
	}

	var selector map[string]string
	if rs.Spec.Selector != nil {
		selector = rs.Spec.Selector.MatchLabels
	}

	return ReplicaSetInfo{
		Name:                 rs.Name,
		Namespace:            rs.Namespace,
		DesiredReplicas:      desired,
		CurrentReplicas:      rs.Status.Replicas,
		ReadyReplicas:        rs.Status.ReadyReplicas,
		AvailableReplicas:    rs.Status.AvailableReplicas,
		FullyLabeledReplicas: rs.Status.FullyLabeledReplicas,
		ObservedGeneration:   rs.Status.ObservedGeneration,
		OwnerReferences:      ownerRefs,
		Selector:             selector,
		CreatedAt:            rs.CreationTimestamp.Format(time.RFC3339),
	}
}

func (p *LocalKubernetesProvider) GetReplicaSet(ctx context.Context, namespace, name string) (ReplicaSetInfo, error) {
	rs, err := p.clientset.AppsV1().ReplicaSets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return ReplicaSetInfo{Name: name, Namespace: namespace, Unavailable: true, Reason: err.Error()}, &ProviderError{
			Code:    ErrCodeResourceNotFound,
			Message: fmt.Sprintf("replicaset %q not found in namespace %q", name, namespace),
			Err:     err,
		}
	}
	return mapReplicaSetInfo(*rs), nil
}

func mapStatefulSetInfo(sts appsv1.StatefulSet) StatefulSetInfo {
	var conds []WorkloadCondition
	for _, c := range sts.Status.Conditions {
		conds = append(conds, WorkloadCondition{
			Type:               string(c.Type),
			Status:             string(c.Status),
			Reason:             c.Reason,
			Message:            c.Message,
			LastTransitionTime: c.LastTransitionTime.Format(time.RFC3339),
		})
	}

	desired := int32(1)
	if sts.Spec.Replicas != nil {
		desired = *sts.Spec.Replicas
	}

	strategy := string(sts.Spec.UpdateStrategy.Type)

	return StatefulSetInfo{
		Name:               sts.Name,
		Namespace:          sts.Namespace,
		DesiredReplicas:    desired,
		CurrentReplicas:    sts.Status.Replicas,
		ReadyReplicas:      sts.Status.ReadyReplicas,
		UpdatedReplicas:    sts.Status.UpdatedReplicas,
		CurrentRevision:    sts.Status.CurrentRevision,
		UpdateRevision:     sts.Status.UpdateRevision,
		ServiceName:        sts.Spec.ServiceName,
		ObservedGeneration: sts.Status.ObservedGeneration,
		UpdateStrategy:     strategy,
		CreatedAt:          sts.CreationTimestamp.Format(time.RFC3339),
		Conditions:         conds,
	}
}

func (p *LocalKubernetesProvider) GetStatefulSet(ctx context.Context, namespace, name string) (StatefulSetInfo, error) {
	sts, err := p.clientset.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return StatefulSetInfo{Name: name, Namespace: namespace, Unavailable: true, Reason: err.Error()}, &ProviderError{
			Code:    ErrCodeResourceNotFound,
			Message: fmt.Sprintf("statefulset %q not found in namespace %q", name, namespace),
			Err:     err,
		}
	}
	return mapStatefulSetInfo(*sts), nil
}

func mapDaemonSetInfo(ds appsv1.DaemonSet) DaemonSetInfo {
	return DaemonSetInfo{
		Name:               ds.Name,
		Namespace:          ds.Namespace,
		DesiredScheduled:   ds.Status.DesiredNumberScheduled,
		CurrentScheduled:   ds.Status.CurrentNumberScheduled,
		NumberReady:        ds.Status.NumberReady,
		NumberAvailable:    ds.Status.NumberAvailable,
		NumberUnavailable:  ds.Status.NumberUnavailable,
		NumberMisscheduled: ds.Status.NumberMisscheduled,
		UpdatedScheduled:   ds.Status.UpdatedNumberScheduled,
		ObservedGeneration: ds.Status.ObservedGeneration,
		CreatedAt:          ds.CreationTimestamp.Format(time.RFC3339),
	}
}

func (p *LocalKubernetesProvider) GetDaemonSet(ctx context.Context, namespace, name string) (DaemonSetInfo, error) {
	ds, err := p.clientset.AppsV1().DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return DaemonSetInfo{Name: name, Namespace: namespace, Unavailable: true, Reason: err.Error()}, &ProviderError{
			Code:    ErrCodeResourceNotFound,
			Message: fmt.Sprintf("daemonset %q not found in namespace %q", name, namespace),
			Err:     err,
		}
	}
	return mapDaemonSetInfo(*ds), nil
}

func mapJobInfo(job batchv1.Job) JobInfo {
	var ownerRefs []OwnerReferenceInfo
	for _, o := range job.OwnerReferences {
		ctrl := false
		if o.Controller != nil {
			ctrl = *o.Controller
		}
		ownerRefs = append(ownerRefs, OwnerReferenceInfo{
			Kind:       o.Kind,
			Name:       o.Name,
			UID:        string(o.UID),
			Controller: ctrl,
		})
	}

	var conds []WorkloadCondition
	for _, c := range job.Status.Conditions {
		conds = append(conds, WorkloadCondition{
			Type:               string(c.Type),
			Status:             string(c.Status),
			Reason:             c.Reason,
			Message:            c.Message,
			LastTransitionTime: c.LastTransitionTime.Format(time.RFC3339),
		})
	}

	completions := int32(1)
	if job.Spec.Completions != nil {
		completions = *job.Spec.Completions
	}

	parallelism := int32(1)
	if job.Spec.Parallelism != nil {
		parallelism = *job.Spec.Parallelism
	}

	backoffLimit := int32(6)
	if job.Spec.BackoffLimit != nil {
		backoffLimit = *job.Spec.BackoffLimit
	}

	startTime := ""
	if job.Status.StartTime != nil {
		startTime = job.Status.StartTime.Format(time.RFC3339)
	}

	completionTime := ""
	if job.Status.CompletionTime != nil {
		completionTime = job.Status.CompletionTime.Format(time.RFC3339)
	}

	return JobInfo{
		Name:            job.Name,
		Namespace:       job.Namespace,
		Completions:     completions,
		Parallelism:     parallelism,
		Active:          job.Status.Active,
		Succeeded:       job.Status.Succeeded,
		Failed:          job.Status.Failed,
		StartTime:       startTime,
		CompletionTime:  completionTime,
		BackoffLimit:    backoffLimit,
		OwnerReferences: ownerRefs,
		CreatedAt:       job.CreationTimestamp.Format(time.RFC3339),
		Conditions:      conds,
	}
}

func (p *LocalKubernetesProvider) GetJob(ctx context.Context, namespace, name string) (JobInfo, error) {
	job, err := p.clientset.BatchV1().Jobs(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return JobInfo{Name: name, Namespace: namespace, Unavailable: true, Reason: err.Error()}, &ProviderError{
			Code:    ErrCodeResourceNotFound,
			Message: fmt.Sprintf("job %q not found in namespace %q", name, namespace),
			Err:     err,
		}
	}
	return mapJobInfo(*job), nil
}

func mapCronJobInfo(cj batchv1.CronJob) CronJobInfo {
	suspend := false
	if cj.Spec.Suspend != nil {
		suspend = *cj.Spec.Suspend
	}

	lastSched := ""
	if cj.Status.LastScheduleTime != nil {
		lastSched = cj.Status.LastScheduleTime.Format(time.RFC3339)
	}

	lastSucc := ""
	if cj.Status.LastSuccessfulTime != nil {
		lastSucc = cj.Status.LastSuccessfulTime.Format(time.RFC3339)
	}

	return CronJobInfo{
		Name:               cj.Name,
		Namespace:          cj.Namespace,
		Schedule:           cj.Spec.Schedule,
		Suspend:            suspend,
		ActiveJobsCount:    len(cj.Status.Active),
		LastScheduleTime:   lastSched,
		LastSuccessfulTime: lastSucc,
		ConcurrencyPolicy:  string(cj.Spec.ConcurrencyPolicy),
		CreatedAt:          cj.CreationTimestamp.Format(time.RFC3339),
	}
}

func (p *LocalKubernetesProvider) GetCronJob(ctx context.Context, namespace, name string) (CronJobInfo, error) {
	cj, err := p.clientset.BatchV1().CronJobs(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return CronJobInfo{Name: name, Namespace: namespace, Unavailable: true, Reason: err.Error()}, &ProviderError{
			Code:    ErrCodeResourceNotFound,
			Message: fmt.Sprintf("cronjob %q not found in namespace %q", name, namespace),
			Err:     err,
		}
	}
	return mapCronJobInfo(*cj), nil
}

// ── Configuration & Identity Provider Methods (Feature 15) ───────────────────

// GetConfigMap returns ConfigMap metadata and key names. Values are NOT collected.
func (p *LocalKubernetesProvider) GetConfigMap(ctx context.Context, namespace, name string) (ConfigMapInfo, error) {
	cm, err := p.clientset.CoreV1().ConfigMaps(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return ConfigMapInfo{
			Name:        name,
			Namespace:   namespace,
			Unavailable: true,
			Reason:      err.Error(),
		}, &ProviderError{
			Code:    ErrCodeResourceNotFound,
			Message: fmt.Sprintf("configmap %q not found in namespace %q", name, namespace),
			Err:     err,
		}
	}

	// Collect key names only — never values.
	keys := make([]string, 0, len(cm.Data)+len(cm.BinaryData))
	for k := range cm.Data {
		keys = append(keys, k)
	}
	for k := range cm.BinaryData {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	immutable := false
	if cm.Immutable != nil {
		immutable = *cm.Immutable
	}

	var createdAt string
	if !cm.CreationTimestamp.IsZero() {
		createdAt = cm.CreationTimestamp.Format(time.RFC3339)
	}

	return ConfigMapInfo{
		Name:      cm.Name,
		Namespace: cm.Namespace,
		Keys:      keys,
		Immutable: immutable,
		CreatedAt: createdAt,
	}, nil
}

// GetSecretMetadata returns Secret metadata and key names ONLY.
// SECRET VALUES ARE NEVER RETURNED. The .Data map values are discarded immediately;
// only the key names (map keys) are extracted.
func (p *LocalKubernetesProvider) GetSecretMetadata(ctx context.Context, namespace, name string) (SecretInfo, error) {
	secret, err := p.clientset.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return SecretInfo{
			Name:            name,
			Namespace:       namespace,
			Unavailable:     true,
			Reason:          err.Error(),
			ValuesCollected: false,
		}, &ProviderError{
			Code:    ErrCodeResourceNotFound,
			Message: fmt.Sprintf("secret %q not found in namespace %q", name, namespace),
			Err:     err,
		}
	}

	// Extract key NAMES only — discard all byte-slice values immediately.
	keys := make([]string, 0, len(secret.Data))
	for k := range secret.Data {
		keys = append(keys, k) // only the key name string is kept
	}
	sort.Strings(keys)
	// Explicitly nil out the data fields to ensure values cannot leak.
	secret.Data = nil
	secret.StringData = nil

	immutable := false
	if secret.Immutable != nil {
		immutable = *secret.Immutable
	}

	var createdAt string
	if !secret.CreationTimestamp.IsZero() {
		createdAt = secret.CreationTimestamp.Format(time.RFC3339)
	}

	return SecretInfo{
		Name:            secret.Name,
		Namespace:       secret.Namespace,
		Type:            string(secret.Type),
		Keys:            keys,
		Immutable:       immutable,
		CreatedAt:       createdAt,
		ValuesCollected: false, // always false — security boundary
	}, nil
}

// GetServiceAccount returns ServiceAccount metadata for a given namespace and name.
func (p *LocalKubernetesProvider) GetServiceAccount(ctx context.Context, namespace, name string) (ServiceAccountInfo, error) {
	sa, err := p.clientset.CoreV1().ServiceAccounts(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return ServiceAccountInfo{
			Name:        name,
			Namespace:   namespace,
			Unavailable: true,
			Reason:      err.Error(),
		}, &ProviderError{
			Code:    ErrCodeResourceNotFound,
			Message: fmt.Sprintf("serviceaccount %q not found in namespace %q", name, namespace),
			Err:     err,
		}
	}

	ipsNames := make([]string, 0, len(sa.ImagePullSecrets))
	for _, ips := range sa.ImagePullSecrets {
		ipsNames = append(ipsNames, ips.Name)
	}

	var createdAt string
	if !sa.CreationTimestamp.IsZero() {
		createdAt = sa.CreationTimestamp.Format(time.RFC3339)
	}

	return ServiceAccountInfo{
		Name:                      sa.Name,
		Namespace:                 sa.Namespace,
		ImagePullSecrets:          ipsNames,
		AutomountServiceAcctToken: sa.AutomountServiceAccountToken,
		CreatedAt:                 createdAt,
	}, nil
}
