# Architecture & Provider Specification — Feature 15

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
  - `GetDeployment(ctx, namespace, name)`
  - `GetReplicaSet(ctx, namespace, name)`
  - `GetStatefulSet(ctx, namespace, name)`
  - `GetDaemonSet(ctx, namespace, name)`
  - `GetJob(ctx, namespace, name)`
  - `GetCronJob(ctx, namespace, name)`
  - `GetPodEvents(ctx, namespace, podName)`
  - `GetContainerLogs(ctx, namespace, podName, containerName, tailLines)`
  - `GetConfigMap(ctx, namespace, name)`
  - `GetSecretMetadata(ctx, namespace, name)`
  - `GetServiceAccount(ctx, namespace, name)`
- **Domain Types:** `NodeInfo`, `NodeResources`, `NodeGPUResources`, `NodeSystemInfo`, `PodInfo`, `PodVolumeInfo`, `PersistentVolumeClaimInfo`, `PersistentVolumeInfo`, `StorageClassInfo`, `DeploymentInfo`, `ReplicaSetInfo`, `StatefulSetInfo`, `DaemonSetInfo`, `JobInfo`, `CronJobInfo`, `OwnerReferenceInfo`, `WorkloadCondition`, `ContainerInfo`, `ServiceInfo`, `ServicePortInfo`, `EndpointSubsetInfo`, `EndpointAddressInfo`, `EventInfo`, `ContainerLogInfo`, `ConfigMapInfo`, `SecretInfo`, `ServiceAccountInfo`, `EnvVarRef`.
- **Client Isolation:** `k8s.io/client-go`, `k8s.io/api`, and `k8s.io/apimachinery` structs are strictly contained inside `provider/local_k8s.go`. They are never passed to higher-level diagnostic layers.

### 2. Evidence Collection Layer (`buildEvidencePackage`)
- Depends **only** on `InfrastructureProvider` and domain types.
- Converts provider domain objects into normalized `EvidencePackage` JSON structures with sequential IDs (`E1`, `E2`, …).
- **Node & Resource Evidence (Feature 10):** Includes `node_status` evidence items with explicit `NodeAssigned` flags (`true` for scheduled pods with node capacity/allocatable, `false` for unscheduled pending pods).
- **GPU Evidence (Feature 11):** Includes `gpu_status` evidence items representing extended GPU resources (e.g. `nvidia.com/gpu`), node capacity/allocatable, container requests/limits, and node model metadata. Handles GPU-unexposed nodes cleanly with `gpu_resource_exposed: false` without fabricating values.
- **Service & Network Evidence (Feature 12):** Includes `service_status` evidence items matching pod labels against Kubernetes Service selectors. Captures Service type, ClusterIP, external IPs, exposed ports, target ports, backing endpoints, ready address count, and whether the pod IP/name is in ready endpoints.
- **Storage Evidence (Feature 13):** Includes `storage_status` evidence items tracing the Pod → PVC → PV → StorageClass chain. Represents PVC phase (`Bound`, `Pending`, `NotFound`), requested capacity, access modes, volume mode, bound PV capacity/source, and StorageClass binding mode. Honest representation: pending PVCs return 0 PVs (`[]`) rather than inventing fake PVs; missing PVCs report `NotFound` with `unavailable: true`.
- **Workload & Controller Evidence (Feature 14):** Includes `workload_status` evidence items tracing controller ownership chains (`Pod` → `ReplicaSet` → `Deployment`, `Pod` → `StatefulSet`, `Pod` → `DaemonSet`, `Pod` → `Job` → `CronJob`, Standalone Pod). Captures desired/current/ready/available replica counts, rollout strategies, generations, revisions, completion states, and controller conditions.
- **Configuration & Identity Evidence (Feature 15):** Includes `configuration_status` evidence items observing referenced ConfigMaps, Secrets (metadata only), and ServiceAccounts. Evaluates existence, key presence for volume mounts and env var references, ServiceAccount existence, auto-mount flags, and image pull secret existence. Strictly respects security boundaries by NEVER reading, transmitting, or recording raw Secret values or payload contents.
- Does not know whether data originated from local Kubernetes or Stack8s.

### 3. Deterministic Diagnosis Engine (`diagnoseEvidence`)
- Depends **only** on `EvidencePackage`.
- Evaluates explicit, non-AI rules against `EvidenceItem` contents.
- Returns `PodDiagnosis` containing status (`CONFIRMED` or `UNKNOWN`), `DiagnosisCode`, and supporting `evidence_ids`.
- Completely provider-agnostic.

