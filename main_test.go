package main

import (
	"testing"

	"github.com/stack8s/stack8s-ai-diagnose/provider"
)

func TestDiagnosePod_ImagePullBackOff(t *testing.T) {
	podPkg := PodEvidencePackage{
		PodName:   "test-image-pull",
		Namespace: "default",
		Evidence: []EvidenceItem{
			{
				ID:     "E1",
				Source: SourcePodStatus,
				Data: PodStatusData{
					PodName:   "test-image-pull",
					Namespace: "default",
					Phase:     "Pending",
					Containers: []ContainerEvidenceData{
						{
							Name:  "app",
							Image: "invalid-repo/invalid-image:latest",
							State: "Waiting (ImagePullBackOff: Back-off pulling image)",
						},
					},
				},
			},
			{
				ID:     "E2",
				Source: SourceKubernetesEvent,
				Data: KubernetesEventData{
					PodName:   "test-image-pull",
					Namespace: "default",
					Reason:    "ImagePullBackOff",
					Message:   "Back-off pulling image invalid-repo/invalid-image:latest",
				},
			},
		},
	}

	diag := diagnosePod(podPkg)

	if diag.Status != DiagnosisConfirmed {
		t.Fatalf("expected status CONFIRMED, got %s", diag.Status)
	}
	if diag.DiagnosisCode != CodeImagePullFailure {
		t.Fatalf("expected code IMAGE_PULL_FAILURE, got %s", diag.DiagnosisCode)
	}
	if len(diag.EvidenceIDs) != 2 || diag.EvidenceIDs[0] != "E1" || diag.EvidenceIDs[1] != "E2" {
		t.Fatalf("expected evidence IDs [E1, E2], got %v", diag.EvidenceIDs)
	}
}

func TestDiagnosePod_ErrImagePull(t *testing.T) {
	podPkg := PodEvidencePackage{
		PodName:   "test-err-image-pull",
		Namespace: "default",
		Evidence: []EvidenceItem{
			{
				ID:     "E1",
				Source: SourcePodStatus,
				Data: PodStatusData{
					PodName:   "test-err-image-pull",
					Namespace: "default",
					Phase:     "Pending",
					Containers: []ContainerEvidenceData{
						{
							Name:  "app",
							Image: "nonexistent:v1",
							State: "Waiting (ErrImagePull: rpc error)",
						},
					},
				},
			},
			{
				ID:     "E2",
				Source: SourceKubernetesEvent,
				Data: KubernetesEventData{
					PodName:   "test-err-image-pull",
					Namespace: "default",
					Reason:    "Failed",
					Message:   "Failed to pull image nonexistent:v1: rpc error: code = NotFound",
				},
			},
		},
	}

	diag := diagnosePod(podPkg)

	if diag.Status != DiagnosisConfirmed {
		t.Fatalf("expected status CONFIRMED, got %s", diag.Status)
	}
	if diag.DiagnosisCode != CodeImagePullFailure {
		t.Fatalf("expected code IMAGE_PULL_FAILURE, got %s", diag.DiagnosisCode)
	}
	if len(diag.EvidenceIDs) != 2 || diag.EvidenceIDs[0] != "E1" || diag.EvidenceIDs[1] != "E2" {
		t.Fatalf("expected evidence IDs [E1, E2], got %v", diag.EvidenceIDs)
	}
}

