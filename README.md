# stack8s-ai-diagnose

A Go prototype that connects to a real Kubernetes cluster and collects diagnostic information — nodes, pod status, events, and container logs — using only the official `client-go` library. No AI, no mock data, no Stack8s integration.

---

## Feature 1 — Kubernetes Connectivity

### What it does
Verifies that the Go program can connect to a real Kubernetes cluster and list all cluster nodes with their Ready status.

### Current Kubernetes Context
```
docker-desktop
```

### How it connects
1. Reads kubeconfig from `$KUBECONFIG` or `~/.kube/config`.
2. Builds a REST config via `clientcmd.BuildConfigFromFlags`.
3. Creates a typed clientset via `kubernetes.NewForConfig`.
4. Calls `clientset.CoreV1().Nodes().List(...)`.

No credentials or tokens are printed.

### How to run
```bash
go run main.go
# or
go build -o k8s-check . && ./k8s-check
```

### Expected output (Feature 1 section)
```
Connected to Kubernetes cluster via: https://127.0.0.1:6443

Nodes (1):
NAME              STATUS
----              ------
docker-desktop    Ready
```

---

## Feature 2 — Pod Status Collector

### What it does
Retrieves and displays the real status of all pods in the `default` namespace.

### Fields collected per pod

| # | Field | Source |
|---|-------|--------|
| 1 | Pod name | `pod.Name` |
| 2 | Namespace | `pod.Namespace` |
| 3 | Pod phase | `pod.Status.Phase` |
| 4 | Creation time | `pod.CreationTimestamp` |
| 5 | Node name | `pod.Spec.NodeName` |
| 6 | Container name(s) | `pod.Spec.Containers[*].Name` |
| 7 | Container image(s) | `pod.Spec.Containers[*].Image` |
| 8 | Container ready state | `pod.Status.ContainerStatuses[*].Ready` |
| 9 | Restart count | `pod.Status.ContainerStatuses[*].RestartCount` |
| 10 | Current container state | `pod.Status.ContainerStatuses[*].State` |
| 11 | Last termination reason | `pod.Status.ContainerStatuses[*].LastTerminationState.Terminated.Reason` |
| 12 | Last termination exit code | `pod.Status.ContainerStatuses[*].LastTerminationState.Terminated.ExitCode` |

### Security note
No Secret values, environment variables, passwords, tokens, certificates, or kubeconfig credentials are printed.

### Namespace
`default` — hardcoded for the MVP.

---

## Feature 3 — Kubernetes Events Collector

### What Kubernetes Events are
Kubernetes Events are structured records written by the control plane and kubelet when something significant happens to a workload.

### Why Events are useful for diagnosis
- Show the exact sequence of actions Kubernetes took for a pod.
- Surface warnings (`BackOff`, `Failed`, `OOMKilled`) before a pod enters a crash loop.
- Provide human-readable messages with precise timing.

### Fields collected per event

| # | Field | Source |
|---|-------|--------|
| 1 | Reason | `event.Reason` |
| 2 | Type | `event.Type` |
| 3 | Message | `event.Message` |
| 4 | Involved object kind | `event.InvolvedObject.Kind` |
| 5 | Involved object name | `event.InvolvedObject.Name` |
| 6 | Namespace | `event.Namespace` |
| 7 | First observed | `event.FirstTimestamp` |
| 8 | Last observed | `event.LastTimestamp` |
| 9 | Count | `event.Count` |
| 10 | Source component | `event.Source.Component` |

### How events are filtered
Field selector: `involvedObject.name=<podName>,involvedObject.namespace=<ns>,involvedObject.kind=Pod`

### How to verify with kubectl
```bash
kubectl get events -n default --field-selector involvedObject.name=<pod-name> --sort-by='.lastTimestamp'
kubectl describe pod <pod-name> -n default
```

---

## Feature 4 — Container Logs Collector

### What container logs are
Container logs are the raw stdout/stderr output produced by the application running inside a Kubernetes container.

### How logs are retrieved
Uses `clientset.CoreV1().Pods(namespace).GetLogs(podName, &PodLogOptions{TailLines: &limit})`. Snapshot only — never streams indefinitely.

### Current line limit
**100 lines** (most recent). Defined by the `defaultLogTailLines` constant.

### Security considerations
- No Secret values are collected or printed.
- Log content is passed through from the Kubernetes API as-is.
- Logs are bounded to 100 lines per container.

### How to verify with kubectl
```bash
kubectl logs <pod-name> -n default -c <container-name> --tail=100
```

---

## Feature 5 — Unified Evidence Package

### What an Evidence Package is

The Evidence Package is a single, machine-readable JSON document that combines the Pod status, Kubernetes Events, and Container Logs already collected by Features 2–4 into one normalised structure.

> **Training data is separate from runtime evidence.**
> The Evidence Package contains information collected from the actual Kubernetes cluster. Nothing is invented, inferred, or mocked.

Its purpose is to serve as structured input for a future diagnostic step (Needle 3). Feature 5 only builds and serialises the package — no AI is involved.

### Why normalisation is required

Features 2–4 print human-readable output to stdout. The Evidence Package:
- provides machine-readable JSON for downstream consumers
- assigns every piece of evidence a **unique ID** (`E1`, `E2`, …)
- records a consistent collection timestamp
- clearly represents partial failures (e.g. an event API error) without fabricating data

