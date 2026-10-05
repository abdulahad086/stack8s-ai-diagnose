#!/usr/bin/env python3
"""
Feature 19 — Needle 3 In-Process Benchmark
Loads the model ONCE, evaluates all examples in one session.
Uses the exact internal Needle 3 API discovered via inspection.

Usage:
  source venv/bin/activate
  python3 benchmark_inproc.py \
    --checkpoint ./checkpoints/needle3.safetensors \
    --split dataset/validation.jsonl \
    --max-examples 10 \
    --out models/needle3/baseline/metrics_small.json
"""

import argparse
import json
import os
import re
import sys
import time


# ─── Output Parser ────────────────────────────────────────────────────────────

def parse_tool_calls(raw: str) -> list:
    """Parse <tool_call>[...]</tool_call> from Needle 3 output (confirmed format)."""
    raw = re.sub(r'<think>.*?</think>', '', raw, flags=re.DOTALL)
    calls = []
    for m in re.findall(r'<tool_call>(.*?)</tool_call>', raw, re.DOTALL):
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
            calls.append({"_invalid": True, "_raw": m[:200]})
    return calls


# ─── Evaluation ───────────────────────────────────────────────────────────────

def evaluate_example(query, expected_calls, predicted_calls, valid_tools):
    for c in predicted_calls:
        if not c.get("_invalid") and c.get("name") not in valid_tools:
            c["_invalid"] = True

    q_lower = query.lower()
    unsupported_label_args = []
    for exp_call in expected_calls:
        for k, v in exp_call.get("arguments", {}).items():
            if str(v).lower().strip() not in q_lower:
                unsupported_label_args.append({"tool": exp_call["name"], "arg": k, "val": v})

    has_invalid = any(c.get("_invalid") for c in predicted_calls)
    expected_names = [c["name"] for c in expected_calls]
    predicted_names = [c.get("name", "") for c in predicted_calls if not c.get("_invalid")]
    tools_correct = sorted(expected_names) == sorted(predicted_names)

    missing_tools = [e for e in expected_names if e not in predicted_names]
    extra_tools = [p for p in predicted_names if p not in expected_names]

    arg_details = []
    arg_c = arg_w = arg_m = arg_e = 0

    for exp_call in expected_calls:
        exp_name = exp_call["name"]
        exp_args = exp_call.get("arguments", {})
        pred_call = next(
            (c for c in predicted_calls if c.get("name") == exp_name and not c.get("_invalid")),
            None
        )
        if pred_call is None:
            for k in exp_args:
                arg_m += 1
                arg_details.append({"tool": exp_name, "arg": k, "status": "missing",
                                     "expected": exp_args[k], "predicted": None})
            continue
        pred_args = pred_call.get("arguments", {})
        for k, v in exp_args.items():
            if k not in pred_args:
                arg_m += 1
                arg_details.append({"tool": exp_name, "arg": k, "status": "missing",
                                     "expected": v, "predicted": None})
            elif str(pred_args[k]) == str(v):
                arg_c += 1
                arg_details.append({"tool": exp_name, "arg": k, "status": "correct",
                                     "expected": v, "predicted": pred_args[k]})
            else:
                arg_w += 1
                arg_details.append({"tool": exp_name, "arg": k, "status": "wrong",
                                     "expected": v, "predicted": pred_args[k]})
        for k in pred_args:
            if k not in exp_args:
                arg_e += 1
                arg_details.append({"tool": exp_name, "arg": k, "status": "invented",
                                     "expected": None, "predicted": pred_args[k]})

    args_exact = all(d["status"] == "correct" for d in arg_details) if arg_details else (
        len(expected_calls) == 0 and len(predicted_calls) == 0
    )
    exact = tools_correct and args_exact

    return {
        "exact_match": exact,
        "tools_correct": tools_correct,
        "has_invalid": has_invalid,
        "has_label_error": len(unsupported_label_args) > 0,
        "unsupported_label_args_count": len(unsupported_label_args),
        "missing_tools_count": len(missing_tools),
        "extra_tools_count": len(extra_tools),
        "expected_names": expected_names,
        "predicted_names": predicted_names,
        "arg_correct": arg_c,
        "arg_wrong": arg_w,
        "arg_missing": arg_m,
        "arg_extra": arg_e,
        "arg_details": arg_details,
    }


def detect_category(query, expected_calls):
    q = query.lower()
    names = {c["name"] for c in expected_calls}
    if not expected_calls:
        return "NO_CALL"
    if "image" in q or "imagepull" in q:
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
    if "unknown" in q:
        return "UNKNOWN"
    return "OTHER"


