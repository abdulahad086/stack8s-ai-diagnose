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

---

## Feature 6 — Controlled ImagePullBackOff Failure Test

> **This is a controlled local test. It is not an AI diagnosis.**

### Why controlled failures are useful

Before trusting a diagnostic pipeline in production, it must be verified against known, observable failure modes. Feature 6 creates one harmless, reproducible failure locally and confirms that the existing evidence collectors (F2 Pod status, F3 Events, F4 Logs, F5 Evidence Package) capture it correctly — with no invented data.

### Test workload

| Property | Value |
|----------|-------|
| Pod name | `test-image-failure` |
| Namespace | `default` |
| Container | `app` |
| Image | `stack8s-demo/nonexistent-image:does-not-exist` |
| Restart policy | `Never` |
| Privileged | No |
| Host network | No |
| Mounts | None |
| Secrets accessed | None |
| Intended failure | Image pull failure only |

### Expected Kubernetes behaviour

```
Pod scheduled → Image pull attempted → Pull fails (repo does not exist)
→ ErrImagePull → ImagePullBackOff → Pod stays Pending
```

### Actual observed Pod state (kubectl)

```
NAME                 READY   STATUS           RESTARTS   AGE
test-image-failure   0/1     ErrImagePull     0          2m11s
```

Container state from `kubectl describe`:
```
State:    Waiting
  Reason: ErrImagePull
Ready:    False
```

### Actual observed Events (kubectl describe)

```
Type     Reason     Age                  From               Message
----     ------     ----                 ----               -------
Normal   Scheduled  2m11s                default-scheduler  Successfully assigned default/test-image-failure to docker-desktop
Warning  Failed     39s (x6 over 2m3s)  kubelet            Error: ImagePullBackOff
Normal   Pulling    26s (x4 over 2m5s)  kubelet            Pulling image "stack8s-demo/nonexistent-image:does-not-exist"
Warning  Failed     25s (x4 over 2m3s)  kubelet            Failed to pull image "stack8s-demo/nonexistent-image:does-not-exist": Error response from daemon: pull access denied for stack8s-demo/nonexistent-image, repository does not exist or may require 'docker login'
Warning  Failed     25s (x4 over 2m3s)  kubelet            Error: ErrImagePull
Normal   BackOff    9s  (x7 over 2m3s)  kubelet            Back-off pulling image "stack8s-demo/nonexistent-image:does-not-exist"
```

### Container log behaviour

Because the container never started (image was never pulled), no container logs exist. The Go collector correctly reported:

```json
{
  "unavailable": true,
  "unavailable_reason": "container not yet started",
  "api_error": false,
  "lines_retrieved": 0
}
```

This correctly distinguishes **"no logs because container never started"** from **"log API call failed"**. No fake logs were inserted.

### Evidence Package result (Go collector)

```
Evidence Count: 8  (for test-image-failure alone)

  Pod: test-image-failure
    E1 → pod_status
    E2 → kubernetes_event   (Scheduled)
    E3 → kubernetes_event   (Failed / ImagePullBackOff)
    E4 → kubernetes_event   (Pulling)
    E5 → kubernetes_event   (Failed / pull access denied)
    E6 → kubernetes_event   (Failed / ErrImagePull)
    E7 → kubernetes_event   (BackOff)
    E8 → container_logs     (unavailable: container not yet started)
```

Captured in `pod_status` evidence (E1):
```json
{
  "phase": "Pending",
  "containers": [{
    "name": "app",
    "image": "stack8s-demo/nonexistent-image:does-not-exist",
    "ready": false,
    "restart_count": 0,
    "state": "Waiting (ImagePullBackOff: Back-off pulling image \"stack8s-demo/nonexistent-image:does-not-exist\")"
  }]
}
```

### kubectl comparison

| Field | kubectl | Go collector |
|-------|---------|-------------|
| Phase | Pending | Pending ✅ |
| Container state | Waiting / ErrImagePull | Waiting (ImagePullBackOff) ✅ |
| Image | `stack8s-demo/nonexistent-image:does-not-exist` | identical ✅ |
| Ready | False | false ✅ |
| Restart count | 0 | 0 ✅ |
| Events collected | 6 distinct event entries | 6 evidence items E2–E7 ✅ |
| Log availability | N/A (never started) | unavailable, api_error=false ✅ |

