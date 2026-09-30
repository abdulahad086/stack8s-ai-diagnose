package provider

import (
	"context"
	"fmt"
)

// FakeProvider is an in-memory InfrastructureProvider used for unit testing
// evidence collection, data mapping, and diagnosis boundaries without a live cluster.
type FakeProvider struct {
	ClusterName        string
	NodesData          []NodeInfo
	GPUNodesData       map[string]NodeGPUResources                      // nodeName -> NodeGPUResources
	PodsData           map[string][]PodInfo                             // namespace -> []PodInfo
	ServicesData       map[string][]ServiceInfo                         // namespace -> []ServiceInfo
	PVCsData           map[string]map[string]PersistentVolumeClaimInfo // namespace -> name -> PVC
	PVsData            map[string]PersistentVolumeInfo                  // name -> PV
	StorageClassesData map[string]StorageClassInfo                      // name -> StorageClass
	DeploymentsData    map[string]map[string]DeploymentInfo             // namespace -> name -> Deployment
	ReplicaSetsData    map[string]map[string]ReplicaSetInfo             // namespace -> name -> ReplicaSet
	StatefulSetsData   map[string]map[string]StatefulSetInfo            // namespace -> name -> StatefulSet
	DaemonSetsData     map[string]map[string]DaemonSetInfo              // namespace -> name -> DaemonSet
	JobsData           map[string]map[string]JobInfo                    // namespace -> name -> Job
	CronJobsData       map[string]map[string]CronJobInfo                // namespace -> name -> CronJob
	EventsData         map[string][]EventInfo                           // podName -> []EventInfo
	LogsData           map[string]ContainerLogInfo                      // podName/container -> ContainerLogInfo
}

