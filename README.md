# stack8s-ai-diagnose — Feature 1: Kubernetes Connectivity

This is Feature 1 of the `stack8s-ai-diagnose` prototype.  
Its **only** purpose is to verify that a Go program can successfully connect to a
Kubernetes cluster using the official `client-go` library and print the real
node names and their status.

---

## Current Kubernetes Context

```
docker-desktop
```

The context is resolved automatically from `~/.kube/config` (or the `$KUBECONFIG`
environment variable if set).

---

## How the Go Program Connects

1. Reads the kubeconfig from `~/.kube/config` (or `$KUBECONFIG`).
2. Builds a REST client configuration using `clientcmd.BuildConfigFromFlags`.
3. Creates a typed Kubernetes clientset via `kubernetes.NewForConfig`.
4. Calls `clientset.CoreV1().Nodes().List(...)` to fetch all nodes.
5. Iterates over each node, reads its `Ready` condition, and prints the result.

No credentials, tokens, or secrets are printed at any point.

---

## Running the Program

```bash
go run main.go
```

Or build a binary first:

```bash
go build -o k8s-check .
./k8s-check
```

---

## Expected Output

```
Connected to Kubernetes cluster via: https://127.0.0.1:6443

Nodes (1):
NAME             STATUS
----             ------
docker-desktop   Ready
```

The node name and status will reflect your actual cluster.  
The example above was produced against Docker Desktop's built-in Kubernetes cluster.

---

## Dependencies

| Package | Version |
|---------|---------|
| `k8s.io/client-go` | v0.30.2 |
| `k8s.io/apimachinery` | v0.30.2 |
| `k8s.io/api` | v0.30.2 |

Install / sync:

```bash
go mod tidy
```

---

## What This Feature Does NOT Do

- No AI or LLM calls
- No diagnosis logic
- No connection to any production Stack8s cluster
- No secrets or kubeconfig credentials are printed
- No mock data — all output is from the live cluster