### Evidence ID concept

Every individual piece of evidence is assigned a sequential ID within a single run:

```
E1  → pod_status        (first pod)
E2  → kubernetes_event  (first event for that pod)
E3  → kubernetes_event  ...
E7  → container_logs    (first container logs)
E8  → pod_status        (second pod)
E9  → container_logs    (second pod's container logs)
```

IDs are:
- unique within a collection run
- sequential and deterministic (same pod order → same IDs)
- never duplicated within one package

### Evidence source types

| Source constant | Value | Meaning |
|-----------------|-------|---------|
| `SourcePodStatus` | `pod_status` | Feature 2 pod + container status |
| `SourceKubernetesEvent` | `kubernetes_event` | Feature 3 Kubernetes event |
| `SourceContainerLogs` | `container_logs` | Feature 4 container log snapshot |

### Go types introduced

| Type | Purpose |
|------|---------|
| `EvidenceSource` | Typed string constant for source identification |
| `ContainerEvidenceData` | Normalised container status (F2) |
| `PodStatusData` | Normalised pod status (F2) |
| `KubernetesEventData` | Normalised Kubernetes event (F3) |
| `ContainerLogsData` | Normalised log snapshot (F4) |
| `EvidenceItem` | One evidence item: `{id, source, data}` |
| `PodEvidencePackage` | All evidence for one pod |
| `EvidencePackage` | Top-level: cluster + namespace + all pods |
| `EventCollectionResult` | Internal: clearly separates empty vs API error |

### JSON structure

```json
{
  "cluster": "https://127.0.0.1:6443",
  "namespace": "default",
  "collected_at": "2026-09-30T08:03:45Z",
  "pods": [
    {
      "pod_name": "test-logger",
      "namespace": "default",
      "collected_at": "2026-09-30T08:03:45Z",
      "evidence": [
        {
          "id": "E1",
          "source": "pod_status",
          "data": { ... }
        },
        {
          "id": "E2",
          "source": "kubernetes_event",
          "data": { ... }
        },
        {
          "id": "E7",
          "source": "container_logs",
          "data": { ... }
        }
      ]
    }
  ]
}
```

### Error representation

The package clearly distinguishes between:

| Situation | Representation |
|-----------|---------------|
| No events exist for the pod | No `kubernetes_event` items in evidence list |
| Event API call failed | One `kubernetes_event` item with `"api_error": true, "api_error_message": "..."` |
| Container has no log output | `container_logs` item with `"unavailable": true, "unavailable_reason": "no log output available"` |
| Container not yet started | `container_logs` item with `"unavailable": true, "unavailable_reason": "container not yet started"` |
| Log API call failed | `container_logs` item with `"api_error": true, "api_error_message": "..."` |

**No fake evidence is ever inserted. A failure is always represented as a failure.**

### Security considerations
- No Secret values, credentials, tokens, certificates, or kubeconfig contents are included in the package.
- Log content is bounded to 100 lines per container.
- Event messages are passed through from the Kubernetes API unchanged.
- The package does not persist to disk (printed to stdout only in this MVP).

### How to run
```bash
go run main.go
```

All features (F1–F5) run in a single invocation. The Evidence Package JSON is printed at the end.

### How to verify
```bash
# Validate JSON is well-formed
go run main.go | tail -200 | python3 -m json.tool > /dev/null && echo "Valid JSON"
```

### Actual test result (Feature 5)

**Pods collected:** `test-logger` (Completed), `test-nginx` (Running)

**Evidence summary:**
```
  Cluster      : https://127.0.0.1:6443
  Namespace    : default
  Collected At : 2026-09-30T08:03:45Z
  Pods         : 2
  Evidence Count: 9

  Pod: test-logger
    E1 → pod_status
    E2 → kubernetes_event  (Scheduled)
    E3 → kubernetes_event  (Pulling)
    E4 → kubernetes_event  (Pulled)
    E5 → kubernetes_event  (Created)
    E6 → kubernetes_event  (Started)
    E7 → container_logs    (6 lines)

  Pod: test-nginx
    E8 → pod_status
    E9 → container_logs    (19 lines)
```

> **Note on missing test-nginx events:** Kubernetes Events have a default TTL of ~1 hour. At the time of the Feature 5 run, `test-nginx` was ~65 minutes old so its events had already been garbage-collected by Kubernetes. The collector correctly returns **no** event items — it does not fabricate them. This is an example of the "no events exist" vs "API error" distinction working correctly.

**JSON round-trip:** The generated JSON was verified to parse back into `EvidencePackage` without error. ✅

### Limitations
- Evidence Package is printed to stdout only (no file persistence yet).
- Only `default` namespace is queried.
- Log tail limit is 100 lines per container.
- Event TTL expiry is a Kubernetes limitation, not a collector bug.

### Test pods

```bash
kubectl delete pod test-nginx test-logger -n default
```

---

## Dependencies

| Package | Version |
|---------|---------|
| `k8s.io/client-go` | v0.30.2 |
| `k8s.io/apimachinery` | v0.30.2 |
| `k8s.io/api` | v0.30.2 |

```bash
go mod tidy
```