func TestDiagnosePod_OOMKilled(t *testing.T) {
	podPkg := PodEvidencePackage{
		PodName:   "test-oom-pod",
		Namespace: "default",
		Evidence: []EvidenceItem{
			{
				ID:     "E1",
				Source: SourcePodStatus,
				Data: PodStatusData{
					PodName:   "test-oom-pod",
					Namespace: "default",
					Phase:     "Failed",
					Containers: []ContainerEvidenceData{
						{
							Name:               "memory-hog",
							Image:              "alpine:latest",
							State:              "Terminated (reason: OOMKilled, exit: 137)",
							HasLastTermination: true,
							LastTermReason:     "OOMKilled",
							LastTermExitCode:   137,
						},
					},
				},
			},
			{
				ID:     "E2",
				Source: SourceKubernetesEvent,
				Data: KubernetesEventData{
					PodName:   "test-oom-pod",
					Namespace: "default",
					Reason:    "OOMKilled",
					Message:   "System OOM killer invoked for container memory-hog",
				},
			},
		},
	}

	diag := diagnosePod(podPkg)

	if diag.Status != DiagnosisConfirmed {
		t.Fatalf("expected status CONFIRMED, got %s", diag.Status)
	}
	if diag.DiagnosisCode != CodeOOMKilled {
		t.Fatalf("expected code OOM_KILLED, got %s", diag.DiagnosisCode)
	}
	if len(diag.EvidenceIDs) != 2 || diag.EvidenceIDs[0] != "E1" || diag.EvidenceIDs[1] != "E2" {
		t.Fatalf("expected evidence IDs [E1, E2], got %v", diag.EvidenceIDs)
	}
}

func TestDiagnosePod_CrashLoopBackOff_NoRootCause(t *testing.T) {
	podPkg := PodEvidencePackage{
		PodName:   "test-crashloop",
		Namespace: "default",
		Evidence: []EvidenceItem{
			{
				ID:     "E1",
				Source: SourcePodStatus,
				Data: PodStatusData{
					PodName:   "test-crashloop",
					Namespace: "default",
					Phase:     "Running",
					Containers: []ContainerEvidenceData{
						{
							Name:               "app",
							Image:              "my-app:v1",
							State:              "Waiting (CrashLoopBackOff: back-off 5m0s restarting failed container)",
							HasLastTermination: true,
							LastTermReason:     "Error",
							LastTermExitCode:   1,
						},
					},
				},
			},
		},
	}

	diag := diagnosePod(podPkg)

	if diag.Status != DiagnosisUnknown {
		t.Fatalf("expected status UNKNOWN for CrashLoopBackOff without root cause, got %s", diag.Status)
	}
	if diag.DiagnosisCode != CodeUnknown {
		t.Fatalf("expected code UNKNOWN, got %s", diag.DiagnosisCode)
	}
	if len(diag.EvidenceIDs) != 0 {
		t.Fatalf("expected empty evidence IDs for UNKNOWN, got %v", diag.EvidenceIDs)
	}
}

func TestDiagnosePod_Pending_NoCause(t *testing.T) {
	podPkg := PodEvidencePackage{
		PodName:   "test-pending",
		Namespace: "default",
		Evidence: []EvidenceItem{
			{
				ID:     "E1",
				Source: SourcePodStatus,
				Data: PodStatusData{
					PodName:   "test-pending",
					Namespace: "default",
					Phase:     "Pending",
					Containers: []ContainerEvidenceData{
						{
							Name:  "app",
							Image: "nginx:latest",
							State: "Pending (no status yet)",
						},
					},
				},
			},
		},
	}

	diag := diagnosePod(podPkg)

	if diag.Status != DiagnosisUnknown {
		t.Fatalf("expected status UNKNOWN for pending pod without scheduling event, got %s", diag.Status)
	}
	if diag.DiagnosisCode != CodeUnknown {
		t.Fatalf("expected code UNKNOWN, got %s", diag.DiagnosisCode)
	}
}

