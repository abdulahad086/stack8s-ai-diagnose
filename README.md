# stack8s-ai-diagnose

A Go prototype that connects to a real Kubernetes cluster and collects diagnostic information.

---

## Feature 1 — Kubernetes Connectivity

### What it does

Verifies that the Go program can connect to a real Kubernetes cluster using `client-go` and list all cluster nodes with their Ready status.

### Current Kubernetes Context

```
docker-desktop
```

### How it connects

1. Reads kubeconfig from `$KUBECONFIG` or `~/.kube/config`.
2. Builds a REST config via `clientcmd.BuildConfigFromFlags`.
3. Creates a typed clientset via `kubernetes.NewForConfig`.
4. Calls `clientset.CoreV1().Nodes().List(...)`.

No credentials or tokens are printed.

### How to run

```bash
go run main.go
```

Or build first:

```bash
go build -o k8s-check .
./k8s-check
```

### Expected output (Feature 1 section)

```
Connected to Kubernetes cluster via: https://127.0.0.1:6443

Nodes (1):
NAME              STATUS
----              ------
docker-desktop    Ready
```

---

## Feature 2 — Pod Status Collector

### What it does

Extends the Go program to retrieve and display the real status of all pods in the `default` namespace of the live Kubernetes cluster.

No AI, no diagnosis logic, no Stack8s integration.

### Fields collected per pod

| # | Field | Source |
|---|-------|--------|
| 1 | Pod name | `pod.Name` |
| 2 | Namespace | `pod.Namespace` |
| 3 | Pod phase | `pod.Status.Phase` |
| 4 | Creation time | `pod.CreationTimestamp` |
| 5 | Node name | `pod.Spec.NodeName` |
| 6 | Container name(s) | `pod.Spec.Containers[*].Name` |
| 7 | Container image(s) | `pod.Spec.Containers[*].Image` |
| 8 | Container ready state | `pod.Status.ContainerStatuses[*].Ready` |
| 9 | Restart count | `pod.Status.ContainerStatuses[*].RestartCount` |
| 10 | Current container state | `pod.Status.ContainerStatuses[*].State` |
| 11 | Last termination reason | `pod.Status.ContainerStatuses[*].LastTerminationState.Terminated.Reason` |
| 12 | Last termination exit code | `pod.Status.ContainerStatuses[*].LastTerminationState.Terminated.ExitCode` |

### Security note

- **No Secret values are collected or printed.**
- No environment variable values are printed.
- No passwords, tokens, certificates, or kubeconfig credentials are printed.

### Namespace used

`default` — hardcoded for the MVP. Other namespaces are not queried.

### How to run

```bash
go run main.go
```

Feature 1 (nodes) and Feature 2 (pods) output together in a single run.

### How to verify with kubectl

```bash
# Check what kubectl sees:
kubectl get pods -n default -o wide

# For per-container details:
kubectl describe pod <pod-name> -n default
```

### Actual test result (Feature 2)

Test pod deployed:
```bash
kubectl apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: test-nginx
  namespace: default
spec:
  containers:
  - name: nginx
    image: nginx:1.27-alpine
    ports:
    - containerPort: 80
EOF
```

Program output:
```
── Pod Status Collector (namespace: default) ──────────────────────────
Pods found: 1

[1/1] Pod: test-nginx
    Namespace   : default
    Phase       : Running
    Created     : 2026-09-30 11:58:50 UTC
    Node        : docker-desktop

    Container   : nginx
      Image     : nginx:1.27-alpine
      Ready     : true
      Restarts  : 0
      State     : Running (started: 2026-09-30 11:58:56)
```

`kubectl get pods -n default` output (verified match):
```
NAME         READY   STATUS    RESTARTS   AGE   IP         NODE
test-nginx   1/1     Running   0          80s   10.1.0.6   docker-desktop
```

All fields match. ✅

### Edge cases handled

| Scenario | Handling |
|----------|----------|
| No pods in namespace | Prints `No pods found in namespace: default` |
| Pod pending / not yet scheduled | Shows `<not yet scheduled>` for node name |
| Pod pending with no container status | Shows spec-level image, `Ready: false`, `State: Pending` |
| Container in `Running` state | Shows start time |
| Container in `Waiting` state | Shows reason + message |
| Container in `Terminated` state | Shows reason + exit code |
| Multiple containers per pod | All containers iterated and printed |
| Missing last termination info | Section omitted silently |
| kubeconfig not found | `ERROR:` message + exit 1 |
| Cluster not reachable | `ERROR:` message + exit 1 |
| API forbidden / namespace not found | `ERROR:` message + exit 1 |

### Test pod

The `test-nginx` pod is left running in the `default` namespace as a harmless test workload (nginx:1.27-alpine). To remove it:

```bash
kubectl delete pod test-nginx -n default
```

---

## Dependencies

| Package | Version |
|---------|---------|
| `k8s.io/client-go` | v0.30.2 |
| `k8s.io/apimachinery` | v0.30.2 |
| `k8s.io/api` | v0.30.2 |

```bash
go mod tidy
```
