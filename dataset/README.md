# Diagnostic Dataset for Needle 3 Fine-Tuning — Feature 8

> **Feature 8 prepares the dataset. Needle 3 fine-tuning is a later feature.**

## Purpose

This dataset converts verified Kubernetes diagnostic scenarios into structured JSONL training, validation, and testing datasets compatible with the local Needle 3 fine-tuning format.

Training data and runtime evidence are kept strictly separate:
- **Training Data:** Teaches diagnostic behavior, tool invocation workflows, grounded argument extraction, selective tool usage, and when to declare `UNKNOWN`.
- **Runtime Evidence:** Collected at runtime from real customer clusters by Features 1–7.

---

## Official Needle 3 JSONL Format

Each example line in `train.jsonl`, `validation.jsonl`, and `test.jsonl` is a single valid JSON object adhering to the Needle schema:

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

---

## Tool Definitions (`dataset/tools.json`)

Needle diagnostic tools map directly to the implemented collectors:

| Tool Name | Purpose | Required Arguments |
|-----------|---------|-------------------|
| `get_pod_status` | Retrieve current status, phase, container states, ready conditions, and restart counts. | `namespace`, `pod` |
| `get_pod_events` | Retrieve chronological Kubernetes Events associated with a Pod. | `namespace`, `pod` |
| `get_container_logs` | Retrieve recent log output lines from a container within a Pod. | `namespace`, `pod`, `container` |

---

## Grounding Rules & Secret Redaction

1. **Strict Argument Grounding:** Arguments in `answers` must strictly contain values present in the query context (e.g. `namespace: "default"`, `pod: "test-image-failure"`). Placeholders like `<pod_name>` or `TODO` are strictly prohibited.
2. **Secret Redaction:** No passwords, tokens, API keys, `bearer` tokens, or certificates are present in any dataset record. Any credential patterns are automatically checked and redacted using `[REDACTED]`.

---

## Dataset Split & Data Leakage Prevention

| Split | File | Percentage | Description |
|-------|------|------------|-------------|
| **Train** | `dataset/train.jsonl` | 63.0% (17 records) | Verified real cases & synthetic grounded diagnostic cases. |
| **Validation** | `dataset/validation.jsonl` | 18.5% (5 records) | Intermediate evaluation cases for model hyperparameter tuning. |
| **Holdout Test** | `dataset/test.jsonl` | 18.5% (5 records) | Holdout test set with unseen pod names, unseen namespaces, and novel phrasings. |

**Leakage Prevention:**
The split is performed strictly by **incident / pod context** rather than random line shuffling. Pod names and namespaces in `test.jsonl` (e.g. `invoice-generator`, `production-eu`, `search-indexer-pod`, `billing-v2`) are completely disjoint from `train.jsonl`.

---

## UNKNOWN & Negative Examples

- **Negative Examples (`answers: []`):** Non-diagnostic requests (e.g., general conceptual questions or greetings) instruct the model not to over-trigger diagnostic tools.
- **UNKNOWN Cases:** Diagnostic inquiries where collected evidence is insufficient or symptom-only (e.g., `CrashLoopBackOff` without OOM / image pull root cause) map to ground truth diagnosis code `UNKNOWN`.

---

## Dataset Statistics

- **Total Examples:** 27
- **Train Examples:** 17
- **Validation Examples:** 5
- **Test Examples:** 5
- **Real Verified Cases:** 4
- **Synthetic Grounded Cases:** 23
- **Negative / Empty Answer Cases (`[]`):** 5
- **UNKNOWN Cases:** 6
- **Dataset Schema Validation:** PASSED ✅
- **Data Leakage Check:** PASSED ✅

---

## Validation Tool

Dataset validation is integrated directly into the Go build and test suite (`dataset_test.go` and `dataset_builder.go`):

```bash
# Run automated dataset validation and tests
go test -v ./...

# Build binary and print dataset statistics
go build -o k8s-check .
./k8s-check
```

---

## Known Limitations

- Fine-tuning Needle 3 is deferred to a later feature; Feature 8 only prepares and validates the dataset.
- Tool schemas currently cover only implemented collectors (`get_pod_status`, `get_pod_events`, `get_container_logs`).
