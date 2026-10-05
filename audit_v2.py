import json
import os
import glob

tools_path = "dataset/tools.json"
with open(tools_path) as f:
    tools_list = json.load(f)
tool_schemas = {t["name"]: t for t in tools_list}

def is_grounded(arg_key, arg_val, query):
    if arg_val is None:
        return False
    val_str = str(arg_val).strip()
    if not val_str:
        return False
    
    q_lower = query.lower()
    val_lower = val_str.lower()
    
    # Substring check
    if val_lower in q_lower:
        return True
    
    return False

def audit_file(filepath):
    examples = []
    with open(filepath) as f:
        for line_num, line in enumerate(f, 1):
            line = line.strip()
            if not line:
                continue
            ex = json.loads(line)
            examples.append((line_num, ex))
    return examples

splits = ["train", "validation", "test"]
all_examples = []
for split in splits:
    path = f"dataset/{split}.jsonl"
    if os.path.exists(path):
        exs = audit_file(path)
        for line_num, ex in exs:
            all_examples.append((split, line_num, ex))

total_examples = len(all_examples)
total_expected_calls = 0
total_arguments = 0
grounded_arguments = 0
ungrounded_arguments = 0
affected_cases = []
multitool_cases = []

for split, line_num, ex in all_examples:
    query = ex.get("query", "")
    answers = ex.get("answers", [])
    total_expected_calls += len(answers)
    
    if len(answers) > 1:
        multitool_cases.append((split, line_num, query, answers))
    
    ex_has_ungrounded = False
    ex_ungrounded_details = []
    
    for ans in answers:
        tname = ans.get("name", "")
        args = ans.get("arguments", {})
        for k, v in args.items():
            total_arguments += 1
            grounded = is_grounded(k, v, query)
            if grounded:
                grounded_arguments += 1
            else:
                ungrounded_arguments += 1
                ex_has_ungrounded = True
                ex_ungrounded_details.append({"tool": tname, "arg_key": k, "arg_val": v})
    
    if ex_has_ungrounded:
        affected_cases.append({
            "split": split,
            "line_num": line_num,
            "query": query,
            "ungrounded": ex_ungrounded_details
        })

print(f"Total Examples: {total_examples}")
print(f"Total Expected Tool Calls: {total_expected_calls}")
print(f"Total Arguments: {total_arguments}")
print(f"Grounded Arguments: {grounded_arguments} ({grounded_arguments/total_arguments*100:.1f}%)")
print(f"Ungrounded Arguments: {ungrounded_arguments} ({ungrounded_arguments/total_arguments*100:.1f}%)")
print(f"Affected Cases (with at least 1 ungrounded arg): {len(affected_cases)} ({len(affected_cases)/total_examples*100:.1f}%)")
print(f"Multi-Tool Cases: {len(multitool_cases)} ({len(multitool_cases)/total_examples*100:.1f}%)")

print("\nSample Ungrounded Arguments Affected Cases:")
for case in affected_cases[:10]:
    print(f"[{case['split']}:L{case['line_num']}] Query: {case['query']}")
    for u in case['ungrounded']:
        print(f"  -> Tool: {u['tool']}, Arg: {u['arg_key']}={u['arg_val']}")
