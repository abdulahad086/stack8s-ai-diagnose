# Dataset Argument Grounding Audit Report (Task 2)

## Executive Summary

This audit evaluates every expected tool call across all examples in Dataset V2 and Dataset V3 (`train.jsonl`, `validation.jsonl`, `test.jsonl`, and `manifest.jsonl`).

The mandatory rule for Dataset V3 argument grounding is:
> **Every expected tool call argument MUST be explicitly supported by either (A) the user query text OR (B) explicitly available initial system context.**

When an expected tool call contains arguments (such as a container name or node name) that are not present in the user query or context, it is flagged as `dataset_argument_not_grounded` (an unsupported expected argument).

---

## Audit Statistics (Dataset V2 vs Dataset V3)

| Metric | Dataset V2 | Dataset V3 (Feature 20) | Change / Status |
| :--- | :--- | :--- | :--- |
| **Total Examples Audited** | 380 | **548** | +168 examples |
| **Total Expected Tool Calls** | 942 | **1,063** | Audited |
| **Total Tool Arguments** | 1,843 | **2,066** | Audited |
| **Grounded Arguments** | 1,791 (97.2%) | **2,066 (100.0%)** | **100% Grounded** ✅ |
| **Ungrounded Arguments** | 52 (2.8%) | **0 (0.0%)** | **0 Ungrounded** ✅ |
| **Affected Cases (≥1 Ungrounded Arg)** | 52 (13.7%) | **0 (0.0%)** | **Resolved** ✅ |

---

## Historical Breakdown of Ungrounded Arguments in Dataset V2

During Task 2 audit of Dataset V2, 52 ungrounded argument occurrences were identified:

| Tool Name | Ungrounded Arg | Example Value | Count | Root Cause / Reason |
| :--- | :--- | :--- | :--- | :--- |
| `get_node_status` | `node_name` | `"docker-desktop"` | 42 | Query asked about unschedulable pod or node status without specifying `"docker-desktop"`. |
| `get_gpu_status` | `node_name` | `"docker-desktop"` | 8 | Query asked about GPU capacity without specifying node name in text. |
| `get_container_logs` | `container` | `"app"` | 2 | Query asked why pod `test-image-failure` is failing without specifying container `"app"`. |

---

## Resolution Strategy Implemented for Dataset V3

1. **Explicit Query Parameter Grounding:**
   - Where `get_container_logs` is expected, the query explicitly specifies the container name (e.g. `"Get logs for container app in pod test-image-failure in default namespace"`).
   - Where `get_node_status` or `get_gpu_status` is expected, the query explicitly names the target node (e.g. `"Check node status for cluster node docker-desktop hosting pod ctrl-sched-cpu-001"`).

2. **Sequential Disambiguation:**
   - Where a user query only asks generally why a pod is failing, the expected tool calls are restricted to `get_pod_status` and `get_pod_events`.

3. **Validation Enforcement:**
   - `dataset_validator.py` enforces 100% substring matching for all expected arguments against the query text across all splits (`train`, `validation`, `test`).

---

## Affected Cases & Status Summary

- Total Dataset V3 Examples: **548**
- Grounded Arguments: **2,066 / 2,066 (100.0%)**
- Ungrounded Arguments: **0 / 2,066 (0.0%)**
- Unsupported Expected Arguments: **0**
- Affected Cases: **0**
