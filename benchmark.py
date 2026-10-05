#!/usr/bin/env python3
"""
Feature 19 — Needle 3 Benchmark Script
Evaluates base and fine-tuned Needle 3 models on tool selection and argument grounding.

Usage:
  python benchmark.py --checkpoint ./checkpoints/needle3.safetensors \
                      --split dataset/validation.jsonl \
                      --max-examples 20 \
                      --out models/needle3/baseline/metrics.json
"""

import argparse
import json
import os
import re
import subprocess
import sys
import time
from typing import Any

# ─── Output Format Parser ────────────────────────────────────────────────────

def parse_needle_output(raw: str) -> list:
    """
    Parse raw Needle 3 CLI output into a list of tool call dicts.
    
    Confirmed format from smoke test:
      <tool_call>[{"name":"get_pod_status","arguments":{"namespace":"X","pod":"Y"}}]</tool_call><|im_end|>
    
    Also handles:
      - Empty answers (no <tool_call> block) → []
      - Multiple calls inside a single array
      - <think>...</think> prefix block
    """
    # Strip think blocks
    raw = re.sub(r'<think>.*?</think>', '', raw, flags=re.DOTALL)
    
    # Find tool_call blocks
    pattern = r'<tool_call>(.*?)</tool_call>'
    matches = re.findall(pattern, raw, re.DOTALL)
    
    calls = []
    for m in matches:
        m = m.strip()
        if not m:
            continue
        try:
            parsed = json.loads(m)
            if isinstance(parsed, list):
                calls.extend(parsed)
            elif isinstance(parsed, dict):
                calls.append(parsed)
        except json.JSONDecodeError:
            calls.append({"_invalid": True, "_raw": m})
    
    return calls


# ─── Metrics ─────────────────────────────────────────────────────────────────

