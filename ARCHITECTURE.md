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
  - `GetPods(ctx, namespace)`
  - `GetPodEvents(ctx, namespace, podName)`
  - `GetContainerLogs(ctx, namespace, podName, containerName, tailLines)`
- **Domain Types:** `NodeInfo`, `NodeResources`, `NodeSystemInfo`, `PodInfo`, `ContainerInfo`, `EventInfo`, `ContainerLogInfo`.
- **Client Isolation:** `k8s.io/client-go`, `k8s.io/api`, and `k8s.io/apimachinery` structs are strictly contained inside `provider/local_k8s.go`. They are never passed to higher-level diagnostic layers.

### 2. Evidence Collection Layer (`buildEvidencePackage`)
- Depends **only** on `InfrastructureProvider` and domain types.
- Converts provider domain objects into normalized `EvidencePackage` JSON structures with sequential IDs (`E1`, `E2`, …).
- **Node & Resource Evidence (Feature 10):** Includes `node_status` evidence items with explicit `NodeAssigned` flags (`true` for scheduled pods with node capacity/allocatable, `false` for unscheduled pending pods).
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

## Resource Semantics (Feature 10)

> **Node capacity and allocatable are configuration/state information reported by Kubernetes. They are not equivalent to real-time resource utilization.**

- **Capacity:** Total physical/virtual resource reported by the Node hardware.
- **Allocatable:** Resource amount Kubernetes makes available for workload scheduling after reserving system memory/CPU.
- **Request:** Minimum resource guaranteed/reserved for a container by Kubernetes scheduler.
- **Limit:** Maximum resource cap enforced for a container.

---

## Future Stack8s Integration Plan

When access to the actual Stack8s backend/API is available:

1. Create implementation of `InfrastructureProvider` in `provider/stack8s_provider.go`:
   ```go
   type Stack8sProvider struct {
       client *stack8s.Client
   }
   ```
2. Map Stack8s API responses to standard domain types (`NodeInfo`, `PodInfo`, `EventInfo`, `ContainerLogInfo`).
3. Set environment variable `STACK8S_PROVIDER=stack8s`.
4. **Zero changes** will be required in `buildEvidencePackage`, `diagnoseEvidence`, or dataset generation.