func NewFakeProvider() *FakeProvider {
	return &FakeProvider{
		ClusterName:        "https://fake.cluster.local:6443",
		GPUNodesData:       make(map[string]NodeGPUResources),
		PodsData:           make(map[string][]PodInfo),
		ServicesData:       make(map[string][]ServiceInfo),
		PVCsData:           make(map[string]map[string]PersistentVolumeClaimInfo),
		PVsData:            make(map[string]PersistentVolumeInfo),
		StorageClassesData: make(map[string]StorageClassInfo),
		DeploymentsData:    make(map[string]map[string]DeploymentInfo),
		ReplicaSetsData:    make(map[string]map[string]ReplicaSetInfo),
		StatefulSetsData:   make(map[string]map[string]StatefulSetInfo),
		DaemonSetsData:     make(map[string]map[string]DaemonSetInfo),
		JobsData:           make(map[string]map[string]JobInfo),
		CronJobsData:       make(map[string]map[string]CronJobInfo),
		EventsData:         make(map[string][]EventInfo),
		LogsData:           make(map[string]ContainerLogInfo),
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

func (f *FakeProvider) GetPersistentVolumeClaim(ctx context.Context, namespace, name string) (PersistentVolumeClaimInfo, error) {
	if nsMap, ok := f.PVCsData[namespace]; ok {
		if pvc, ok := nsMap[name]; ok {
			return pvc, nil
		}
	}
	return PersistentVolumeClaimInfo{
		Name:        name,
		Namespace:   namespace,
		Phase:       "NotFound",
		Unavailable: true,
		Reason:      fmt.Sprintf("pvc %q not found in namespace %q", name, namespace),
	}, &ProviderError{
		Code:    ErrCodeResourceNotFound,
		Message: fmt.Sprintf("pvc %q not found in namespace %q", name, namespace),
	}
}

func (f *FakeProvider) GetPersistentVolume(ctx context.Context, name string) (PersistentVolumeInfo, error) {
	if pv, ok := f.PVsData[name]; ok {
		return pv, nil
	}
	return PersistentVolumeInfo{
		Name:  name,
		Phase: "NotFound",
	}, &ProviderError{
		Code:    ErrCodeResourceNotFound,
		Message: fmt.Sprintf("pv %q not found", name),
	}
}

func (f *FakeProvider) GetStorageClass(ctx context.Context, name string) (StorageClassInfo, error) {
	if sc, ok := f.StorageClassesData[name]; ok {
		return sc, nil
	}
	return StorageClassInfo{
		Name: name,
	}, &ProviderError{
		Code:    ErrCodeResourceNotFound,
		Message: fmt.Sprintf("storage class %q not found", name),
	}
}

func (f *FakeProvider) GetStorageClasses(ctx context.Context) ([]StorageClassInfo, error) {
	result := make([]StorageClassInfo, 0, len(f.StorageClassesData))
	for _, sc := range f.StorageClassesData {
		result = append(result, sc)
	}
	return result, nil
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

func (f *FakeProvider) GetDeployment(ctx context.Context, namespace, name string) (DeploymentInfo, error) {
	if nsMap, ok := f.DeploymentsData[namespace]; ok {
		if dep, ok := nsMap[name]; ok {
			return dep, nil
		}
	}
	return DeploymentInfo{Name: name, Namespace: namespace, Unavailable: true}, &ProviderError{
		Code:    ErrCodeResourceNotFound,
		Message: fmt.Sprintf("deployment %q not found in namespace %q", name, namespace),
	}
}

func (f *FakeProvider) GetReplicaSet(ctx context.Context, namespace, name string) (ReplicaSetInfo, error) {
	if nsMap, ok := f.ReplicaSetsData[namespace]; ok {
		if rs, ok := nsMap[name]; ok {
			return rs, nil
		}
	}
	return ReplicaSetInfo{Name: name, Namespace: namespace, Unavailable: true}, &ProviderError{
		Code:    ErrCodeResourceNotFound,
		Message: fmt.Sprintf("replicaset %q not found in namespace %q", name, namespace),
	}
}

func (f *FakeProvider) GetStatefulSet(ctx context.Context, namespace, name string) (StatefulSetInfo, error) {
	if nsMap, ok := f.StatefulSetsData[namespace]; ok {
		if sts, ok := nsMap[name]; ok {
			return sts, nil
		}
	}
	return StatefulSetInfo{Name: name, Namespace: namespace, Unavailable: true}, &ProviderError{
		Code:    ErrCodeResourceNotFound,
		Message: fmt.Sprintf("statefulset %q not found in namespace %q", name, namespace),
	}
}

func (f *FakeProvider) GetDaemonSet(ctx context.Context, namespace, name string) (DaemonSetInfo, error) {
	if nsMap, ok := f.DaemonSetsData[namespace]; ok {
		if ds, ok := nsMap[name]; ok {
			return ds, nil
		}
	}
	return DaemonSetInfo{Name: name, Namespace: namespace, Unavailable: true}, &ProviderError{
		Code:    ErrCodeResourceNotFound,
		Message: fmt.Sprintf("daemonset %q not found in namespace %q", name, namespace),
	}
}

func (f *FakeProvider) GetJob(ctx context.Context, namespace, name string) (JobInfo, error) {
	if nsMap, ok := f.JobsData[namespace]; ok {
		if job, ok := nsMap[name]; ok {
			return job, nil
		}
	}
	return JobInfo{Name: name, Namespace: namespace, Unavailable: true}, &ProviderError{
		Code:    ErrCodeResourceNotFound,
		Message: fmt.Sprintf("job %q not found in namespace %q", name, namespace),
	}
}

func (f *FakeProvider) GetCronJob(ctx context.Context, namespace, name string) (CronJobInfo, error) {
	if nsMap, ok := f.CronJobsData[namespace]; ok {
		if cj, ok := nsMap[name]; ok {
			return cj, nil
		}
	}
	return CronJobInfo{Name: name, Namespace: namespace, Unavailable: true}, &ProviderError{
		Code:    ErrCodeResourceNotFound,
		Message: fmt.Sprintf("cronjob %q not found in namespace %q", name, namespace),
	}
}
