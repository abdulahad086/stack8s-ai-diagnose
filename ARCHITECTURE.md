# Architecture & Provider Specification — Feature 9

## Overview

`stack8s-ai-diagnose` is built on a clean provider-based diagnostic architecture. The core diagnostic components (**Evidence Package**, **Deterministic Diagnosis Engine**, and **Dataset Builder**) are decoupled from specific infrastructure APIs.

Infrastructure data queries are abstracted through the `InfrastructureProvider` interface:

- **NOW:** `LocalKubernetesProvider` (connects to local Kubernetes / Docker Desktop via `k8s.io/client-go`).
- **FUTURE:** `Stack8sProvider` (will connect to actual Stack8s infrastructure backend).

---

## Conceptual Architecture

```
                       ┌──────────────────────────────┐
                       │  Deterministic Diagnosis     │
                       │          Engine              │
                       └──────────────┬───────────────┘
                                      │
                                      ▼
                       ┌──────────────────────────────┐
                       │   Unified Evidence Package   │
                       └──────────────┬───────────────┘
                                      │
                                      ▼
                       ┌──────────────────────────────┐
                       │    InfrastructureProvider    │
                       │         (Interface)          │
                       └───────┬──────────────┬───────┘
                               │              │
              ┌────────────────┘              └────────────────┐
              ▼                                                ▼
┌───────────────────────────┐                    ┌───────────────────────────┐
│  LocalKubernetesProvider  │                    │      Stack8sProvider      │
│           (NOW)           │                    │         (FUTURE)          │
│ Encapsulates client-go    │                    │ Plugs into Stack8s API    │
└───────────────────────────┘                    └───────────────────────────┘
```

---

## Key Boundaries & Layer Responsibilities

### 1. InfrastructureProvider Layer (`provider/`)
- **Interface:** `provider.InfrastructureProvider` defines methods:
  - `GetNodes(ctx)`
  - `GetNode(ctx, name)`
  - `GetNodeGPUResources(ctx, name)`
  - `GetPods(ctx, namespace)`
  - `GetServices(ctx, namespace)`
  - `GetService(ctx, namespace, name)`
  - `GetPersistentVolumeClaims(ctx, namespace)`
  - `GetPersistentVolumeClaim(ctx, namespace, name)`
  - `GetPersistentVolume(ctx, name)`
  - `GetStorageClasses(ctx)`
  - `GetStorageClass(ctx, name)`
  - `GetPodEvents(ctx, namespace, podName)`
  - `GetContainerLogs(ctx, namespace, podName, containerName, tailLines)`
- **Domain Types:** `NodeInfo`, `NodeResources`, `NodeGPUResources`, `NodeSystemInfo`, `PodInfo`, `PodVolumeInfo`, `PersistentVolumeClaimInfo`, `PersistentVolumeInfo`, `StorageClassInfo`, `ContainerInfo`, `ServiceInfo`, `ServicePortInfo`, `EndpointSubsetInfo`, `EndpointAddressInfo`, `EventInfo`, `ContainerLogInfo`.
- **Client Isolation:** `k8s.io/client-go`, `k8s.io/api`, and `k8s.io/apimachinery` structs are strictly contained inside `provider/local_k8s.go`. They are never passed to higher-level diagnostic layers.

### 2. Evidence Collection Layer (`buildEvidencePackage`)
- Depends **only** on `InfrastructureProvider` and domain types.
- Converts provider domain objects into normalized `EvidencePackage` JSON structures with sequential IDs (`E1`, `E2`, …).
- **Node & Resource Evidence (Feature 10):** Includes `node_status` evidence items with explicit `NodeAssigned` flags (`true` for scheduled pods with node capacity/allocatable, `false` for unscheduled pending pods).
- **GPU Evidence (Feature 11):** Includes `gpu_status` evidence items representing extended GPU resources (e.g. `nvidia.com/gpu`), node capacity/allocatable, container requests/limits, and node model metadata. Handles GPU-unexposed nodes cleanly with `gpu_resource_exposed: false` without fabricating values.
- **Service & Network Evidence (Feature 12):** Includes `service_status` evidence items matching pod labels against Kubernetes Service selectors. Captures Service type, ClusterIP, external IPs, exposed ports, target ports, backing endpoints, ready address count, and whether the pod IP/name is in ready endpoints.
- **Storage Evidence (Feature 13):** Includes `storage_status` evidence items tracing the Pod → PVC → PV → StorageClass chain. Represents PVC phase (`Bound`, `Pending`, `NotFound`), requested capacity, access modes, volume mode, bound PV capacity/source, and StorageClass binding mode. Honest representation: pending PVCs return 0 PVs (`[]`) rather than inventing fake PVs; missing PVCs report `NotFound` with `unavailable: true`.
- Does not know whether data originated from local Kubernetes or Stack8s.

