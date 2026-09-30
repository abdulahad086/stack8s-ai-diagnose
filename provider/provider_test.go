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