### 4. Dataset Builder (`generateDatasets` / `validateDatasets`) - Feature 18
- Depends **only** on verified diagnostic case specifications and dataset format rules.
- Generates and validates Needle 3 compatible JSONL datasets (`train.jsonl`, `validation.jsonl`, `test.jsonl`, `tools.json`).
- Includes 9 comprehensive diagnostic tools (`get_pod_status`, `get_pod_events`, `get_container_logs`, `get_node_status`, `get_gpu_status`, `get_service_status`, `get_storage_status`, `get_workload_status`, `get_configuration_status`).
- Creates 380+ examples across 29 Kubernetes failure/success categories.
- Ensures data leakage prevention with disjoint pod names/namespaces across train, validation, and test splits.
- Enforces strict secret redaction (no plaintext credentials or payloads in datasets).
- Captures real controlled Kubernetes evidence snapshots and stores them in `dataset/evidence/`.
- Completely provider-agnostic.

---

## Resource, GPU, Network, Storage, Workload & Config Semantics (Features 10–15)

> **Node capacity and allocatable are configuration/state information reported by Kubernetes. They are not equivalent to real-time resource utilization.**
>
> **Feature 11 reports GPU resource information exposed by the infrastructure. It does not infer real-time GPU utilization.**
>
> **Feature 12 collects observable Kubernetes Service and Endpoint state. It does not perform end-to-end network connectivity or latency probing.**
>
> **Feature 13 reports Kubernetes storage configuration and state. It does not by itself prove end-to-end storage health or successful application-level I/O.**
>
> **Feature 14 reports Kubernetes workload/controller configuration and state. It does not by itself determine whether a workload is healthy or unhealthy.**
>
> **Feature 15 observes Kubernetes configuration references and identity metadata. It NEVER inspects secret payloads, raw secret data, or application settings.**

- **Capacity:** Total physical/virtual resource reported by the Node hardware (e.g. 4 CPUs, 8 GPUs).
- **Allocatable:** Resource amount Kubernetes makes available for workload scheduling after reserving system memory/CPU.
- **Request:** Minimum resource guaranteed/reserved for a container by Kubernetes scheduler (e.g. `nvidia.com/gpu: 1`).
- **Limit:** Maximum resource cap enforced for a container.
- **GPU Availability vs Utilization:** `gpu_available: true` indicates allocatable GPU resource > 0 on the node. It does NOT represent real-time GPU telemetry.
- **Service Selector Matching:** Evaluates if `service.spec.selector` is a non-empty subset of `pod.metadata.labels`. If matched, endpoint lists are checked for `pod_in_endpoints` and `pod_endpoint_ready`. If no selector matches, reports `service_found: false`.
- **Storage Chain & Quantities:** Traces Pod volume PVC reference → PVC phase & requested capacity → Bound PV & capacity → StorageClass & provisioner. Storage quantity strings (e.g. `"10Mi"`, `"1Gi"`) are preserved in exact Kubernetes string representation. Pending PVCs result in empty PV arrays; missing PVCs are flagged `NotFound`. Pods without PVCs report `pvc_found: false`.
- **Workload Ownership & Replica Metrics:** Uses `ownerReferences` to resolve direct owner and root controller. Captures desired vs actual ready/available/updated replica counts, rollout strategies (`RollingUpdate`, `Recreate`), observed generations, and conditions without converting conditions into premature diagnosis errors. Standalone pods report `controller_found: false`.
- **Configuration & Secret Metadata Boundary:** Checks existence of referenced ConfigMaps, Secrets, ServiceAccounts, and ImagePullSecrets. For Secrets, only metadata (existence, type, data key names) is collected. Raw values, stringData, or binary data fields are strictly excluded. Missing resources flag `found: false` and `unavailable: true` without crashing.

---

## Future Stack8s Integration Plan

When access to the actual Stack8s backend/API is available:

1. Create implementation of `InfrastructureProvider` in `provider/stack8s_provider.go`:
   ```go
   type Stack8sProvider struct {
       client *stack8s.Client
   }
   ```
2. Map Stack8s API responses to standard domain types (`NodeInfo`, `NodeGPUResources`, `PodInfo`, `ServiceInfo`, `PersistentVolumeClaimInfo`, `PersistentVolumeInfo`, `StorageClassInfo`, `DeploymentInfo`, `ReplicaSetInfo`, `StatefulSetInfo`, `DaemonSetInfo`, `JobInfo`, `CronJobInfo`, `EventInfo`, `ContainerLogInfo`, `ConfigMapInfo`, `SecretInfo`, `ServiceAccountInfo`).
3. Set environment variable `STACK8S_PROVIDER=stack8s`.
4. **Zero changes** will be required in `buildEvidencePackage`, `diagnoseEvidence`, or dataset generation.