func TestDiagnosePod_NormalRunningPod(t *testing.T) {
	podPkg := PodEvidencePackage{
		PodName:   "test-nginx",
		Namespace: "default",
		Evidence: []EvidenceItem{
			{
				ID:     "E1",
				Source: SourcePodStatus,
				Data: PodStatusData{
					PodName:   "test-nginx",
					Namespace: "default",
					Phase:     "Running",
					Containers: []ContainerEvidenceData{
						{
							Name:  "nginx",
							Image: "nginx:alpine",
							Ready: true,
							State: "Running (started: 2026-09-30 08:00:00)",
						},
					},
				},
			},
		},
	}

	diag := diagnosePod(podPkg)

	if diag.Status != DiagnosisUnknown {
		t.Fatalf("expected status UNKNOWN for healthy running pod, got %s", diag.Status)
	}
	if diag.DiagnosisCode != CodeUnknown {
		t.Fatalf("expected code UNKNOWN, got %s", diag.DiagnosisCode)
	}
}

func TestDiagnosePod_Precedence_OOMOverBackoff(t *testing.T) {
	podPkg := PodEvidencePackage{
		PodName:   "test-oom-crashloop",
		Namespace: "default",
		Evidence: []EvidenceItem{
			{
				ID:     "E1",
				Source: SourcePodStatus,
				Data: PodStatusData{
					PodName:   "test-oom-crashloop",
					Namespace: "default",
					Phase:     "Running",
					Containers: []ContainerEvidenceData{
						{
							Name:               "worker",
							Image:              "worker:latest",
							State:              "Waiting (CrashLoopBackOff)",
							HasLastTermination: true,
							LastTermReason:     "OOMKilled",
							LastTermExitCode:   137,
						},
					},
				},
			},
			{
				ID:     "E2",
				Source: SourceKubernetesEvent,
				Data: KubernetesEventData{
					PodName:   "test-oom-crashloop",
					Namespace: "default",
					Reason:    "BackOff",
					Message:   "Back-off restarting failed container",
				},
			},
		},
	}

	diag := diagnosePod(podPkg)

	if diag.Status != DiagnosisConfirmed {
		t.Fatalf("expected status CONFIRMED, got %s", diag.Status)
	}
	if diag.DiagnosisCode != CodeOOMKilled {
		t.Fatalf("expected OOM_KILLED taking precedence over CrashLoopBackOff, got %s", diag.DiagnosisCode)
	}
	if len(diag.EvidenceIDs) != 1 || diag.EvidenceIDs[0] != "E1" {
		t.Fatalf("expected evidence ID [E1], got %v", diag.EvidenceIDs)
	}
}

func TestBuildEvidencePackage_NodeEvidence(t *testing.T) {
	fake := provider.NewFakeProvider()
	fake.NodesData = []provider.NodeInfo{
		{
			Name:   "node-1",
			Status: "Ready",
			Resources: provider.NodeResources{
				CPUCapacity:       "4",
				CPUAllocatable:    "4",
				MemoryCapacity:    "8Gi",
				MemoryAllocatable: "7Gi",
			},
		},
	}
	fake.PodsData["default"] = []provider.PodInfo{
		{
			PodName:   "scheduled-pod",
			Namespace: "default",
			NodeName:  "node-1",
			Containers: []provider.ContainerInfo{
				{
					Name:          "web",
					Image:         "nginx:latest",
					CPURequest:    "100m",
					CPULimit:      "200m",
					MemoryRequest: "64Mi",
					MemoryLimit:   "128Mi",
				},
			},
		},
		{
			PodName:   "unscheduled-pod",
			Namespace: "default",
			NodeName:  "<not yet scheduled>",
		},
	}

	pkg := buildEvidencePackage(fake, "default")
	if len(pkg.Pods) != 2 {
		t.Fatalf("expected 2 pods in package, got %d", len(pkg.Pods))
	}

	// Verify scheduled pod node_status evidence
	var scheduledNodeEv *NodeStatusData
	for _, ev := range pkg.Pods[0].Evidence {
		if ev.Source == SourceNodeStatus {
			if nsd, ok := ev.Data.(NodeStatusData); ok {
				scheduledNodeEv = &nsd
			}
		}
	}
	if scheduledNodeEv == nil || !scheduledNodeEv.NodeAssigned || scheduledNodeEv.NodeName != "node-1" {
		t.Fatalf("unexpected node_status evidence for scheduled pod: %v", scheduledNodeEv)
	}

	// Verify unscheduled pod node_status evidence
	var unscheduledNodeEv *NodeStatusData
	for _, ev := range pkg.Pods[1].Evidence {
		if ev.Source == SourceNodeStatus {
			if nsd, ok := ev.Data.(NodeStatusData); ok {
				unscheduledNodeEv = &nsd
			}
		}
	}
	if unscheduledNodeEv == nil || unscheduledNodeEv.NodeAssigned || unscheduledNodeEv.NodeName != "<none>" {
		t.Fatalf("unexpected node_status evidence for unscheduled pod: %v", unscheduledNodeEv)
	}
}

