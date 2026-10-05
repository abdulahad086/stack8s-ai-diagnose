# Dataset V3 Ground-Truth Audit + Real Kubernetes Expansion Report (Feature 20)

## Executive Summary

Feature 20 delivers **Dataset V3**: a fully audited, leak-free, 100% grounded diagnostic benchmark with substantially expanded real controlled Kubernetes incidents. 

Following Feature 19's initial Needle 3 evaluation (BASE holdout exact match 63.5% vs TUNED 74.6%), Feature 20 addresses critical dataset label weaknesses, ungrounded tool parameters, and dataset split leakage **BEFORE** any future model training.

> **CRITICAL DIRECTIVE ENFORCED:** Zero Needle training runs were performed in Feature 20. All existing Feature 19 model weights and artifacts remain strictly preserved.

---

## 1. Primary Dataset V3 Statistics & Split Breakdown

| Dataset Metric | Count | Percentage | Description / Target |
| :--- | :--- | :--- | :--- |
| **Total Dataset V3 Examples** | **548** | **100.0%** | Target Range: 500–700 examples |
| **Train Split (`train.jsonl`)** | **383** | **69.9%** | Disjoint training set |
| **Validation Split (`validation.jsonl`)** | **97** | **17.7%** | Disjoint validation set |
| **Test Split (`test.jsonl`)** | **68** | **12.4%** | Disjoint holdout test set |

---

## 2. Provenance & Source Type Breakdown

Every example in `manifest.jsonl` contains full provenance metadata (`case_id`, `source_type`, `scenario_category`, `description`, `namespace`, `target_pod`, `user_query`, `expected_tool_calls`, `evidence_snapshot`, `ground_truth_code`, `ground_truth_status`, `verification_commands`).

| Source Type | Count | Target / Rule | Verification Status |
| :--- | :--- | :--- | :--- |
| **`controlled_kubernetes`** | **55** | ≥ 50 Controlled Examples | Verified against `dataset/evidence/*.json` snapshots |
| **`synthetic_unit_test`** | **406** | Balanced coverage | Verified schema compliance |
| **`documentation_derived`** | **87** | Real doc scenarios | Verified schema compliance |

---

## 3. Controlled Kubernetes Category Distribution (Task 5 & 16)

The 55 real controlled Kubernetes incidents are distributed across all core failure domains:

| Category | Controlled Count | Target Coverage | Evidence Snapshot Example |
| :--- | :--- | :--- | :--- |
| **`IMAGE`** | 5 | Multiple invalid images, namespaces | `dataset/evidence/ctrl-img-001.json` |
| **`OOM`** | 4 | Memory limits, exit code 137 | `dataset/evidence/ctrl-oom-001.json` |
| **`CRASH`** | 4 | Repeated container crashes, logs | `dataset/evidence/ctrl-crash-001.json` |
| **`SCHEDULING`** | 4 | Insufficient CPU, nodeSelector mismatch | `dataset/evidence/ctrl-sched-cpu-001.json` |
| **`SERVICE`** | 5 | Zero endpoints, selector mismatch, healthy | `dataset/evidence/ctrl-svc-zero-001.json` |
| **`STORAGE`** | 4 | Pending PVC, missing PVC, bound PVC | `dataset/evidence/ctrl-storage-pending-001.json` |
| **`CONFIGURATION`** | 4 | Missing ConfigMap, missing key, missing secret | `dataset/evidence/ctrl-config-cm-001.json` |
| **`WORKLOAD`** | 6 | Replica mismatch, Job failure, Job ok, StatefulSet | `dataset/evidence/ctrl-workload-dep-001.json` |
| **`NODE`** | 2 | Allocatable resources, conditions | `dataset/evidence/ctrl-node-cond-001.json` |
| **`HEALTHY`** | 7 | Healthy Pod, Service, PVC, Deployment | `dataset/evidence/ctrl-healthy-pod-001.json` |
| **`UNKNOWN`** | 4 | Ambiguous state, incomplete evidence | `dataset/evidence/ctrl-unknown-ambig-001.json` |
| **`CONFUSION_PAIRS`** | 6 | ImagePull vs CrashLoop, CPU vs Storage | `dataset/evidence/ctrl-confusion-img-crash-001.json` |
| **Total Controlled** | **55** | **Target ≥ 50 Achieved** | **100% Verified** |

---

## 4. Special Category Counts (Healthy, UNKNOWN, No-Call, Multi-Tool)

