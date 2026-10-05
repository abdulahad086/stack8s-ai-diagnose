import json
import sys

def dedup(filepath):
    seen = set()
    lines = []
    with open(filepath, 'r') as f:
        for line in f:
            if not line.strip(): continue
            data = json.loads(line)
            q = data['query']
            if q not in seen:
                seen.add(q)
                lines.append(line)
    with open(filepath, 'w') as f:
        for line in lines:
            f.write(line)
    print(f"Deduped {filepath}: {len(lines)} lines")

if __name__ == '__main__':
    dedup('dataset/train.jsonl')
    dedup('dataset/validation.jsonl')
    dedup('dataset/test.jsonl')
