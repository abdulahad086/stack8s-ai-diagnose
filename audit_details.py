import json

with open("dataset/tools.json") as f:
    tools_list = json.load(f)

def audit_details():
    splits = ["train", "validation", "test"]
    affected = []
    by_tool_arg = {}
    
    for split in splits:
        with open(f"dataset/{split}.jsonl") as f:
            for lidx, line in enumerate(f, 1):
                if not line.strip(): continue
                ex = json.loads(line)
                q = ex.get("query", "")
                q_lower = q.lower()
                for ans in ex.get("answers", []):
                    tname = ans.get("name", "")
                    for k, v in ans.get("arguments", {}).items():
                        v_str = str(v)
                        if v_str.lower() not in q_lower:
                            affected.append({
                                "split": split,
                                "line": lidx,
                                "query": q,
                                "tool": tname,
                                "arg_key": k,
                                "arg_val": v
                            })
                            key = f"{tname}::{k}"
                            by_tool_arg[key] = by_tool_arg.get(key, 0) + 1

    print(f"Total ungrounded argument occurrences: {len(affected)}")
    print("\nBreakdown by tool and argument:")
    for k, count in sorted(by_tool_arg.items(), key=lambda x: x[1], reverse=True):
        print(f"  {k}: {count}")

    print("\nDetailed list of ungrounded cases:")
    for item in affected:
        print(f"[{item['split']}:L{item['line']}] Tool: {item['tool']} | Arg: {item['arg_key']}='{item['arg_val']}' | Query: {item['query']}")

if __name__ == "__main__":
    audit_details()