class BenchmarkMetrics:
    def __init__(self):
        self.total = 0
        self.exact_match = 0
        self.tool_selection_correct = 0
        self.no_call_correct = 0
        self.no_call_total = 0
        self.invalid_call = 0
        self.multi_tool_correct = 0
        self.multi_tool_total = 0
        self.arg_total = 0
        self.arg_correct = 0
        self.arg_wrong = 0
        self.arg_missing = 0
        self.arg_invented = 0
        self.arg_extra = 0

        # Task 15 explicit metric breakdown
        self.model_error_count = 0
        self.dataset_label_error_count = 0
        self.unsupported_expected_argument_count = 0
        self.wrong_tool_count = 0
        self.missing_tool_count = 0
        self.extra_tool_count = 0
        self.wrong_argument_count = 0
        self.missing_argument_count = 0
        self.invented_argument_count = 0

        self.category_results = {}
        self.inference_times = []
        self.failures = []
        self.confusion = {}
        self.examples = []

    def add_confusion(self, expected, predicted):
        if expected not in self.confusion:
            self.confusion[expected] = {}
        self.confusion[expected][predicted] = self.confusion[expected].get(predicted, 0) + 1

    def record(self, query, expected_calls, predicted_calls, inference_time, category="OTHER"):
        self.total += 1
        self.inference_times.append(inference_time)

        # Check for ungrounded / unsupported expected arguments in dataset ground truth
        q_lower = query.lower()
        unsupported_label_args = []
        for exp_call in expected_calls:
            for k, v in exp_call.get("arguments", {}).items():
                if str(v).lower().strip() not in q_lower:
                    unsupported_label_args.append({"tool": exp_call["name"], "arg": k, "val": v})
        
        if unsupported_label_args:
            self.dataset_label_error_count += 1
            self.unsupported_expected_argument_count += len(unsupported_label_args)

        has_invalid = any(c.get("_invalid") for c in predicted_calls)
        if has_invalid:
            self.invalid_call += 1

        expected_names = [c["name"] for c in expected_calls]
        predicted_names = [c.get("name", "") for c in predicted_calls if not c.get("_invalid")]

        is_no_call = len(expected_calls) == 0
        if is_no_call:
            self.no_call_total += 1
            if len(predicted_calls) == 0:
                self.no_call_correct += 1

        if len(expected_calls) > 1:
            self.multi_tool_total += 1

        tools_correct = (sorted(expected_names) == sorted(predicted_names))
        if tools_correct:
            self.tool_selection_correct += 1
        else:
            for e in expected_names:
                if e not in predicted_names:
                    self.missing_tool_count += 1
            for p in predicted_names:
                if p not in expected_names:
                    self.wrong_tool_count += 1
                    self.extra_tool_count += 1

        arg_details = []
        for exp_call in expected_calls:
            exp_name = exp_call["name"]
            exp_args = exp_call.get("arguments", {})
            pred_call = next((c for c in predicted_calls if c.get("name") == exp_name and not c.get("_invalid")), None)
            if pred_call is None:
                for k in exp_args:
                    self.arg_missing += 1
                    self.missing_argument_count += 1
                    self.arg_total += 1
                    arg_details.append({"tool": exp_name, "arg": k, "status": "missing", "expected": exp_args[k], "predicted": None})
                self.add_confusion(exp_name, "<missing>")
                continue

            pred_args = pred_call.get("arguments", {})
            for k, v in exp_args.items():
                self.arg_total += 1
                if k not in pred_args:
                    self.arg_missing += 1
                    self.missing_argument_count += 1
                    arg_details.append({"tool": exp_name, "arg": k, "status": "missing", "expected": v, "predicted": None})
                elif str(pred_args[k]) == str(v):
                    self.arg_correct += 1
                    arg_details.append({"tool": exp_name, "arg": k, "status": "correct", "expected": v, "predicted": pred_args[k]})
                else:
                    self.arg_wrong += 1
                    self.wrong_argument_count += 1
                    arg_details.append({"tool": exp_name, "arg": k, "status": "wrong", "expected": v, "predicted": pred_args[k]})

            for k in pred_args:
                if k not in exp_args:
                    self.arg_extra += 1
                    self.arg_invented += 1
                    self.invented_argument_count += 1
                    arg_details.append({"tool": exp_name, "arg": k, "status": "invented", "expected": None, "predicted": pred_args[k]})

            self.add_confusion(exp_name, exp_name)

        args_exact = all(d["status"] == "correct" for d in arg_details) if arg_details else (len(expected_calls) == 0 and len(predicted_calls) == 0)
        is_exact = tools_correct and args_exact
        if is_exact:
            self.exact_match += 1
            if len(expected_calls) > 1:
                self.multi_tool_correct += 1
        else:
            self.model_error_count += 1

        if category not in self.category_results:
            self.category_results[category] = {"total": 0, "exact": 0, "tools_correct": 0}
        self.category_results[category]["total"] += 1
        if is_exact:
            self.category_results[category]["exact"] += 1
        if tools_correct:
            self.category_results[category]["tools_correct"] += 1

        if not is_exact and len(self.failures) < 20:
            error_type = []
            if has_invalid:
                error_type.append("invalid_tool")
            if sorted(expected_names) != sorted(predicted_names):
                for e in expected_names:
                    if e not in predicted_names:
                        error_type.append(f"missing_tool:{e}")
                for p in predicted_names:
                    if p not in expected_names:
                        error_type.append(f"extra_tool:{p}")
            for d in arg_details:
                if d["status"] != "correct":
                    error_type.append(f"{d['status']}_arg:{d['arg']}")
            self.failures.append({
                "query": query[:120],
                "expected": expected_calls,
                "predicted": predicted_calls,
                "error_types": error_type,
                "category": category,
            })

        self.examples.append({
            "query": query[:120],
            "expected_names": expected_names,
            "predicted_names": predicted_names,
            "tools_correct": tools_correct,
            "exact_match": is_exact,
            "arg_details": arg_details,
            "inference_time": inference_time,
            "category": category,
        })

    def report(self):
        def pct(n, d):
            return round(100.0 * n / d, 1) if d > 0 else 0.0

        avg_time = sum(self.inference_times) / len(self.inference_times) if self.inference_times else 0
        total_time = sum(self.inference_times)

        return {
            "total_examples": self.total,
            "exact_match_accuracy": pct(self.exact_match, self.total),
            "tool_selection_accuracy": pct(self.tool_selection_correct, self.total),
            "no_call_accuracy": pct(self.no_call_correct, self.no_call_total) if self.no_call_total else None,
            "multi_tool_exact_match": pct(self.multi_tool_correct, self.multi_tool_total) if self.multi_tool_total else None,
            "invalid_tool_rate": pct(self.invalid_call, self.total),
            "argument_accuracy": pct(self.arg_correct, self.arg_total) if self.arg_total else None,
            "missing_argument_rate": pct(self.arg_missing, self.arg_total) if self.arg_total else None,
            "wrong_argument_rate": pct(self.arg_wrong, self.arg_total) if self.arg_total else None,
            "extra_argument_rate": pct(self.arg_extra, self.arg_total) if self.arg_total else None,
            "invented_argument_count": self.arg_invented,
            "task_15_error_breakdown": {
                "1_model_error": self.model_error_count,
                "2_dataset_label_error": self.dataset_label_error_count,
                "3_unsupported_expected_argument": self.unsupported_expected_argument_count,
                "4_wrong_tool": self.wrong_tool_count,
                "5_missing_tool": self.missing_tool_count,
                "6_extra_tool": self.extra_tool_count,
                "7_wrong_argument": self.wrong_argument_count,
                "8_missing_argument": self.missing_argument_count,
                "9_invented_argument": self.invented_argument_count,
            },
            "avg_inference_time_s": round(avg_time, 2),
            "total_inference_time_s": round(total_time, 2),
            "per_category": {
                cat: {
                    "total": v["total"],
                    "exact_match_pct": pct(v["exact"], v["total"]),
                    "tool_selection_pct": pct(v["tools_correct"], v["total"]),
                }
                for cat, v in self.category_results.items()
            },
            "tool_confusion_matrix": self.confusion,
            "failures_first_20": self.failures,
            "examples": self.examples,
        }