### Unaffected workloads confirmed

`kubectl get pods -A` after test confirmed:

| Pod | Namespace | Status |
|-----|-----------|--------|
| `test-nginx` | default | Running ✅ unaffected |
| `test-logger` | default | Completed ✅ unaffected |
| All `kube-system` pods | kube-system | Running ✅ unaffected |

### Cleanup

The `test-image-failure` pod was deleted after testing:

```bash
kubectl delete pod test-image-failure -n default
```

Confirmed removed. Only `test-nginx` and `test-logger` remain.

---

## Feature 7 — Deterministic Diagnosis Engine

> **Feature 7 does not use AI. It provides a deterministic baseline against which the future Needle 3 diagnostic system can be evaluated.**

### Purpose & Why Deterministic Rules are Needed Before AI

Before introducing AI models or Needle 3, a reliable diagnostic pipeline requires a trusted, deterministic baseline. AI diagnosis can generate hallucinations or probabilistic guesses if unsupported by facts.

The deterministic diagnosis engine evaluates explicit rules against the real **Evidence Package** (Feature 5). It operates under strict rules:
- **Zero AI / LLM / probabilistic scoring used.**
- **Never guess.** If evidence is insufficient to establish a supported root cause, return `UNKNOWN`.
- **Traceable trust:** Every `CONFIRMED` diagnosis cites the exact `EvidenceItem` IDs supporting it.

### Supported Diagnosis Codes

| Code | Description | Implementation Status |
|------|-------------|-----------------------|
| `IMAGE_PULL_FAILURE` | Container image could not be pulled | Supported (Rule 2) |
| `OOM_KILLED` | Container killed due to memory limit | Supported (Rule 1) |
| `NODE_NOT_READY` | Node condition indicates NotReady | Supported (Rule 3) |
| `INSUFFICIENT_MEMORY` | Scheduling failure due to memory | Reserved constant |
| `INSUFFICIENT_CPU` | Scheduling failure due to CPU | Reserved constant |
| `INSUFFICIENT_GPU` | Scheduling failure due to GPU | Reserved constant |
| `DB_AUTH_FAILURE` | Database authentication failure | Reserved constant |
| `DB_CONNECTION_FAILURE` | Database network connection failure | Reserved constant |
| `UNKNOWN` | Insufficient evidence for root cause | Default state |

### Rule Conditions & Precedence

Rules are evaluated in strict order of causal strength:

1. **Rule 1: `OOM_KILLED` (Priority 1)**
   - **Condition:** Pod container `LastTermReason == "OOMKilled"` OR container state contains `"OOMKilled"` OR Kubernetes event reason/message contains `"OOMKilled"` / `"Out of memory: Kill process"`.
   - **Result:** `status = CONFIRMED`, `diagnosis_code = OOM_KILLED`.
   - **Cites:** Exact matching `pod_status` and `kubernetes_event` Evidence IDs.

2. **Rule 2: `IMAGE_PULL_FAILURE` (Priority 2)**
   - **Condition:** Pod container state contains `"ErrImagePull"` or `"ImagePullBackOff"` OR Kubernetes event reason is `"ErrImagePull"`/`"ImagePullBackOff"` or event message contains `"Failed to pull image"` / `"pull access denied"`.
   - **Result:** `status = CONFIRMED`, `diagnosis_code = IMAGE_PULL_FAILURE`.
   - **Cites:** Exact matching `pod_status` and `kubernetes_event` Evidence IDs.

3. **Rule 3: `NODE_NOT_READY` (Priority 3)**
   - **Condition:** Kubernetes event reason is `"NodeNotReady"` or event message contains `"NodeNotReady"`.
   - **Result:** `status = CONFIRMED`, `diagnosis_code = NODE_NOT_READY`.
   - **Cites:** Matching `kubernetes_event` Evidence IDs.

4. **Rule 4: `UNKNOWN` (Priority 4 - Default)**
   - **Condition:** No explicit root-cause rule matched.
   - **Result:** `status = UNKNOWN`, `diagnosis_code = UNKNOWN`.
   - **Cites:** None (`evidence_ids = []`).

### Symptom vs. Root Cause Distinction

