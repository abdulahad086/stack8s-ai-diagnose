package provider

import (
	"context"
	"testing"
)

func TestFakeProviderInterfaceCompliance(t *testing.T) {
	fake := NewFakeProvider()
	fake.NodesData = []NodeInfo{
		{Name: "fake-node-1", Status: "Ready"},
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
					Name:  "app",
					Image: "invalid/image:v1",
					State: "Waiting (ImagePullBackOff)",
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

	// Verify InfrastructureProvider interface methods
	ctx := context.Background()

	if fake.Name() != "fake-provider" {
		t.Fatalf("expected name fake-provider, got %s", fake.Name())
	}

	nodes, err := fake.GetNodes(ctx)
	if err != nil || len(nodes) != 1 || nodes[0].Name != "fake-node-1" {
		t.Fatalf("unexpected nodes output: %v, err: %v", nodes, err)
	}

	pods, err := fake.GetPods(ctx, "default")
	if err != nil || len(pods) != 1 || pods[0].PodName != "fake-pod-1" {
		t.Fatalf("unexpected pods output: %v, err: %v", pods, err)
	}

	events, err := fake.GetPodEvents(ctx, "default", "fake-pod-1")
	if err != nil || len(events) != 1 || events[0].Reason != "ImagePullBackOff" {
		t.Fatalf("unexpected events output: %v, err: %v", events, err)
	}

	logs, err := fake.GetContainerLogs(ctx, "default", "fake-pod-1", "app", 100)
	if err != nil || !logs.Unavailable {
		t.Fatalf("unexpected logs output: %v, err: %v", logs, err)
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