# ─── Main ────────────────────────────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--checkpoint", required=True)
    parser.add_argument("--split", required=True)
    parser.add_argument("--tools", default="dataset/tools.json")
    parser.add_argument("--max-examples", type=int, default=None)
    parser.add_argument("--max-len", type=int, default=300)
    parser.add_argument("--seed", type=int, default=42)
    parser.add_argument("--out", required=True)
    args = parser.parse_args()

    # ── Load tools
    with open(args.tools) as f:
        tools_list = json.load(f)
    valid_tools = {t["name"] for t in tools_list}

    # ── Load examples
    examples = []
    with open(args.split) as f:
        for line in f:
            line = line.strip()
            if line:
                examples.append(json.loads(line))
    if args.max_examples:
        examples = examples[:args.max_examples]

    print(f"\n=== Needle 3 In-Process Benchmark ===", flush=True)
    print(f"  Checkpoint : {args.checkpoint}", flush=True)
    print(f"  Split      : {args.split} ({len(examples)} examples)", flush=True)
    print(f"  Max tokens : {args.max_len}", flush=True)
    print(f"  Output     : {args.out}", flush=True)
    print(flush=True)

    # ── Load Needle 3 (exact API from source inspection)
    from needle.model.run import load_checkpoint, generate, get_tokenizer, build_prompt, SimpleAttentionNetwork

    print(f"  Loading checkpoint...", flush=True)
    t_load = time.time()
    params, config = load_checkpoint(args.checkpoint)
    model = SimpleAttentionNetwork(config)
    tokenizer = get_tokenizer(config.vocab_size)
    t_load = time.time() - t_load
    print(f"  Loaded in {t_load:.1f}s. vocab_size={config.vocab_size}", flush=True)

    # ── JIT warmup
    print(f"  JIT warmup (tiny prompt)...", flush=True)
    t_warm = time.time()
    try:
        generate(model, params, tokenizer,
                 "<|im_start|>user\nping<|im_end|>\n<|im_start|>assistant\n",
                 max_new_tokens=3, temperature=0.0, seed=0, stream=False)
        print(f"  JIT warm in {time.time()-t_warm:.1f}s\n", flush=True)
    except Exception as e:
        print(f"  JIT warmup error: {e}\n", flush=True)

    # ── Evaluate
    all_results = []
    raw_outputs = []
    failures = []
    category_stats = {}

    total = exact_sum = tools_sum = no_call_correct = no_call_total = 0
    multi_exact = multi_total = invalid_sum = 0
    arg_c_total = arg_w_total = arg_m_total = arg_e_total = 0
    inference_times = []

    for i, ex in enumerate(examples):
        query = ex["query"]
        expected_calls = ex.get("answers", [])
        category = detect_category(query, expected_calls)

        print(f"[{i+1:2}/{len(examples)}] {query[:70]!r}", flush=True)
        print(f"           expected : {[c['name'] for c in expected_calls]}", flush=True)

        # Build prompt exactly as needle CLI does
        prompt = build_prompt(query, tools_list)

        raw_out = ""
        elapsed = 0.0
        predicted_calls = []
        t0 = time.time()
        try:
            raw_out = generate(
                model, params, tokenizer, prompt,
                max_new_tokens=args.max_len,
                temperature=0.0,
                seed=args.seed,
                stream=False,   # capture output instead of printing
            )
            elapsed = time.time() - t0
            predicted_calls = parse_tool_calls(raw_out)
        except Exception as e:
            elapsed = time.time() - t0
            print(f"           ERROR   : {e}", flush=True)
            predicted_calls = [{"_invalid": True, "_reason": str(e)}]

        pred_names = [c.get("name", "?") for c in predicted_calls if not c.get("_invalid")]
        print(f"           predicted: {pred_names}  [{elapsed:.1f}s]", flush=True)

        raw_outputs.append({
            "index": i,
            "query": query,
            "raw_output": raw_out[:2000],
            "elapsed_s": round(elapsed, 2),
        })

        m = evaluate_example(query, expected_calls, predicted_calls, valid_tools)
        m.update({"query": query, "category": category, "elapsed_s": round(elapsed, 2),
                   "expected_calls": expected_calls, "predicted_calls": predicted_calls})
        all_results.append(m)

        total += 1
        inference_times.append(elapsed)
        if m["exact_match"]:
            exact_sum += 1
        if m["tools_correct"]:
            tools_sum += 1
        if m["has_invalid"]:
            invalid_sum += 1
        if len(expected_calls) == 0:
            no_call_total += 1
            if len(predicted_calls) == 0:
                no_call_correct += 1
        if len(expected_calls) > 1:
            multi_total += 1
            if m["exact_match"]:
                multi_exact += 1
        arg_c_total += m["arg_correct"]
        arg_w_total += m["arg_wrong"]
        arg_m_total += m["arg_missing"]
        arg_e_total += m["arg_extra"]

        if category not in category_stats:
            category_stats[category] = {"total": 0, "exact": 0, "tools": 0}
        category_stats[category]["total"] += 1
        if m["exact_match"]:
            category_stats[category]["exact"] += 1
        if m["tools_correct"]:
            category_stats[category]["tools"] += 1

        if not m["exact_match"] and len(failures) < 20:
            errs = []
            if m["has_invalid"]:
                errs.append("invalid_tool")
            for e_name in m["expected_names"]:
                if e_name not in m["predicted_names"]:
                    errs.append(f"missing_tool:{e_name}")
            for p_name in m["predicted_names"]:
                if p_name not in m["expected_names"]:
                    errs.append(f"extra_tool:{p_name}")
            for d in m["arg_details"]:
                if d["status"] != "correct":
                    errs.append(f"{d['status']}_arg:{d['arg']}")
            failures.append({
                "index": i, "query": query[:120],
                "expected": expected_calls, "predicted": predicted_calls,
                "error_types": errs, "category": category,
            })

    # ── Build report
    def pct(n, d):
        return round(100.0 * n / d, 1) if d > 0 else 0.0

    arg_total = arg_c_total + arg_w_total + arg_m_total
    avg_time = sum(inference_times) / len(inference_times) if inference_times else 0

    report = {
        "checkpoint": args.checkpoint,
        "split": args.split,
        "total_examples": total,
        "exact_match_accuracy": pct(exact_sum, total),
        "tool_selection_accuracy": pct(tools_sum, total),
        "no_call_accuracy": pct(no_call_correct, no_call_total) if no_call_total else None,
        "multi_tool_exact_match": pct(multi_exact, multi_total) if multi_total else None,
        "invalid_tool_rate": pct(invalid_sum, total),
        "argument_accuracy": pct(arg_c_total, arg_total) if arg_total else None,
        "missing_argument_rate": pct(arg_m_total, arg_total) if arg_total else None,
        "wrong_argument_rate": pct(arg_w_total, arg_total) if arg_total else None,
        "extra_argument_rate": pct(arg_e_total, arg_total) if arg_total else None,
        "invented_argument_count": arg_e_total,
        "avg_inference_time_s": round(avg_time, 2),
        "total_inference_time_s": round(sum(inference_times), 2),
        "model_load_time_s": round(t_load, 2),
        "per_category": {
            cat: {
                "total": v["total"],
                "exact_match_pct": pct(v["exact"], v["total"]),
                "tool_selection_pct": pct(v["tools"], v["total"]),
            }
            for cat, v in category_stats.items()
        },
        "failures_first_20": failures,
        "examples": [
            {k: v for k, v in r.items() if k not in ("expected_calls", "predicted_calls")}
            for r in all_results
        ],
    }

    # ── Save
    os.makedirs(os.path.dirname(args.out) if os.path.dirname(args.out) else ".", exist_ok=True)
    base_dir = os.path.dirname(args.out) or "."

    with open(args.out, "w") as f:
        json.dump(report, f, indent=2)

    with open(os.path.join(base_dir, "raw_outputs.jsonl"), "w") as f:
        for rec in raw_outputs:
            f.write(json.dumps(rec) + "\n")

    with open(os.path.join(base_dir, "predictions.jsonl"), "w") as f:
        for r in all_results:
            f.write(json.dumps({
                "query": r["query"],
                "expected": r["expected_names"],
                "predicted": r["predicted_names"],
                "exact_match": r["exact_match"],
                "tools_correct": r["tools_correct"],
                "arg_details": r["arg_details"],
                "category": r["category"],
                "elapsed_s": r["elapsed_s"],
            }) + "\n")

    # ── Print summary
    print()
    print("=" * 62)
    print("  BASE MODEL RESULTS SUMMARY")
    print("=" * 62)
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
    if failures:
        print(f"  Failures ({len(failures)} of {total-exact_sum}):")
        for fi, fail in enumerate(failures[:8]):
            exp_n = [c['name'] for c in fail['expected']]
            pred_n = [c.get('name', '?') for c in fail['predicted'] if not c.get('_invalid')]
            print(f"    [{fi+1}] {fail['query'][:65]!r}")
            print(f"         expected : {exp_n}")
            print(f"         predicted: {pred_n}")
            print(f"         errors   : {fail['error_types'][:5]}")
    print()
    print(f"  Saved → {args.out}")
    print(f"  Raw   → {os.path.join(base_dir, 'raw_outputs.jsonl')}")
    print(f"  Preds → {os.path.join(base_dir, 'predictions.jsonl')}")


if __name__ == "__main__":
    main()