- **`CrashLoopBackOff` is a symptom/state**, not a root cause. If a pod enters `CrashLoopBackOff` because of an `OOMKilled` container termination, Rule 1 evaluates first and identifies `OOM_KILLED` as the root cause. If a pod enters `CrashLoopBackOff` without explicit root-cause evidence, the engine returns `UNKNOWN`.
- **`Pending` is a state**, not a root cause. A pending pod returns `UNKNOWN` unless explicit evidence (e.g. image pull error or node error) proves the cause.

### Controlled Test Workloads & Results

Controlled failures were executed on the local cluster to test all diagnosis outcomes:

| Test Workload | Container Image / Limit | Observed Outcome | Status | Diagnosis Code | Evidence IDs |
|---------------|------------------------|------------------|--------|----------------|--------------|
| `test-image-failure` | `stack8s-demo/nonexistent-image:does-not-exist` | `ImagePullBackOff` | `CONFIRMED` | `IMAGE_PULL_FAILURE` | `E1, E5, E12` |
| `test-oom-failure` | `alpine:latest` (limit: `10Mi`) | `OOMKilled (exit: 137)` | `CONFIRMED` | `OOM_KILLED` | `E19` |
| `test-logger` | `busybox:1.36` | `Completed (exit: 0)` | `UNKNOWN` | `UNKNOWN` | `none` |
| `test-nginx` | `nginx:1.27-alpine` | `Running` | `UNKNOWN` | `UNKNOWN` | `none` |

### Unit Test Coverage

Automated Go unit tests (`main_test.go`) verify rule execution:
- `TestDiagnosePod_ImagePullBackOff`: PASS
- `TestDiagnosePod_ErrImagePull`: PASS
- `TestDiagnosePod_OOMKilled`: PASS
- `TestDiagnosePod_CrashLoopBackOff_NoRootCause`: PASS (`UNKNOWN`)
- `TestDiagnosePod_Pending_NoCause`: PASS (`UNKNOWN`)
- `TestDiagnosePod_NormalRunningPod`: PASS (`UNKNOWN`)
- `TestDiagnosePod_Precedence_OOMOverBackoff`: PASS (`OOM_KILLED` precedence)

All test workloads were cleaned up after verification.

---

## Feature 8 — Diagnostic Dataset Builder

> **Feature 8 prepares the dataset. Needle 3 fine-tuning is a later feature.**

### Purpose & Architecture

Feature 8 converts verified Kubernetes diagnostic cases into a machine-readable dataset for future Needle 3 fine-tuning and evaluation.

- **Conceptual Separation:** Training data teaches diagnostic tool selection and argument extraction. Runtime evidence comes from actual Kubernetes cluster collectors.
- **Dataset Location:** Files are generated under `dataset/` (`tools.json`, `train.jsonl`, `validation.jsonl`, `test.jsonl`, `README.md`).

### Official Needle 3 JSONL Format

Each record in `train.jsonl`, `validation.jsonl`, and `test.jsonl` follows the official Needle schema:

```json
{
  "query": "Why is pod test-image-failure failing in namespace default?",
  "tools": [ ... ],
  "answers": [
    {
      "name": "get_pod_status",
      "arguments": {
        "namespace": "default",
        "pod": "test-image-failure"
      }
    },
    {
      "name": "get_pod_events",
      "arguments": {
        "namespace": "default",
        "pod": "test-image-failure"
      }
    },
    {
      "name": "get_container_logs",
      "arguments": {
        "container": "app",
        "namespace": "default",
        "pod": "test-image-failure"
      }
    }
  ],
  "reasoning": "The pod name 'test-image-failure', namespace 'default', and container 'app' are explicitly specified in the query."
}
```

### Diagnostic Tools (`dataset/tools.json`)

Tools represent implemented collectors only:
1. `get_pod_status` (required: `namespace`, `pod`)
2. `get_pod_events` (required: `namespace`, `pod`)
3. `get_container_logs` (required: `namespace`, `pod`, `container`)

### Grounding Rules & Secret Redaction

- **Strict Grounding:** Tool call arguments must be derived from the query context. Placeholders like `<pod_name>` or `TODO` are prohibited.
- **Secret Redaction:** Passwords, tokens, API keys, and certificate strings are scanned and redacted (`[REDACTED]`).

