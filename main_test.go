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

func TestBuildEvidencePackage_StorageEvidence(t *testing.T) {
	fake := provider.NewFakeProvider()
	fake.NodesData = []provider.NodeInfo{{Name: "node-1", Status: "Ready"}}

	// Setup StorageClass
	fake.StorageClassesData["standard"] = provider.StorageClassInfo{
		Name:              "standard",
		Provisioner:       "docker.io/hostpath",
		ReclaimPolicy:     "Delete",
		VolumeBindingMode: "Immediate",
	}

	// Setup PVCs
	fake.PVCsData["default"] = map[string]provider.PersistentVolumeClaimInfo{
		"pvc-bound": {
			Name:             "pvc-bound",
			Namespace:        "default",
			Phase:            "Bound",
			StorageClassName: "standard",
			VolumeName:       "pv-100",
			RequestedStorage: "1Gi",
			AccessModes:      []string{"ReadWriteOnce"},
			VolumeMode:       "Filesystem",
		},
		"pvc-pending": {
			Name:             "pvc-pending",
			Namespace:        "default",
			Phase:            "Pending",
			StorageClassName: "non-existent-sc",
			RequestedStorage: "5Gi",
			AccessModes:      []string{"ReadWriteOnce"},
			VolumeMode:       "Filesystem",
		},
	}
	fake.PVsData["pv-100"] = provider.PersistentVolumeInfo{
		Name:             "pv-100",
		Phase:            "Bound",
		Capacity:         "1Gi",
		StorageClassName: "standard",
		AccessModes:      []string{"ReadWriteOnce"},
		VolumeMode:       "Filesystem",
		ClaimRef:         "default/pvc-bound",
		ReclaimPolicy:    "Delete",
		PluginSource:     "hostPath",
	}

	// Setup Pods
	fake.PodsData["default"] = []provider.PodInfo{
		{
			PodName:   "pod-bound-pvc",
			Namespace: "default",
			NodeName:  "node-1",
			Volumes: []provider.PodVolumeInfo{
				{Name: "data", ClaimName: "pvc-bound", Type: "persistentVolumeClaim"},
			},
		},
		{
			PodName:   "pod-pending-pvc",
			Namespace: "default",
			NodeName:  "<not yet scheduled>",
			Volumes: []provider.PodVolumeInfo{
				{Name: "data", ClaimName: "pvc-pending", Type: "persistentVolumeClaim"},
			},
		},
		{
			PodName:   "pod-missing-pvc",
			Namespace: "default",
			NodeName:  "<not yet scheduled>",
			Volumes: []provider.PodVolumeInfo{
				{Name: "data", ClaimName: "pvc-missing", Type: "persistentVolumeClaim"},
			},
		},
		{
			PodName:   "pod-no-pvc",
			Namespace: "default",
			NodeName:  "node-1",
			Volumes: []provider.PodVolumeInfo{
				{Name: "tmp", Type: "emptyDir"},
			},
		},
	}

	pkg := buildEvidencePackage(fake, "default")
	if len(pkg.Pods) != 4 {
		t.Fatalf("expected 4 pods in evidence package, got %d", len(pkg.Pods))
	}

	// 1. Verify pod-bound-pvc
	var boundStorageEv *StorageStatusData
	for _, ev := range pkg.Pods[0].Evidence {
		if ev.Source == SourceStorageStatus {
			if ssd, ok := ev.Data.(StorageStatusData); ok {
				boundStorageEv = &ssd
			}
		}
	}
	if boundStorageEv == nil || !boundStorageEv.PVCFound {
		t.Fatalf("unexpected storage_status for pod-bound-pvc: %+v", boundStorageEv)
	}
	if len(boundStorageEv.PVCs) != 1 || boundStorageEv.PVCs[0].Phase != "Bound" || boundStorageEv.PVCs[0].VolumeName != "pv-100" {
		t.Fatalf("unexpected PVCs for bound pod: %+v", boundStorageEv.PVCs)
	}
	if len(boundStorageEv.PVs) != 1 || boundStorageEv.PVs[0].Name != "pv-100" || boundStorageEv.PVs[0].Capacity != "1Gi" {
		t.Fatalf("unexpected PVs for bound pod: %+v", boundStorageEv.PVs)
	}
	if len(boundStorageEv.StorageClasses) != 1 || boundStorageEv.StorageClasses[0].Name != "standard" {
		t.Fatalf("unexpected StorageClasses for bound pod: %+v", boundStorageEv.StorageClasses)
	}

	// 2. Verify pod-pending-pvc
	var pendingStorageEv *StorageStatusData
	for _, ev := range pkg.Pods[1].Evidence {
		if ev.Source == SourceStorageStatus {
			if ssd, ok := ev.Data.(StorageStatusData); ok {
				pendingStorageEv = &ssd
			}
		}
	}
	if pendingStorageEv == nil || !pendingStorageEv.PVCFound {
		t.Fatalf("unexpected storage_status for pod-pending-pvc: %+v", pendingStorageEv)
	}
	if len(pendingStorageEv.PVCs) != 1 || pendingStorageEv.PVCs[0].Phase != "Pending" {
		t.Fatalf("unexpected PVC phase: %+v", pendingStorageEv.PVCs)
	}
	if len(pendingStorageEv.PVs) != 0 {
		t.Fatalf("expected 0 PVs for pending PVC, got %d", len(pendingStorageEv.PVs))
	}

	// 3. Verify pod-missing-pvc
	var missingStorageEv *StorageStatusData
	for _, ev := range pkg.Pods[2].Evidence {
		if ev.Source == SourceStorageStatus {
			if ssd, ok := ev.Data.(StorageStatusData); ok {
				missingStorageEv = &ssd
			}
		}
	}
	if missingStorageEv == nil || !missingStorageEv.PVCFound {
		t.Fatalf("expected PVCFound=true for missing PVC pod, got %+v", missingStorageEv)
	}
	if len(missingStorageEv.PVCs) != 1 || missingStorageEv.PVCs[0].Phase != "NotFound" || !missingStorageEv.PVCs[0].Unavailable {
		t.Fatalf("expected PVC with Phase=NotFound and Unavailable=true, got %+v", missingStorageEv.PVCs)
	}
	if len(missingStorageEv.Volumes) != 1 || missingStorageEv.Volumes[0].ClaimName != "pvc-missing" {
		t.Fatalf("unexpected volumes for missing PVC pod: %+v", missingStorageEv.Volumes)
	}

	// 4. Verify pod-no-pvc
	var noPvcStorageEv *StorageStatusData
	for _, ev := range pkg.Pods[3].Evidence {
		if ev.Source == SourceStorageStatus {
			if ssd, ok := ev.Data.(StorageStatusData); ok {
				noPvcStorageEv = &ssd
			}
		}
	}
	if noPvcStorageEv == nil || noPvcStorageEv.PVCFound {
		t.Fatalf("expected PVCFound=false for pod without PVC, got %+v", noPvcStorageEv)
	}
	if len(noPvcStorageEv.Volumes) != 1 || noPvcStorageEv.Volumes[0].Type != "emptyDir" {
		t.Fatalf("unexpected volumes for pod-no-pvc: %+v", noPvcStorageEv)
	}
}