### 3. Deterministic Diagnosis Engine (`diagnoseEvidence`)
- Depends **only** on `EvidencePackage`.
- Evaluates explicit, non-AI rules against `EvidenceItem` contents.
- Returns `PodDiagnosis` containing status (`CONFIRMED` or `UNKNOWN`), `DiagnosisCode`, and supporting `evidence_ids`.
- Completely provider-agnostic.

### 4. Dataset Builder (`generateDatasets` / `validateDatasets`)
- Depends **only** on verified diagnostic case specifications and dataset format rules.
- Generates and validates Needle 3 compatible JSONL datasets (`train.jsonl`, `validation.jsonl`, `test.jsonl`, `tools.json`).
- Completely provider-agnostic.

---

## Resource, GPU, Network & Storage Semantics (Features 10–13)

> **Node capacity and allocatable are configuration/state information reported by Kubernetes. They are not equivalent to real-time resource utilization.**
>
> **Feature 11 reports GPU resource information exposed by the infrastructure. It does not infer real-time GPU utilization.**
>
> **Feature 12 collects observable Kubernetes Service and Endpoint state. It does not perform end-to-end network connectivity or latency probing.**
>
> **"Feature 13 reports Kubernetes storage configuration and state. It does not by itself prove end-to-end storage health or successful application-level I/O."**

- **Capacity:** Total physical/virtual resource reported by the Node hardware (e.g. 4 CPUs, 8 GPUs).
- **Allocatable:** Resource amount Kubernetes makes available for workload scheduling after reserving system memory/CPU.
- **Request:** Minimum resource guaranteed/reserved for a container by Kubernetes scheduler (e.g. `nvidia.com/gpu: 1`).
- **Limit:** Maximum resource cap enforced for a container.
- **GPU Availability vs Utilization:** `gpu_available: true` indicates allocatable GPU resource > 0 on the node. It does NOT represent real-time GPU telemetry.
- **Service Selector Matching:** Evaluates if `service.spec.selector` is a non-empty subset of `pod.metadata.labels`. If matched, endpoint lists are checked for `pod_in_endpoints` and `pod_endpoint_ready`. If no selector matches, reports `service_found: false`.
- **Storage Chain & Quantities:** Traces Pod volume PVC reference → PVC phase & requested capacity → Bound PV & capacity → StorageClass & provisioner. Storage quantity strings (e.g. `"10Mi"`, `"1Gi"`) are preserved in exact Kubernetes string representation. Pending PVCs result in empty PV arrays; missing PVCs are flagged `NotFound`. Pods without PVCs report `pvc_found: false`.

---

## Future Stack8s Integration Plan

When access to the actual Stack8s backend/API is available:

1. Create implementation of `InfrastructureProvider` in `provider/stack8s_provider.go`:
   ```go
   type Stack8sProvider struct {
       client *stack8s.Client
   }
   ```
2. Map Stack8s API responses to standard domain types (`NodeInfo`, `NodeGPUResources`, `PodInfo`, `ServiceInfo`, `PersistentVolumeClaimInfo`, `PersistentVolumeInfo`, `StorageClassInfo`, `EventInfo`, `ContainerLogInfo`).
3. Set environment variable `STACK8S_PROVIDER=stack8s`.
4. **Zero changes** will be required in `buildEvidencePackage`, `diagnoseEvidence`, or dataset generation.


