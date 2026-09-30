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

func (s *Stack8sProvider) GetPersistentVolumeClaim(ctx context.Context, namespace, name string) (PersistentVolumeClaimInfo, error) {
	return PersistentVolumeClaimInfo{}, &ProviderError{Code: ErrCodeUnavailable, Message: "Stack8sProvider API pending implementation"}
}

func (s *Stack8sProvider) GetPersistentVolume(ctx context.Context, name string) (PersistentVolumeInfo, error) {
	return PersistentVolumeInfo{}, &ProviderError{Code: ErrCodeUnavailable, Message: "Stack8sProvider API pending implementation"}
}

func (s *Stack8sProvider) GetStorageClass(ctx context.Context, name string) (StorageClassInfo, error) {
	return StorageClassInfo{}, &ProviderError{Code: ErrCodeUnavailable, Message: "Stack8sProvider API pending implementation"}
}

func (s *Stack8sProvider) GetStorageClasses(ctx context.Context) ([]StorageClassInfo, error) {
	return nil, &ProviderError{Code: ErrCodeUnavailable, Message: "Stack8sProvider API pending implementation"}
}

func (s *Stack8sProvider) GetDeployment(ctx context.Context, namespace, name string) (DeploymentInfo, error) {
	return DeploymentInfo{}, &ProviderError{Code: ErrCodeUnavailable, Message: "Stack8sProvider API pending implementation"}
}

func (s *Stack8sProvider) GetReplicaSet(ctx context.Context, namespace, name string) (ReplicaSetInfo, error) {
	return ReplicaSetInfo{}, &ProviderError{Code: ErrCodeUnavailable, Message: "Stack8sProvider API pending implementation"}
}

func (s *Stack8sProvider) GetStatefulSet(ctx context.Context, namespace, name string) (StatefulSetInfo, error) {
	return StatefulSetInfo{}, &ProviderError{Code: ErrCodeUnavailable, Message: "Stack8sProvider API pending implementation"}
}

func (s *Stack8sProvider) GetDaemonSet(ctx context.Context, namespace, name string) (DaemonSetInfo, error) {
	return DaemonSetInfo{}, &ProviderError{Code: ErrCodeUnavailable, Message: "Stack8sProvider API pending implementation"}
}

func (s *Stack8sProvider) GetJob(ctx context.Context, namespace, name string) (JobInfo, error) {
	return JobInfo{}, &ProviderError{Code: ErrCodeUnavailable, Message: "Stack8sProvider API pending implementation"}
}

func (s *Stack8sProvider) GetCronJob(ctx context.Context, namespace, name string) (CronJobInfo, error) {
	return CronJobInfo{}, &ProviderError{Code: ErrCodeUnavailable, Message: "Stack8sProvider API pending implementation"}
}

func (s *Stack8sProvider) GetPodEvents(ctx context.Context, namespace, podName string) ([]EventInfo, error) {
	return nil, &ProviderError{Code: ErrCodeUnavailable, Message: "Stack8sProvider API pending implementation"}
}

func (s *Stack8sProvider) GetContainerLogs(ctx context.Context, namespace, podName, containerName string, tailLines int64) (ContainerLogInfo, error) {
	return ContainerLogInfo{}, &ProviderError{Code: ErrCodeUnavailable, Message: "Stack8sProvider API pending implementation"}
}
