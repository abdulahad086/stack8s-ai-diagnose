package provider

import (
	"context"
	"testing"
)

func TestFakeProviderInterfaceCompliance(t *testing.T) {
	fake := NewFakeProvider()
	fake.NodesData = []NodeInfo{
		{
			Name:      "fake-node-1",
			Status:    "Ready",
			CreatedAt: "2026-09-30T10:00:00Z",
			Resources: NodeResources{
				CPUCapacity:       "4",
				CPUAllocatable:    "4",
				MemoryCapacity:    "8Gi",
				MemoryAllocatable: "7Gi",
			},
			SystemInfo: NodeSystemInfo{
				KubeletVersion:  "v1.30.2",
				OperatingSystem: "linux",
				Architecture:    "amd64",
			},
		},
	}
	fake.PodsData["default"] = []PodInfo{
		{
			PodName:   "fake-pod-1",
			Namespace: "default",
			Phase:     "Pending",
			NodeName:  "fake-node-1",
			CreatedAt: "2026-09-30T10:00:00Z",
			Containers: []ContainerInfo{
				{
					Name:          "app",
					Image:         "invalid/image:v1",
					State:         "Waiting (ImagePullBackOff)",
					CPURequest:    "100m",
					CPULimit:      "200m",
					MemoryRequest: "64Mi",
					MemoryLimit:   "128Mi",
				},
			},
		},
	}
	fake.EventsData["fake-pod-1"] = []EventInfo{
		{
			PodName:      "fake-pod-1",
			Namespace:    "default",
			Type:         "Warning",
			Reason:       "ImagePullBackOff",
			Message:      "Back-off pulling image invalid/image:v1",
			InvolvedKind: "Pod",
			InvolvedName: "fake-pod-1",
		},
	}
	fake.LogsData["fake-pod-1/app"] = ContainerLogInfo{
		PodName:           "fake-pod-1",
		Namespace:         "default",
		ContainerName:     "app",
		Unavailable:       true,
		UnavailableReason: "container not yet started",
	}

	ctx := context.Background()

	// 1. GetNodes
	nodes, err := fake.GetNodes(ctx)
	if err != nil || len(nodes) != 1 || nodes[0].Name != "fake-node-1" {
		t.Fatalf("unexpected nodes output: %v, err: %v", nodes, err)
	}

	// 2. GetNode
	node, err := fake.GetNode(ctx, "fake-node-1")
	if err != nil || node.Name != "fake-node-1" || node.Status != "Ready" {
		t.Fatalf("unexpected node output: %v, err: %v", node, err)
	}
	if node.Resources.CPUCapacity != "4" || node.Resources.MemoryAllocatable != "7Gi" {
		t.Fatalf("unexpected node resources: %v", node.Resources)
	}

	// 3. GetPods
	pods, err := fake.GetPods(ctx, "default")
	if err != nil || len(pods) != 1 || pods[0].PodName != "fake-pod-1" {
		t.Fatalf("unexpected pods output: %v, err: %v", pods, err)
	}
	c := pods[0].Containers[0]
	if c.CPURequest != "100m" || c.CPULimit != "200m" || c.MemoryRequest != "64Mi" || c.MemoryLimit != "128Mi" {
		t.Fatalf("unexpected container resource limits: %v", c)
	}

	// 4. GetPodEvents
	events, err := fake.GetPodEvents(ctx, "default", "fake-pod-1")
	if err != nil || len(events) != 1 || events[0].Reason != "ImagePullBackOff" {
		t.Fatalf("unexpected events output: %v, err: %v", events, err)
	}

	// 5. GetContainerLogs
	logs, err := fake.GetContainerLogs(ctx, "default", "fake-pod-1", "app", 100)
	if err != nil || !logs.Unavailable {
		t.Fatalf("unexpected logs output: %v, err: %v", logs, err)
	}
}

func TestFakeProviderPendingUnscheduledPod(t *testing.T) {
	fake := NewFakeProvider()
	ctx := context.Background()

	_, err := fake.GetNode(ctx, "<not yet scheduled>")
	if err == nil {
		t.Fatalf("expected error querying node for unscheduled pod")
	}

	_, err = fake.GetNode(ctx, "<none>")
	if err == nil {
		t.Fatalf("expected error querying empty node name")
	}
}

