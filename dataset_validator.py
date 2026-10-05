#!/usr/bin/env python3
"""
Feature 20 — Comprehensive Dataset V3 Validator
Performs 11 quality validation checks across train.jsonl, validation.jsonl,
test.jsonl, manifest.jsonl, tools.json, and evidence snapshots.
"""

import json
import os
import re
import glob
import sys

def validate_all():
    print("==================================================")
    print("  Dataset V3 Comprehensive Quality Validator")
    print("==================================================")
    
    tools_path = "dataset/tools.json"
    if not os.path.exists(tools_path):
        print("FAIL: dataset/tools.json not found")
        sys.exit(1)
        
    with open(tools_path) as f:
        tools_list = json.load(f)
    
    valid_tools = {t["name"]: t for t in tools_list}
    if len(valid_tools) != 9:
        print(f"FAIL: Expected 9 tools in dataset/tools.json, got {len(valid_tools)}")
        sys.exit(1)
        
    splits = ["train", "validation", "test"]
    split_counts = {}
    split_pods = {"train": set(), "validation": set(), "test": set()}
    split_namespaces = {"train": set(), "validation": set(), "test": set()}
    
    queries_seen = set()
    case_ids_seen = set()
    
    total_examples = 0
    total_tools_called = 0
    total_args_checked = 0
    
    healthy_count = 0
    unknown_count = 0
    no_call_count = 0
    multi_tool_count = 0
    
    controlled_count = 0
    synthetic_count = 0
    doc_count = 0

    secret_pattern = re.compile(r'(?i)(bearer\s+[A-Za-z0-9\-\._~\+\/]+=*|password\s*=\s*[\'"][^\'"]+[\'"]|-----BEGIN CERTIFICATE-----|eyJhbGciOi)')
    placeholder_pattern = re.compile(r'(<pod|TODO|placeholder|\[REDACTED\])')
    
    # ── 1. Validate Manifest ──────────────────────────────────────────────────
    manifest_path = "dataset/incidents/manifest.jsonl"
    if not os.path.exists(manifest_path):
        print("FAIL: dataset/incidents/manifest.jsonl missing")
        sys.exit(1)
        
    manifest_records = []
    with open(manifest_path) as f:
        for lnum, line in enumerate(f, 1):
            line = line.strip()
            if not line:
                continue
            rec = json.loads(line)
            cid = rec.get("case_id")
            if not cid:
                print(f"FAIL [manifest:L{lnum}]: Missing case_id")
                sys.exit(1)
            if cid in case_ids_seen:
                print(f"FAIL [manifest:L{lnum}]: Duplicate case_id {cid}")
                sys.exit(1)
            case_ids_seen.add(cid)
            
            stype = rec.get("source_type")
            if stype not in ["controlled_kubernetes", "synthetic_unit_test", "documentation_derived"]:
                print(f"FAIL [manifest:L{lnum}]: Invalid source_type {stype}")
                sys.exit(1)
                
            cat = rec.get("scenario_category")
            if stype == "controlled_kubernetes":
                controlled_count += 1
                if "GPU" in cat:
                    print(f"FAIL [manifest:L{lnum}]: GPU case cannot be controlled_kubernetes (Task 6 violation)!")
                    sys.exit(1)
                # Evidence provenance check
                ev_rel = rec.get("evidence_snapshot")
                if not ev_rel or not os.path.exists(os.path.join("dataset", os.path.basename(ev_rel))):
                    ev_path = os.path.join("dataset", ev_rel) if ev_rel else ""
                    if not os.path.exists(ev_path):
                        print(f"FAIL [manifest:L{lnum}]: Missing evidence file {ev_rel} for controlled case {cid}")
                        sys.exit(1)
            elif stype == "synthetic_unit_test":
                synthetic_count += 1
            elif stype == "documentation_derived":
                doc_count += 1
                
            manifest_records.append(rec)
            
    print(f"✅ Manifest check passed: {len(manifest_records)} records audited.")
    print(f"   Provenance breakdown: Controlled={controlled_count}, Synthetic={synthetic_count}, Doc-derived={doc_count}")

    # ── 2. Validate JSONL Splits ─────────────────────────────────────────────
    for split in splits:
        spath = f"dataset/{split}.jsonl"
        if not os.path.exists(spath):
            print(f"FAIL: {spath} missing")
            sys.exit(1)
            
        count = 0
        with open(spath) as f:
            for lnum, line in enumerate(f, 1):
                line = line.strip()
                if not line:
                    continue
                
                # Check secrets and placeholders
                if secret_pattern.search(line):
                    print(f"FAIL [{split}:L{lnum}]: Secret pattern detected!")
                    sys.exit(1)
                if placeholder_pattern.search(line):
                    print(f"FAIL [{split}:L{lnum}]: Placeholder pattern detected!")
                    sys.exit(1)
                    
                data = json.loads(line)
                query = data.get("query", "").strip()
                if not query:
                    print(f"FAIL [{split}:L{lnum}]: Empty query")
                    sys.exit(1)
                    
                if query in queries_seen:
                    print(f"FAIL [{split}:L{lnum}]: Duplicate query '{query}'")
                    sys.exit(1)
                queries_seen.add(query)
                
                answers = data.get("answers", [])
                if len(answers) == 0:
                    no_call_count += 1
                elif len(answers) > 1:
                    multi_tool_count += 1
                    
                if "HEALTHY" in query.upper() or "healthy" in query.lower():
                    healthy_count += 1
                if "UNKNOWN" in query.upper() or "unknown" in query.lower() or "completed" in query.lower() or "insufficient" in query.lower():
                    unknown_count += 1
                    
                # Schema and Grounding audit
                q_lower = query.lower()
                for ans_idx, ans in enumerate(answers):
                    tname = ans.get("name")
                    if tname not in valid_tools:
                        print(f"FAIL [{split}:L{lnum}]: Unknown tool '{tname}'")
                        sys.exit(1)
                    schema = valid_tools[tname]
                    args = ans.get("arguments", {})
                    
                    # Required args check
                    for req_arg in schema["parameters"]["required"]:
                        if req_arg not in args or str(args[req_arg]).strip() == "":
                            print(f"FAIL [{split}:L{lnum}]: Required arg '{req_arg}' missing for tool '{tname}'")
                            sys.exit(1)
                            
                    # Grounding check
                    for k, v in args.items():
                        total_args_checked += 1
                        v_str = str(v).lower().strip()
                        if v_str not in q_lower:
                            print(f"FAIL [{split}:L{lnum}]: Ungrounded arg '{k}'='{v}' for tool '{tname}' in query: '{query}'")
                            sys.exit(1)
                            
                        # Collect pod and namespace for leakage check
                        if k == "pod" and v_str not in ["default", "kube-system"]:
                            split_pods[split].add(v_str)
                        if k == "namespace" and v_str not in ["default", "kube-system"]:
                            split_namespaces[split].add(v_str)
                            
                    total_tools_called += 1
                    
                count += 1
                total_examples += 1
        split_counts[split] = count
        
    print(f"✅ JSONL splits check passed: Total={total_examples} (Train={split_counts['train']}, Val={split_counts['validation']}, Test={split_counts['test']})")
    print(f"✅ Grounding check passed: 100% of {total_args_checked} arguments strictly grounded in user query text.")
    print(f"   Diagnostic Stats: Healthy={healthy_count}, UNKNOWN={unknown_count}, Empty Answers []={no_call_count}, Multi-tool={multi_tool_count}")

    # ── 3. Validate Data Leakage ──────────────────────────────────────────────
    # Pod leakage
    train_test_pod_overlap = split_pods["train"].intersection(split_pods["test"])
    train_val_pod_overlap = split_pods["train"].intersection(split_pods["validation"])
    val_test_pod_overlap = split_pods["validation"].intersection(split_pods["test"])
    
    if train_test_pod_overlap or train_val_pod_overlap or val_test_pod_overlap:
        print(f"FAIL: Pod leakage detected! Overlaps: {train_test_pod_overlap | train_val_pod_overlap | val_test_pod_overlap}")
        sys.exit(1)
        
    # Namespace leakage
    train_test_ns_overlap = split_namespaces["train"].intersection(split_namespaces["test"])
    train_val_ns_overlap = split_namespaces["train"].intersection(split_namespaces["validation"])
    val_test_ns_overlap = split_namespaces["validation"].intersection(split_namespaces["test"])
    
    if train_test_ns_overlap or train_val_ns_overlap or val_test_ns_overlap:
        print(f"FAIL: Namespace leakage detected! Overlaps: {train_test_ns_overlap | train_val_ns_overlap | val_test_ns_overlap}")
        sys.exit(1)
        
    print("✅ Split leakage check passed: Train, Validation, and Test pods/namespaces are strictly disjoint.")

    # ── Summary ──────────────────────────────────────────────────────────────
    print("\n==================================================")
    print("  ALL 11 QUALITY VALIDATION CHECKS PASSED ✅")
    print("==================================================\n")

if __name__ == "__main__":
    validate_all()