func TestBuildEvidencePackage_GPUEvidence(t *testing.T) {
	fake := provider.NewFakeProvider()
	fake.NodesData = []provider.NodeInfo{
		{Name: "gpu-node-1", Status: "Ready"},
		{Name: "cpu-node-1", Status: "Ready"},
	}
	fake.GPUNodesData["gpu-node-1"] = provider.NodeGPUResources{
		NodeName:           "gpu-node-1",
		GPUAvailable:       true,
		GPUResourceExposed: true,
		ResourceName:       "nvidia.com/gpu",
		Model:              "NVIDIA A100-SXM4-80GB",
		Capacity:           "4",
		Allocatable:        "4",
	}

	fake.PodsData["default"] = []provider.PodInfo{
		{
			PodName:   "gpu-pod",
			Namespace: "default",
			NodeName:  "gpu-node-1",
			Containers: []provider.ContainerInfo{
				{
					Name:       "cuda-app",
					Image:      "nvidia/cuda:12.0.0-base-ubuntu22.04",
					GPURequest: "nvidia.com/gpu: 1",
					GPULimit:   "nvidia.com/gpu: 1",
				},
			},
		},
		{
			PodName:   "cpu-pod",
			Namespace: "default",
			NodeName:  "cpu-node-1",
		},
		{
			PodName:   "pending-gpu-pod",
			Namespace: "default",
			NodeName:  "<not yet scheduled>",
		},
	}

	pkg := buildEvidencePackage(fake, "default")
	if len(pkg.Pods) != 3 {
		t.Fatalf("expected 3 pods in evidence package, got %d", len(pkg.Pods))
	}

	// 1. Verify gpu-pod GPU evidence and container requests
	var gpuStatus *GPUStatusData
	for _, ev := range pkg.Pods[0].Evidence {
		if ev.Source == SourceGPUStatus {
			if gsd, ok := ev.Data.(GPUStatusData); ok {
				gpuStatus = &gsd
			}
		}
	}
	if gpuStatus == nil || !gpuStatus.GPUAvailable || !gpuStatus.GPUResourceExposed || gpuStatus.ResourceName != "nvidia.com/gpu" || gpuStatus.GPUCapacity != "4" {
		t.Fatalf("unexpected gpu_status for gpu-pod: %+v", gpuStatus)
	}
	c := pkg.Pods[0].Evidence[0].Data.(PodStatusData).Containers[0]
	if c.GPURequest != "nvidia.com/gpu: 1" || c.GPULimit != "nvidia.com/gpu: 1" {
		t.Fatalf("unexpected container GPU requests: %+v", c)
	}

	// 2. Verify cpu-pod GPU evidence (un-exposed)
	var cpuGpuStatus *GPUStatusData
	for _, ev := range pkg.Pods[1].Evidence {
		if ev.Source == SourceGPUStatus {
			if gsd, ok := ev.Data.(GPUStatusData); ok {
				cpuGpuStatus = &gsd
			}
		}
	}
	if cpuGpuStatus == nil || cpuGpuStatus.GPUAvailable || cpuGpuStatus.GPUResourceExposed {
		t.Fatalf("expected unexposed GPU for cpu-pod, got: %+v", cpuGpuStatus)
	}

	// 3. Verify pending-gpu-pod GPU evidence (unscheduled)
	var pendingGpuStatus *GPUStatusData
	for _, ev := range pkg.Pods[2].Evidence {
		if ev.Source == SourceGPUStatus {
			if gsd, ok := ev.Data.(GPUStatusData); ok {
				pendingGpuStatus = &gsd
			}
		}
	}
	if pendingGpuStatus == nil || pendingGpuStatus.NodeAssigned || pendingGpuStatus.NodeName != "<none>" {
		t.Fatalf("expected unscheduled pending pod to have node_assigned=false, got: %+v", pendingGpuStatus)
	}
}