func TestStack8sProviderContractPlaceholder(t *testing.T) {
	stack8sProv, err := NewStack8sProvider()
	if err == nil {
		t.Fatalf("expected NewStack8sProvider to return error since Stack8s API is not yet integrated")
	}
	if stack8sProv != nil {
		t.Fatalf("expected nil provider on un-implemented Stack8s provider")
	}
}

func TestProviderErrorFormatting(t *testing.T) {
	pe := &ProviderError{
		Code:    ErrCodeUnavailable,
		Message: "cluster connection timeout",
	}
	if pe.Error() != "provider error [PROVIDER_UNAVAILABLE]: cluster connection timeout" {
		t.Fatalf("unexpected error format: %s", pe.Error())
	}
}

func TestFakeProviderGPUResources(t *testing.T) {
	fake := NewFakeProvider()
	ctx := context.Background()

	// 1. Node with exposed GPU
	fake.NodesData = []NodeInfo{
		{Name: "gpu-node-1", Status: "Ready"},
		{Name: "cpu-node-1", Status: "Ready"},
	}
	fake.GPUNodesData["gpu-node-1"] = NodeGPUResources{
		NodeName:           "gpu-node-1",
		GPUAvailable:       true,
		GPUResourceExposed: true,
		ResourceName:       "nvidia.com/gpu",
		Model:              "NVIDIA A100-SXM4-80GB",
		Capacity:           "8",
		Allocatable:        "8",
	}

	// Test querying GPU node
	gpuRes, err := fake.GetNodeGPUResources(ctx, "gpu-node-1")
	if err != nil {
		t.Fatalf("unexpected error getting GPU resources: %v", err)
	}
	if !gpuRes.GPUAvailable || !gpuRes.GPUResourceExposed || gpuRes.ResourceName != "nvidia.com/gpu" || gpuRes.Capacity != "8" || gpuRes.Allocatable != "8" || gpuRes.Model != "NVIDIA A100-SXM4-80GB" {
		t.Fatalf("unexpected GPU resources output: %+v", gpuRes)
	}

	// 2. Node with no GPU exposed
	cpuRes, err := fake.GetNodeGPUResources(ctx, "cpu-node-1")
	if err != nil {
		t.Fatalf("unexpected error getting CPU node GPU resources: %v", err)
	}
	if cpuRes.GPUAvailable || cpuRes.GPUResourceExposed {
		t.Fatalf("expected GPUNotExposed for cpu-node-1, got: %+v", cpuRes)
	}

	// 3. Unscheduled/Pending pod node query
	pendingRes, err := fake.GetNodeGPUResources(ctx, "<not yet scheduled>")
	if err != nil {
		t.Fatalf("unexpected error querying unscheduled pod node: %v", err)
	}
	if pendingRes.GPUAvailable || pendingRes.GPUResourceExposed {
		t.Fatalf("expected unscheduled node to return un-exposed GPU, got: %+v", pendingRes)
	}
}

func TestFakeProviderServices(t *testing.T) {
	fake := NewFakeProvider()
	ctx := context.Background()

	fake.ServicesData["default"] = []ServiceInfo{
		{
			Name:      "web-svc",
			Namespace: "default",
			Type:      "ClusterIP",
			ClusterIP: "10.96.0.100",
			Selector:  map[string]string{"app": "web"},
			Ports: []ServicePortInfo{
				{Name: "http", Protocol: "TCP", Port: 80, TargetPort: "8080"},
			},
			Subsets: []EndpointSubsetInfo{
				{
					ReadyAddressCount: 1,
					Addresses: []EndpointAddressInfo{
						{IP: "10.1.0.15", PodName: "web-pod-1", Ready: true},
					},
					Ports: []EndpointPortInfo{
						{Name: "http", Port: 8080, Protocol: "TCP"},
					},
				},
			},
		},
	}

	// 1. GetServices
	svcs, err := fake.GetServices(ctx, "default")
	if err != nil || len(svcs) != 1 || svcs[0].Name != "web-svc" {
		t.Fatalf("unexpected GetServices result: %v, err: %v", svcs, err)
	}
	if svcs[0].ClusterIP != "10.96.0.100" || svcs[0].Ports[0].TargetPort != "8080" {
		t.Fatalf("unexpected ServiceInfo details: %+v", svcs[0])
	}

	// 2. GetService
	svc, err := fake.GetService(ctx, "default", "web-svc")
	if err != nil || svc.Name != "web-svc" {
		t.Fatalf("unexpected GetService result: %+v, err: %v", svc, err)
	}

	// 3. GetService missing
	_, err = fake.GetService(ctx, "default", "non-existent")
	if err == nil {
		t.Fatalf("expected error for non-existent service")
	}
}