# ─── Inference ───────────────────────────────────────────────────────────────

def run_needle(checkpoint, query, tools_path, max_len=400, seed=42):
    """Run needle run CLI and return (raw_output, elapsed_seconds)."""
    cmd = [
        "needle", "run",
        "--checkpoint", checkpoint,
        "--query", query,
        "--tools", tools_path,
        "--max-len", str(max_len),
        "--seed", str(seed),
        "--temperature", "0",
    ]
    start = time.time()
    result = subprocess.run(cmd, capture_output=True, text=True, timeout=600)
    elapsed = time.time() - start
    raw = result.stdout + result.stderr
    return raw, elapsed


# ─── Category Detector ───────────────────────────────────────────────────────

def detect_category(query, expected_calls):
    q = query.lower()
    names = {c["name"] for c in expected_calls}
    if not expected_calls:
        return "NO_CALL"
    if "unknown" in q:
        return "UNKNOWN"
    if "image" in q or "imagepull" in q or "errimagepull" in q:
        return "IMAGE_PULL"
    if "oom" in q or ("memory" in q and "kill" in q):
        return "OOM"
    if "crash" in q or "restart" in q:
        return "CRASH_LOOP"
    if "pending" in q or "unschedul" in q:
        return "PENDING"
    if "get_service_status" in names:
        return "SERVICE"
    if "get_storage_status" in names:
        return "STORAGE"
    if "get_configuration_status" in names:
        return "CONFIGURATION"
    if "get_workload_status" in names:
        return "WORKLOAD"
    if "get_gpu_status" in names:
        return "GPU"
    if "get_node_status" in names:
        return "NODE"
    if "healthy" in q or ("running" in q and "fail" not in q):
        return "HEALTHY"
    return "OTHER"