func TestBuildEvidencePackage_ServiceEvidence(t *testing.T) {
	fake := provider.NewFakeProvider()
	fake.NodesData = []provider.NodeInfo{{Name: "node-1", Status: "Ready"}}

	fake.ServicesData["default"] = []provider.ServiceInfo{
		{
			Name:      "web-service",
			Namespace: "default",
			Type:      "ClusterIP",
			ClusterIP: "10.96.0.50",
			Selector:  map[string]string{"app": "web-nginx"},
			Ports: []provider.ServicePortInfo{
				{Name: "http", Protocol: "TCP", Port: 80, TargetPort: "80"},
			},
			Subsets: []provider.EndpointSubsetInfo{
				{
					ReadyAddressCount: 1,
					Addresses: []provider.EndpointAddressInfo{
						{IP: "10.1.0.20", PodName: "nginx-pod", Ready: true},
					},
					Ports: []provider.EndpointPortInfo{
						{Name: "http", Port: 80, Protocol: "TCP"},
					},
				},
			},
		},
	}

	fake.PodsData["default"] = []provider.PodInfo{
		{
			PodName:   "nginx-pod",
			Namespace: "default",
			NodeName:  "node-1",
			PodIP:     "10.1.0.20",
			Labels:    map[string]string{"app": "web-nginx"},
		},
		{
			PodName:   "standalone-pod",
			Namespace: "default",
			NodeName:  "node-1",
			PodIP:     "10.1.0.21",
			Labels:    map[string]string{"app": "unmatched-app"},
		},
	}

	pkg := buildEvidencePackage(fake, "default")
	if len(pkg.Pods) != 2 {
		t.Fatalf("expected 2 pods in evidence package, got %d", len(pkg.Pods))
	}

	// 1. Verify nginx-pod service_status
	var nginxSvcEv *ServiceStatusData
	for _, ev := range pkg.Pods[0].Evidence {
		if ev.Source == SourceServiceStatus {
			if ssd, ok := ev.Data.(ServiceStatusData); ok {
				nginxSvcEv = &ssd
			}
		}
	}
	if nginxSvcEv == nil || !nginxSvcEv.ServiceFound || !nginxSvcEv.SelectorMatched || !nginxSvcEv.PodInEndpoints || !nginxSvcEv.PodEndpointReady {
		t.Fatalf("unexpected service_status evidence for nginx-pod: %+v", nginxSvcEv)
	}
	if nginxSvcEv.ServiceName != "web-service" || nginxSvcEv.ClusterIP != "10.96.0.50" {
		t.Fatalf("unexpected Service details for nginx-pod: %+v", nginxSvcEv)
	}

	// 2. Verify standalone-pod service_status (no matching service)
	var standaloneSvcEv *ServiceStatusData
	for _, ev := range pkg.Pods[1].Evidence {
		if ev.Source == SourceServiceStatus {
			if ssd, ok := ev.Data.(ServiceStatusData); ok {
				standaloneSvcEv = &ssd
			}
		}
	}
	if standaloneSvcEv == nil || standaloneSvcEv.ServiceFound || standaloneSvcEv.SelectorMatched {
		t.Fatalf("expected standalone-pod to have service_found=false, got: %+v", standaloneSvcEv)
	}
}