func TestFakeProviderStorage(t *testing.T) {
	fake := NewFakeProvider()
	ctx := context.Background()

	fake.PVCsData["default"] = map[string]PersistentVolumeClaimInfo{
		"bound-pvc": {
			Name:             "bound-pvc",
			Namespace:        "default",
			Phase:            "Bound",
			StorageClassName: "hostpath",
			VolumeName:       "pv-123",
			RequestedStorage: "1Gi",
			AccessModes:      []string{"ReadWriteOnce"},
			VolumeMode:       "Filesystem",
		},
		"pending-pvc": {
			Name:             "pending-pvc",
			Namespace:        "default",
			Phase:            "Pending",
			StorageClassName: "non-existent-sc",
			RequestedStorage: "5Gi",
			AccessModes:      []string{"ReadWriteOnce"},
			VolumeMode:       "Filesystem",
		},
	}

	fake.PVsData["pv-123"] = PersistentVolumeInfo{
		Name:             "pv-123",
		Phase:            "Bound",
		Capacity:         "1Gi",
		StorageClassName: "hostpath",
		AccessModes:      []string{"ReadWriteOnce"},
		VolumeMode:       "Filesystem",
		ClaimRef:         "default/bound-pvc",
		ReclaimPolicy:    "Delete",
		PluginSource:     "hostPath",
	}

	fake.StorageClassesData["hostpath"] = StorageClassInfo{
		Name:              "hostpath",
		Provisioner:       "docker.io/hostpath",
		ReclaimPolicy:     "Delete",
		VolumeBindingMode: "Immediate",
		IsDefault:         true,
	}

	// 1. GetPersistentVolumeClaim - Bound
	pvc, err := fake.GetPersistentVolumeClaim(ctx, "default", "bound-pvc")
	if err != nil || pvc.Phase != "Bound" || pvc.VolumeName != "pv-123" || pvc.RequestedStorage != "1Gi" {
		t.Fatalf("unexpected PVC result: %+v, err: %v", pvc, err)
	}

	// 2. GetPersistentVolumeClaim - Pending
	pendingPvc, err := fake.GetPersistentVolumeClaim(ctx, "default", "pending-pvc")
	if err != nil || pendingPvc.Phase != "Pending" || pendingPvc.VolumeName != "" {
		t.Fatalf("unexpected Pending PVC result: %+v, err: %v", pendingPvc, err)
	}

	// 3. GetPersistentVolumeClaim - Missing
	missingPvc, err := fake.GetPersistentVolumeClaim(ctx, "default", "missing-pvc")
	if err == nil || missingPvc.Phase != "NotFound" || !missingPvc.Unavailable {
		t.Fatalf("expected NotFound for missing PVC, got: %+v", missingPvc)
	}

	// 4. GetPersistentVolume
	pv, err := fake.GetPersistentVolume(ctx, "pv-123")
	if err != nil || pv.Phase != "Bound" || pv.Capacity != "1Gi" || pv.PluginSource != "hostPath" {
		t.Fatalf("unexpected PV result: %+v, err: %v", pv, err)
	}

	// 5. GetStorageClass & List
	sc, err := fake.GetStorageClass(ctx, "hostpath")
	if err != nil || sc.Provisioner != "docker.io/hostpath" || !sc.IsDefault {
		t.Fatalf("unexpected SC result: %+v, err: %v", sc, err)
	}

	scs, err := fake.GetStorageClasses(ctx)
	if err != nil || len(scs) != 1 || scs[0].Name != "hostpath" {
		t.Fatalf("unexpected GetStorageClasses result: %v, err: %v", scs, err)
	}
}
