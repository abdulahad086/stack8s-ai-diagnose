package main

import (
	"testing"
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
