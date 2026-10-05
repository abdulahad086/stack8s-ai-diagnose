package provider

import (
	"context"
	"testing"
)

func createTestFakeProvider() *FakeProvider {
	fp := NewFakeProvider()
	fp.PodsData["default"] = []PodInfo{
		{
			PodName:   "test-pod-01",
			Namespace: "default",
			Phase:     "Pending",
			NodeName:  "node-01",
			Containers: []ContainerInfo{
				{
					Name:  "app",
					Image: "nginx:invalid-tag",
					Ready: false,
					State: "waiting (ErrImagePull)",
				},
			},
		},
	}
	fp.EventsData["test-pod-01"] = []EventInfo{
		{
			Reason:  "Failed",
			Message: "Failed to pull image nginx:invalid-tag: rpc error: code = NotFound",
			Type:    "Warning",
		},
	}
	fp.NodesData = []NodeInfo{
		{
			Name:   "node-01",
			Status: "Ready",
		},
	}
	fp.GPUNodesData["node-01"] = NodeGPUResources{
		NodeName:     "node-01",
		GPUAvailable: true,
		Capacity:     "1",
	}
	return fp
}

func TestToolDispatcher_ValidGetPodStatus(t *testing.T) {
	fp := createTestFakeProvider()
	dispatcher := NewToolDispatcher(fp)

	call := ToolCall{
		Name: "get_pod_status",
		Arguments: map[string]any{
			"namespace": "default",
			"pod":       "test-pod-01",
		},
	}

	res := dispatcher.Execute(context.Background(), call)
	if res.Error != "" {
		t.Fatalf("expected no error, got: %s", res.Error)
	}

	podInfo, ok := res.Result.(PodInfo)
	if !ok {
		t.Fatalf("expected PodInfo result, got: %T", res.Result)
	}
	if podInfo.PodName != "test-pod-01" {
		t.Errorf("expected pod name test-pod-01, got: %s", podInfo.PodName)
	}
	if podInfo.Phase != "Pending" {
		t.Errorf("expected phase Pending, got: %s", podInfo.Phase)
	}
}

func TestToolDispatcher_ValidGetPodEvents(t *testing.T) {
	fp := createTestFakeProvider()
	dispatcher := NewToolDispatcher(fp)

	call := ToolCall{
		Name: "get_pod_events",
		Arguments: map[string]any{
			"namespace": "default",
			"pod":       "test-pod-01",
		},
	}

	res := dispatcher.Execute(context.Background(), call)
	if res.Error != "" {
		t.Fatalf("expected no error, got: %s", res.Error)
	}

	events, ok := res.Result.([]EventInfo)
	if !ok {
		t.Fatalf("expected []EventInfo result, got: %T", res.Result)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Reason != "Failed" {
		t.Errorf("expected event reason Failed, got: %s", events[0].Reason)
	}
}

func TestToolDispatcher_InvalidToolName(t *testing.T) {
	fp := createTestFakeProvider()
	dispatcher := NewToolDispatcher(fp)

	call := ToolCall{
		Name: "invalid_tool_foo",
		Arguments: map[string]any{
			"namespace": "default",
		},
	}

	res := dispatcher.Execute(context.Background(), call)
	if res.Error == "" {
		t.Fatalf("expected error for invalid tool name, got nil")
	}
}

func TestToolDispatcher_MissingRequiredArgument(t *testing.T) {
	fp := createTestFakeProvider()
	dispatcher := NewToolDispatcher(fp)

	call := ToolCall{
		Name: "get_pod_status",
		Arguments: map[string]any{
			"namespace": "default",
			// "pod" argument is missing
		},
	}

	res := dispatcher.Execute(context.Background(), call)
	if res.Error == "" {
		t.Fatalf("expected error for missing required argument 'pod', got nil")
	}
}

func TestToolDispatcher_EmptyRequiredArgument(t *testing.T) {
	fp := createTestFakeProvider()
	dispatcher := NewToolDispatcher(fp)

	call := ToolCall{
		Name: "get_pod_status",
		Arguments: map[string]any{
			"namespace": "default",
			"pod":       "   ",
		},
	}

	res := dispatcher.Execute(context.Background(), call)
	if res.Error == "" {
		t.Fatalf("expected error for empty required argument 'pod', got nil")
	}
}

func TestToolDispatcher_ProviderResourceNotFound(t *testing.T) {
	fp := createTestFakeProvider()
	dispatcher := NewToolDispatcher(fp)

	call := ToolCall{
		Name: "get_pod_status",
		Arguments: map[string]any{
			"namespace": "default",
			"pod":       "non-existent-pod",
		},
	}

	res := dispatcher.Execute(context.Background(), call)
	if res.Error == "" {
		t.Fatalf("expected error for non-existent pod, got nil")
	}
}

func TestToolDispatcher_GetNodeStatus(t *testing.T) {
	fp := createTestFakeProvider()
	dispatcher := NewToolDispatcher(fp)

	call := ToolCall{
		Name: "get_node_status",
		Arguments: map[string]any{
			"node_name": "node-01",
		},
	}

	res := dispatcher.Execute(context.Background(), call)
	if res.Error != "" {
		t.Fatalf("expected no error, got: %s", res.Error)
	}

	nodeInfo, ok := res.Result.(NodeInfo)
	if !ok {
		t.Fatalf("expected NodeInfo result, got: %T", res.Result)
	}
	if nodeInfo.Name != "node-01" {
		t.Errorf("expected node name node-01, got: %s", nodeInfo.Name)
	}
}