### Dataset Splits & Data Leakage Prevention

| Split | File | Count | Percentage |
|-------|------|-------|------------|
| **Train** | `dataset/train.jsonl` | 17 | 63.0% |
| **Validation** | `dataset/validation.jsonl` | 5 | 18.5% |
| **Holdout Test** | `dataset/test.jsonl` | 5 | 18.5% |

**Leakage Prevention:** Split strictly by incident/pod context. Holdout test set (`dataset/test.jsonl`) contains unseen pod names (`invoice-generator`, `search-indexer-pod`, `metrics-exporter`), unseen namespaces (`production-eu`, `billing-v2`), and novel phrasings.

### UNKNOWN & Negative Examples

- **Negative Examples (`answers: []`):** Teach the model not to trigger tool calls on general inquiries or non-diagnostic statements.
- **UNKNOWN Cases:** Ground-truth cases where evidence is insufficient map to `UNKNOWN`.

### Dataset Validation & Statistics

Integrated into Go test suite (`dataset_test.go` and `dataset_builder.go`):
- Total Examples: 27
- Real Verified Cases: 4
- Synthetic Grounded Cases: 23
- Negative Cases (`answers: []`): 5
- UNKNOWN Diagnostic Cases: 6
- Validation & Leakage Check: PASSED ✅

---

## Feature 9 — Provider-Based Diagnostic Architecture

> **Feature 9 implements architectural separation of concerns. It decouples the core diagnostic engine from specific infrastructure client libraries.**

### Purpose & Architecture

Feature 9 introduces a provider-based abstraction (`provider.InfrastructureProvider`) that isolates infrastructure queries from diagnostic logic:

```
              Diagnostic Engine
                     │
                     ▼
           InfrastructureProvider
                /           \
               /             \
  LocalKubernetesProvider   Stack8sProvider
          NOW                  FUTURE
```