# ─── Main ────────────────────────────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser(description="Needle 3 Benchmark")
    parser.add_argument("--checkpoint", required=True)
    parser.add_argument("--split", required=True, help="JSONL eval file")
    parser.add_argument("--tools", default="dataset/tools.json")
    parser.add_argument("--max-examples", type=int, default=None)
    parser.add_argument("--max-len", type=int, default=400)
    parser.add_argument("--seed", type=int, default=42)
    parser.add_argument("--out", required=True, help="Output metrics JSON")
    args = parser.parse_args()

    for p in [args.checkpoint, args.split, args.tools]:
        if not os.path.exists(p):
            print(f"ERROR: File not found: {p}", file=sys.stderr)
            sys.exit(1)

    examples = []
    with open(args.split) as f:
        for line in f:
            line = line.strip()
            if line:
                examples.append(json.loads(line))

    if args.max_examples:
        examples = examples[:args.max_examples]

    with open(args.tools) as f:
        tools_data = json.load(f)
    valid_tool_names = {t["name"] for t in tools_data}

    print(f"=== Needle 3 Benchmark ===")
    print(f"  Checkpoint : {args.checkpoint}")
    print(f"  Split      : {args.split} ({len(examples)} examples)")
    print(f"  Max length : {args.max_len} tokens")
    print(f"  Output     : {args.out}")
    print()

    metrics = BenchmarkMetrics()

    for i, ex in enumerate(examples):
        query = ex["query"]
        expected_calls = ex.get("answers", [])
        category = detect_category(query, expected_calls)

        print(f"[{i+1:3}/{len(examples)}] {query[:70]!r}")
        print(f"           expected: {[c['name'] for c in expected_calls]}")

        try:
            raw_output, elapsed = run_needle(args.checkpoint, query, args.tools, args.max_len, args.seed)
            predicted_calls = parse_needle_output(raw_output)

            for c in predicted_calls:
                if not c.get("_invalid") and c.get("name") not in valid_tool_names:
                    c["_invalid"] = True
                    c["_reason"] = f"unknown tool: {c.get('name')}"

            print(f"           predicted: {[c.get('name','?') for c in predicted_calls if not c.get('_invalid')]}  [{elapsed:.1f}s]")
            metrics.record(query, expected_calls, predicted_calls, elapsed, category)

        except subprocess.TimeoutExpired:
            print(f"           TIMEOUT after 600s")
            metrics.record(query, expected_calls, [{"_invalid": True, "_reason": "timeout"}], 600.0, category)
        except Exception as e:
            print(f"           ERROR: {e}")
            metrics.record(query, expected_calls, [{"_invalid": True, "_reason": str(e)}], 0.0, category)

    os.makedirs(os.path.dirname(args.out) if os.path.dirname(args.out) else ".", exist_ok=True)
    report = metrics.report()
    with open(args.out, "w") as f:
        json.dump(report, f, indent=2)

    print()
    print("=" * 60)
    print("  RESULTS SUMMARY")
    print("=" * 60)
    print(f"  Total examples              : {report['total_examples']}")
    print(f"  Exact match accuracy        : {report['exact_match_accuracy']}%")
    print(f"  Tool selection accuracy     : {report['tool_selection_accuracy']}%")
    print(f"  No-call accuracy            : {report['no_call_accuracy']}%")
    print(f"  Multi-tool exact match      : {report['multi_tool_exact_match']}%")
    print(f"  Invalid tool rate           : {report['invalid_tool_rate']}%")
    print(f"  Argument accuracy           : {report['argument_accuracy']}%")
    print(f"  Missing argument rate       : {report['missing_argument_rate']}%")
    print(f"  Wrong argument rate         : {report['wrong_argument_rate']}%")
    print(f"  Extra/invented args         : {report['invented_argument_count']}")
    print(f"  Avg inference time          : {report['avg_inference_time_s']}s")
    print(f"  Total inference time        : {report['total_inference_time_s']}s")
    print()
    print("  Per-category:")
    for cat, cv in sorted(report["per_category"].items()):
        print(f"    {cat:<22} exact={cv['exact_match_pct']:5}%  tools={cv['tool_selection_pct']:5}%  n={cv['total']}")
    print()
    if report["failures_first_20"]:
        print("  Sample failures:")
        for fi, fail in enumerate(report["failures_first_20"][:5]):
            print(f"    [{fi+1}] {fail['query'][:65]!r}")
            print(f"         expected : {[c['name'] for c in fail['expected']]}")
            print(f"         predicted: {[c.get('name','?') for c in fail['predicted'] if not c.get('_invalid')]}")
            print(f"         errors   : {fail['error_types'][:4]}")
    print()
    print(f"  Full metrics → {args.out}")


if __name__ == "__main__":
    main()
