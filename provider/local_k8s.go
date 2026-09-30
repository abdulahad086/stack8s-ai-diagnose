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

	corev1 "k8s.io/api/core/v1"
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

		result = append(result, PodInfo{
			PodName:    pod.Name,
			Namespace:  pod.Namespace,
			Phase:      string(pod.Status.Phase),
			NodeName:   nodeName,
			PodIP:      pod.Status.PodIP,
			Labels:     pod.Labels,
			CreatedAt:  pod.CreationTimestamp.Format(time.RFC3339),
			Containers: containers,
		})
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