| Special Category | Count | Percentage |
| :--- | :--- | :--- |
| **Healthy Cases (`HEALTHY_POD`, `SERVICE_HEALTHY`, etc.)** | **96** | 17.5% |
| **UNKNOWN Diagnostic Cases (`ground_truth_status = UNKNOWN`)** | **38** | 6.9% |
| **No-Call Cases (`answers = []` for off-topic/general questions)** | **17** | 3.1% |
| **Multi-Tool Cases (`len(answers) ≥ 2`)** | **363** | 66.2% |

---

## 5. Argument Grounding Audit Summary (Task 2)

- **Total Arguments Audited:** 2,066
- **Grounded Arguments:** 2,066 (100.0%)
- **Ungrounded Arguments:** 0 (0.0%)
- **Affected Cases with Ungrounded Arguments:** 0 (0.0%)

*Resolution:* All parameters in expected tool calls (e.g. `pod`, `namespace`, `container`, `node_name`, `service_name`, `pvc_name`, `workload_name`) are strictly present in the text of the user query.

---

## 6. Multi-Tool Audit Summary (Task 3)

- **Total Multi-Tool Examples Audited:** 363
- **Justified One-Shot Multi-Tool Calls:** 363 (100.0%)
- **Ungrounded / Sequential Multi-Tool Calls:** 0 (0.0%)

*Resolution:* Multi-tool calls are restricted to combinations where every tool's parameters are explicitly declared in the user query text (e.g. `get_pod_status` + `get_pod_events`).

---

## 7. Tool Schema Audit & Proposed Improvements (Task 4)

A review of all 9 diagnostic tools in `dataset/tools.json`:

1. `get_pod_status` (required: `namespace`, `pod`): Fully grounded for single pod queries.
2. `get_pod_events` (required: `namespace`, `pod`): Fully grounded for pod lifecycle event inspection.
3. `get_container_logs` (required: `namespace`, `pod`, `container`): Requires container name. *Proposed API Improvement:* Make `container` optional so single-container pods default to the primary app container.
4. `get_node_status` (required: `node_name`): Requires explicit node name in query text. *Proposed API Improvement:* Allow `pod` + `namespace` lookup to resolve target node automatically.
5. `get_gpu_status` (required: `node_name`): Requires explicit GPU node name.
6. `get_service_status` (required: `namespace`; optional: `pod`, `service_name`): Grounded.
7. `get_storage_status` (required: `namespace`; optional: `pod`, `pvc_name`): Grounded.
8. `get_workload_status` (required: `namespace`; optional: `pod`, `workload_kind`, `workload_name`): Grounded.
9. `get_configuration_status` (required: `namespace`, `pod`): Grounded.

---

## 8. Real GPU Compliance (Task 6)

- Local Docker Desktop does not expose physical GPU hardware.
- Per Task 6 directives, **0** GPU cases are claimed as `controlled_kubernetes`.
- All 17 GPU test cases are marked `source_type = "synthetic_unit_test"`.

---

## 9. Benchmark Evaluator Audit (Task 15)

`benchmark.py` and `benchmark_inproc.py` have been upgraded to distinguish 9 granular error categories:
1. `model_error`
2. `dataset_label_error`
3. `unsupported_expected_argument`
4. `wrong_tool`
5. `missing_tool`
6. `extra_tool`
7. `wrong_argument`
8. `missing_argument`
9. `invented_argument`

---

## 10. Quality Validation Results (Task 14)

Executing `python3 dataset_validator.py` confirms all 11 validation checks pass:

- ✅ **Manifest Validity:** 548 records parsed cleanly.
- ✅ **JSONL Format:** `train.jsonl`, `validation.jsonl`, `test.jsonl` syntax valid.
- ✅ **Tool Schema Compliance:** 100% compliance across all 9 tools.
- ✅ **Argument Grounding:** 100% of 2,066 arguments strictly grounded in query text.
- ✅ **Evidence Provenance:** 100% of controlled cases reference valid evidence files.
- ✅ **Duplicate Queries:** 0 duplicate queries across entire dataset.
- ✅ **Split Leakage:** 0 overlap between Train, Validation, and Test pods/namespaces.
- ✅ **Secret Scan:** 0 bearer tokens, certificates, or private keys detected.
- ✅ **Credential Scan:** 0 hardcoded passwords or secrets detected.
- ✅ **Placeholder Scan:** 0 `<pod>`, `TODO`, `placeholder`, or `[REDACTED]` tokens.
- ✅ **Source Type Correctness:** Task 6 GPU rule verified.

---

## 11. No Training Confirmation (Task 17)

- `needle finetune` command was **NOT** invoked.
- Model weights in `checkpoints/` remain unmodified.
- Feature 19 baseline & tuned metrics remain preserved.

Dataset V3 is ready for future Needle training experiments.