func TestBuildEvidencePackage_WorkloadEvidence(t *testing.T) {
	fake := provider.NewFakeProvider()
	fake.NodesData = []provider.NodeInfo{{Name: "node-1", Status: "Ready"}}

	// Setup Deployment & ReplicaSet
	fake.DeploymentsData["default"] = map[string]provider.DeploymentInfo{
		"web-dep": {
			Name:              "web-dep",
			Namespace:         "default",
			DesiredReplicas:   2,
			ReadyReplicas:     2,
			AvailableReplicas: 2,
		},
	}
	fake.ReplicaSetsData["default"] = map[string]provider.ReplicaSetInfo{
		"web-dep-rs": {
			Name:            "web-dep-rs",
			Namespace:       "default",
			DesiredReplicas: 2,
			ReadyReplicas:   2,
			OwnerReferences: []provider.OwnerReferenceInfo{
				{Kind: "Deployment", Name: "web-dep", Controller: true},
			},
		},
	}

	// Setup StatefulSet
	fake.StatefulSetsData["default"] = map[string]provider.StatefulSetInfo{
		"db-sts": {
			Name:            "db-sts",
			Namespace:       "default",
			DesiredReplicas: 1,
			ReadyReplicas:   1,
			ServiceName:     "db-svc",
		},
	}

	// Setup CronJob & Job
	fake.CronJobsData["default"] = map[string]provider.CronJobInfo{
		"daily-cron": {
			Name:      "daily-cron",
			Namespace: "default",
			Schedule:  "0 0 * * *",
		},
	}
	fake.JobsData["default"] = map[string]provider.JobInfo{
		"daily-job": {
			Name:      "daily-job",
			Namespace: "default",
			Succeeded: 1,
			OwnerReferences: []provider.OwnerReferenceInfo{
				{Kind: "CronJob", Name: "daily-cron", Controller: true},
			},
		},
	}

	// Setup Pods
	fake.PodsData["default"] = []provider.PodInfo{
		{
			PodName:   "pod-deploy-123",
			Namespace: "default",
			NodeName:  "node-1",
			OwnerReferences: []provider.OwnerReferenceInfo{
				{Kind: "ReplicaSet", Name: "web-dep-rs", Controller: true},
			},
		},
		{
			PodName:   "pod-sts-0",
			Namespace: "default",
			NodeName:  "node-1",
			OwnerReferences: []provider.OwnerReferenceInfo{
				{Kind: "StatefulSet", Name: "db-sts", Controller: true},
			},
		},
		{
			PodName:   "pod-job-abc",
			Namespace: "default",
			NodeName:  "node-1",
			OwnerReferences: []provider.OwnerReferenceInfo{
				{Kind: "Job", Name: "daily-job", Controller: true},
			},
		},
		{
			PodName:         "pod-standalone",
			Namespace:       "default",
			NodeName:        "node-1",
			OwnerReferences: nil,
		},
	}

	pkg := buildEvidencePackage(fake, "default")
	if len(pkg.Pods) != 4 {
		t.Fatalf("expected 4 pods in evidence package, got %d", len(pkg.Pods))
	}

	// 1. Verify pod-deploy-123 (Pod -> ReplicaSet -> Deployment)
	var deployEv *WorkloadStatusData
	for _, ev := range pkg.Pods[0].Evidence {
		if ev.Source == SourceWorkloadStatus {
			if wsd, ok := ev.Data.(WorkloadStatusData); ok {
				deployEv = &wsd
			}
		}
	}
	if deployEv == nil || !deployEv.ControllerFound || deployEv.DirectOwner == nil || deployEv.DirectOwner.Kind != "ReplicaSet" {
		t.Fatalf("unexpected direct owner for pod-deploy-123: %+v", deployEv)
	}
	if deployEv.RootController == nil || deployEv.RootController.Kind != "Deployment" || deployEv.RootController.Name != "web-dep" {
		t.Fatalf("unexpected root controller for pod-deploy-123: %+v", deployEv)
	}
	if deployEv.Deployment == nil || deployEv.Deployment.DesiredReplicas != 2 {
		t.Fatalf("unexpected deployment data for pod-deploy-123: %+v", deployEv.Deployment)
	}

	// 2. Verify pod-sts-0 (Pod -> StatefulSet)
	var stsEv *WorkloadStatusData
	for _, ev := range pkg.Pods[1].Evidence {
		if ev.Source == SourceWorkloadStatus {
			if wsd, ok := ev.Data.(WorkloadStatusData); ok {
				stsEv = &wsd
			}
		}
	}
	if stsEv == nil || !stsEv.ControllerFound || stsEv.DirectOwner.Kind != "StatefulSet" || stsEv.StatefulSet == nil {
		t.Fatalf("unexpected statefulset evidence for pod-sts-0: %+v", stsEv)
	}
	if stsEv.StatefulSet.ServiceName != "db-svc" {
		t.Fatalf("unexpected service name in statefulset: %+v", stsEv.StatefulSet)
	}

	// 3. Verify pod-job-abc (Pod -> Job -> CronJob)
	var jobEv *WorkloadStatusData
	for _, ev := range pkg.Pods[2].Evidence {
		if ev.Source == SourceWorkloadStatus {
			if wsd, ok := ev.Data.(WorkloadStatusData); ok {
				jobEv = &wsd
			}
		}
	}
	if jobEv == nil || !jobEv.ControllerFound || jobEv.DirectOwner.Kind != "Job" || jobEv.RootController.Kind != "CronJob" {
		t.Fatalf("unexpected job/cronjob chain for pod-job-abc: %+v", jobEv)
	}
	if jobEv.CronJob == nil || jobEv.CronJob.Schedule != "0 0 * * *" {
		t.Fatalf("unexpected cronjob data: %+v", jobEv.CronJob)
	}

	// 4. Verify pod-standalone (Standalone Pod)
	var standaloneEv *WorkloadStatusData
	for _, ev := range pkg.Pods[3].Evidence {
		if ev.Source == SourceWorkloadStatus {
			if wsd, ok := ev.Data.(WorkloadStatusData); ok {
				standaloneEv = &wsd
			}
		}
	}
	if standaloneEv == nil || standaloneEv.ControllerFound {
		t.Fatalf("expected ControllerFound=false for standalone pod, got: %+v", standaloneEv)
	}
}



