package provider

import (
	"context"
	"fmt"
)

// FakeProvider is an in-memory InfrastructureProvider used for unit testing
// evidence collection, data mapping, and diagnosis boundaries without a live cluster.
type FakeProvider struct {
	ClusterName  string
	NodesData    []NodeInfo
	GPUNodesData map[string]NodeGPUResources // nodeName -> NodeGPUResources
	PodsData     map[string][]PodInfo        // namespace -> []PodInfo
	ServicesData map[string][]ServiceInfo    // namespace -> []ServiceInfo
	EventsData   map[string][]EventInfo      // podName -> []EventInfo
	LogsData     map[string]ContainerLogInfo // podName/container -> ContainerLogInfo
}

func NewFakeProvider() *FakeProvider {
	return &FakeProvider{
		ClusterName:  "https://fake.cluster.local:6443",
		GPUNodesData: make(map[string]NodeGPUResources),
		PodsData:     make(map[string][]PodInfo),
		ServicesData: make(map[string][]ServiceInfo),
		EventsData:   make(map[string][]EventInfo),
		LogsData:     make(map[string]ContainerLogInfo),
	}
}

func (f *FakeProvider) Name() string {
	return "fake-provider"
}

func (f *FakeProvider) ClusterHost() string {
	return f.ClusterName
}

func (f *FakeProvider) GetNodes(ctx context.Context) ([]NodeInfo, error) {
	return f.NodesData, nil
}

func (f *FakeProvider) GetNode(ctx context.Context, name string) (NodeInfo, error) {
	if name == "" || name == "<not yet scheduled>" || name == "<none>" {
		return NodeInfo{}, &ProviderError{
			Code:    ErrCodeResourceNotFound,
			Message: fmt.Sprintf("invalid node name %q", name),
		}
	}
	for _, n := range f.NodesData {
		if n.Name == name {
			return n, nil
		}
	}
	return NodeInfo{}, &ProviderError{
		Code:    ErrCodeResourceNotFound,
		Message: fmt.Sprintf("node %q not found in fake provider", name),
	}
}

func (f *FakeProvider) GetNodeGPUResources(ctx context.Context, name string) (NodeGPUResources, error) {
	if name == "" || name == "<not yet scheduled>" || name == "<none>" {
		return NodeGPUResources{
			NodeName:           name,
			GPUAvailable:       false,
			GPUResourceExposed: false,
		}, nil
	}
	if gpuRes, ok := f.GPUNodesData[name]; ok {
		return gpuRes, nil
	}
	// Default: if node exists in NodesData but not in GPUNodesData, return non-exposed GPU
	for _, n := range f.NodesData {
		if n.Name == name {
			return NodeGPUResources{
				NodeName:           name,
				GPUAvailable:       false,
				GPUResourceExposed: false,
			}, nil
		}
	}
	return NodeGPUResources{
		NodeName:           name,
		GPUAvailable:       false,
		GPUResourceExposed: false,
	}, &ProviderError{
		Code:    ErrCodeResourceNotFound,
		Message: fmt.Sprintf("node %q not found in fake provider for GPU check", name),
	}
}

func (f *FakeProvider) GetPods(ctx context.Context, namespace string) ([]PodInfo, error) {
	return f.PodsData[namespace], nil
}

func (f *FakeProvider) GetServices(ctx context.Context, namespace string) ([]ServiceInfo, error) {
	return f.ServicesData[namespace], nil
}

func (f *FakeProvider) GetService(ctx context.Context, namespace, name string) (ServiceInfo, error) {
	services := f.ServicesData[namespace]
	for _, s := range services {
		if s.Name == name {
			return s, nil
		}
	}
	return ServiceInfo{}, &ProviderError{
		Code:    ErrCodeResourceNotFound,
		Message: fmt.Sprintf("service %q not found in namespace %q", name, namespace),
	}
}

func (f *FakeProvider) GetPodEvents(ctx context.Context, namespace, podName string) ([]EventInfo, error) {
	return f.EventsData[podName], nil
}

func (f *FakeProvider) GetContainerLogs(ctx context.Context, namespace, podName, containerName string, tailLines int64) (ContainerLogInfo, error) {
	key := podName + "/" + containerName
	if logInfo, ok := f.LogsData[key]; ok {
		return logInfo, nil
	}
	return ContainerLogInfo{
		PodName:           podName,
		Namespace:         namespace,
		ContainerName:     containerName,
		LineLimit:         tailLines,
		Unavailable:       true,
		UnavailableReason: "no fake logs available",
	}, nil
}
