package provider

import (
	"context"
	"fmt"
)

// Stack8sProvider (FUTURE)
//
// Stack8sProvider will implement the InfrastructureProvider interface against the
// actual Stack8s infrastructure backend and control plane API.
//
// TODO: Implement Stack8sProvider after access to the actual Stack8s backend/API is available.

type Stack8sProvider struct {
	// Future Stack8s client fields (e.g. Endpoint, AuthToken, Stack8sClient)
}

func NewStack8sProvider() (*Stack8sProvider, error) {
	return nil, &ProviderError{
		Code:    ErrCodeUnavailable,
		Message: "Stack8sProvider is a future integration. Access to Stack8s backend API is not yet available.",
		Err:     fmt.Errorf("stack8s provider not implemented"),
	}
}

func (s *Stack8sProvider) Name() string {
	return "stack8s"
}

func (s *Stack8sProvider) ClusterHost() string {
	return "stack8s.internal.host"
}

func (s *Stack8sProvider) GetNodes(ctx context.Context) ([]NodeInfo, error) {
	return nil, &ProviderError{Code: ErrCodeUnavailable, Message: "Stack8sProvider API pending implementation"}
}

func (s *Stack8sProvider) GetNode(ctx context.Context, name string) (NodeInfo, error) {
	return NodeInfo{}, &ProviderError{Code: ErrCodeUnavailable, Message: "Stack8sProvider API pending implementation"}
}

func (s *Stack8sProvider) GetNodeGPUResources(ctx context.Context, name string) (NodeGPUResources, error) {
	return NodeGPUResources{}, &ProviderError{Code: ErrCodeUnavailable, Message: "Stack8sProvider API pending implementation"}
}

func (s *Stack8sProvider) GetPods(ctx context.Context, namespace string) ([]PodInfo, error) {
	return nil, &ProviderError{Code: ErrCodeUnavailable, Message: "Stack8sProvider API pending implementation"}
}

func (s *Stack8sProvider) GetServices(ctx context.Context, namespace string) ([]ServiceInfo, error) {
	return nil, &ProviderError{Code: ErrCodeUnavailable, Message: "Stack8sProvider API pending implementation"}
}

func (s *Stack8sProvider) GetService(ctx context.Context, namespace, name string) (ServiceInfo, error) {
	return ServiceInfo{}, &ProviderError{Code: ErrCodeUnavailable, Message: "Stack8sProvider API pending implementation"}
}

func (s *Stack8sProvider) GetPodEvents(ctx context.Context, namespace, podName string) ([]EventInfo, error) {
	return nil, &ProviderError{Code: ErrCodeUnavailable, Message: "Stack8sProvider API pending implementation"}
}

func (s *Stack8sProvider) GetContainerLogs(ctx context.Context, namespace, podName, containerName string, tailLines int64) (ContainerLogInfo, error) {
	return ContainerLogInfo{}, &ProviderError{Code: ErrCodeUnavailable, Message: "Stack8sProvider API pending implementation"}
}