### Key Changes
- **Client-Go Isolation:** `k8s.io/client-go` dependency is 100% encapsulated inside `provider/local_k8s.go`.
- **Domain Types:** Queries return clean domain structs (`NodeInfo`, `PodInfo`, `ContainerInfo`, `EventInfo`, `ContainerLogInfo`).
- **Provider Factory:** Configuration via `STACK8S_PROVIDER` environment variable (`local-kubernetes` default, `stack8s` placeholder).
- **Provider-Agnostic Boundaries:** `buildEvidencePackage`, `diagnoseEvidence`, and dataset tooling operate on domain models and `EvidencePackage` only.
- **Future Integration Contract:** Documented placeholder in `provider/stack8s_provider.go` ready for future Stack8s infrastructure backend integration without altering diagnostic engine code.
- **Architecture Specification:** Detailed specification documented in [`ARCHITECTURE.md`](file:///Users/mac/Desktop/ai-stack8s/stack8s-ai-diagnose/ARCHITECTURE.md).

---

## Feature 10 — Node & Resource Evidence Collector

> **Node capacity and allocatable are configuration/state information reported by Kubernetes. They are not equivalent to real-time resource utilization.**

### Purpose & Scope

Feature 10 extends the provider abstraction and unified evidence package to collect real Kubernetes Node capacity, allocatable resources, system metadata, and container resource requests/limits.

### Resource Semantics & Definitions

| Resource Term | Source | Definition |
|---------------|--------|------------|
| **`Capacity`** | Node Status | Total physical/virtual resource reported by the Node hardware. |
| **`Allocatable`** | Node Status | Resource amount Kubernetes makes available for workload scheduling after reserving system memory/CPU. |
| **`Request`** | Pod Workload Spec | Minimum resource guaranteed/reserved for a container by the Kubernetes scheduler. |
| **`Limit`** | Pod Workload Spec | Maximum resource cap enforced for a container. |

### Domain Models & Provider Interface

Added to `provider.InfrastructureProvider`:
- `GetNode(ctx, name) (NodeInfo, error)`

Domain Types:
- `NodeInfo`: `Name`, `Status` (`Ready`/`NotReady`/`Unknown`), `CreatedAt`, `Resources`, `SystemInfo`.
- `NodeResources`: `CPUCapacity`, `CPUAllocatable`, `MemoryCapacity`, `MemoryAllocatable`, `StorageCapacity`, `StorageAllocatable`.
- `NodeSystemInfo`: `KubeletVersion`, `OperatingSystem`, `Architecture`, `KernelVersion`, `ContainerRuntime`.
- `ContainerInfo`: Includes `CPURequest`, `CPULimit`, `MemoryRequest`, `MemoryLimit`.

### Evidence Package Integration (`node_status`)

Node evidence is attached to the `EvidencePackage` as `node_status` evidence items:

- **Scheduled Pods:** Includes node status, CPU/memory capacity, allocatable resources, OS, kernel, and container runtime version.
- **Unscheduled / Pending Pods:** Sets `node_assigned = false`, `node_name = "<none>"`, and records reason `"pod has no assigned node (unscheduled)"` without inventing a fake node.

### Controlled Resource Tests

1. **Resource Specification Test (`test-resource-spec`):** harmless Pod specifying `requests: {cpu: "100m", memory: "64Mi"}` and `limits: {cpu: "200m", memory: "128Mi"}` verified via `kubectl` and collector.
2. **Resource Scheduling Failure Test (`test-pending-cpu`):** safe Pod requesting `cpu: "500"` (500 CPUs, exceeding node allocatable CPU of 4) verified via `kubectl` to trigger `FailedScheduling` (`0/1 nodes are available: 1 Insufficient cpu.`). Verified `node_assigned = false` in evidence package and cleaned up.

---

## Feature 11 — GPU Evidence Collector

> **Feature 11 reports GPU resource information exposed by the infrastructure. It does not infer real-time GPU utilization.**

### Purpose & Scope

Feature 11 extends the provider abstraction and unified evidence package to collect extended GPU resources (such as `nvidia.com/gpu`, `amd.com/gpu`, `intel.com/gpu`), GPU node capacity/allocatable resources, container GPU requests/limits, and GPU model metadata.

### Environment Handling & Safety

Feature 11 handles all 4 cluster GPU states safely:

1. **No GPU Resource Exposed:** Safely returns `gpu_resource_exposed: false` and `gpu_available: false` without fabricating 0 count values.
2. **GPU Resource Exists with 0 Allocatable:** Reports `gpu_resource_exposed: true`, `gpu_available: false`, `gpu_capacity: "1"`, `gpu_allocatable: "0"`.
3. **GPU API / Provider Error:** Captures error in `gpu_status` evidence without crashing.
4. **GPU Resource Exists with Allocatable > 0:** Captures `gpu_resource_exposed: true`, `gpu_available: true`, `gpu_capacity`, `gpu_allocatable`, `gpu_model`, and `resource_name`.

### Extended Provider Interface & Types

Added to `provider.InfrastructureProvider`:
- `GetNodeGPUResources(ctx, name) (NodeGPUResources, error)`

Domain Types:
- `NodeGPUResources`: `NodeName`, `GPUAvailable`, `GPUResourceExposed`, `ResourceName`, `Model`, `Capacity`, `Allocatable`.
- `ContainerInfo`: Updated with `GPURequest` and `GPULimit`.

### Evidence Package Integration (`gpu_status`)

GPU evidence is attached to the `EvidencePackage` as `gpu_status` evidence items for each pod:

- **Scheduled Pods:** Queries host node for GPU extended resources and includes `gpu_available`, `gpu_resource_exposed`, `resource_name`, `capacity`, `allocatable`, and `model`.
- **Unscheduled / Pending Pods:** Sets `node_assigned = false`, `node_name = "<none>"`, and `gpu_available = false` without inventing a fake node.

### Environment Detection Result

- **Local Cluster (`docker-desktop`):** Evaluated via `kubectl get nodes -o json` and node description. Extended GPU resource (`nvidia.com/gpu`) is **not exposed** by local Docker Desktop cluster.
- **Collector Output:** The application correctly reports `gpu_resource_exposed: false` and `gpu_available: false` for `docker-desktop` without fabricating GPU objects.
- **Synthetic Unit Verification:** Complete unit tests (`TestFakeProviderGPUResources`, `TestBuildEvidencePackage_GPUEvidence`) test GPU-positive nodes, multiple GPUs, container GPU requests (`nvidia.com/gpu: 1`), and pending unscheduled pods using `FakeProvider`.




