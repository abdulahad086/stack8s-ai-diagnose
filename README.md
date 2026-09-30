# stack8s-ai-diagnose

A Go prototype that connects to a real Kubernetes cluster and collects diagnostic information — nodes, pod status, events, and container logs — using only the official `client-go` library. No AI, no mock data, no Stack8s integration.

---

## Feature 1 — Kubernetes Connectivity

### What it does
Verifies that the Go program can connect to a real Kubernetes cluster and list all cluster nodes with their Ready status.

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
# or
go build -o k8s-check . && ./k8s-check
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
Retrieves and displays the real status of all pods in the `default` namespace.

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
No Secret values, environment variables, passwords, tokens, certificates, or kubeconfig credentials are printed.

### Namespace
`default` — hardcoded for the MVP.

---

## Feature 3 — Kubernetes Events Collector

### What Kubernetes Events are
Kubernetes Events are structured records written by the control plane and kubelet when something significant happens to a workload — scheduling, image pulling, container starting, OOMKills, back-offs, etc.

### Why Events are useful for diagnosis
- Show the exact sequence of actions Kubernetes took for a pod.
- Surface warnings (`BackOff`, `Failed`, `OOMKilled`) before a pod enters a crash loop.
- Provide human-readable messages with precise timing.

### Fields collected per event

| # | Field | Source |
|---|-------|--------|
| 1 | Reason | `event.Reason` |
| 2 | Type | `event.Type` |
| 3 | Message | `event.Message` |
| 4 | Involved object kind | `event.InvolvedObject.Kind` |
| 5 | Involved object name | `event.InvolvedObject.Name` |
| 6 | Namespace | `event.Namespace` |
| 7 | First observed | `event.FirstTimestamp` |
| 8 | Last observed | `event.LastTimestamp` |
| 9 | Count | `event.Count` |
| 10 | Source component | `event.Source.Component` |

### How events are filtered
Field selector: `involvedObject.name=<podName>,involvedObject.namespace=<ns>,involvedObject.kind=Pod` — same strategy as `kubectl describe pod`.

### How to verify with kubectl
```bash
kubectl get events -n default --field-selector involvedObject.name=<pod-name> --sort-by='.lastTimestamp'
kubectl describe pod <pod-name> -n default
```

---

## Feature 4 — Container Logs Collector

### What container logs are
Container logs are the raw stdout/stderr output produced by the application running inside a Kubernetes container. They are the primary source of application-level diagnostic information — startup errors, connection failures, authentication errors, crashes, configuration problems, and exceptions.

### Why logs are useful for diagnosis
- Reveal the exact error message an application printed before crashing.
- Show configuration issues that Kubernetes itself cannot see.
- Provide application-level context that supplements Kubernetes Events.
- Essential for diagnosing `CrashLoopBackOff`, `OOMKilled`, and silent failures.

### How logs are retrieved
Uses the Kubernetes Logs API via `client-go`:
```go
clientset.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{
    Container: containerName,
    TailLines: &tailLines,   // bounded — never streams indefinitely
}).Stream(context.TODO())
```
This is a **snapshot collector**, not a live log tailer. Logs are read once and the stream is closed immediately.

### Current line limit
**100 lines** (most recent). Defined by the `defaultLogTailLines` constant. Never requests unbounded log history.

### Namespace scope
`default` — matches Feature 2 and Feature 3.

### Container selection behaviour

| Scenario | Behaviour |
|----------|-----------|
| Single-container pod | Container selected automatically |
| Multi-container pod | Logs collected for **each** container in `pod.Spec.Containers` order |
| Container not yet started (`Waiting`) | Prints `No logs available for container: <name> (container not yet started)` — non-fatal |
| Container name does not exist | Kubernetes API returns error → printed as `WARNING` — non-fatal |
| Empty log output | Prints `No logs available for container: <name>` — non-fatal |
| Log stream read error | Printed as `WARNING` — non-fatal |

### Security considerations
- **No Secret values are collected or printed.**
- No credentials, tokens, certificates, or kubeconfig contents are printed.
- Log content is passed through from the Kubernetes API as-is. Application logs may contain sensitive data — treat them accordingly in production use.
- Logs are bounded to 100 lines per container. No unbounded retrieval.
- No log persistence is implemented. Logs are only printed during the diagnostic run.

### How to verify with kubectl
```bash
# Last 100 lines for a specific container
kubectl logs <pod-name> -n default -c <container-name> --tail=100

# All logs for a single-container pod
kubectl logs <pod-name> -n default
```

### Actual test results (Feature 4)

#### Test A — `test-logger` (busybox:1.36, `Completed` pod)

Workload:
```yaml
name: test-logger
container: logger
image: busybox:1.36
command: echo STACK8S_TEST_LOG_1 && ... && echo 'feature4 log collector verified'
restartPolicy: Never
```

**kubectl reference:**
```
STACK8S_TEST_LOG_1
STACK8S_TEST_LOG_2
STACK8S_TEST_LOG_3
STACK8S_TEST_LOG_4
STACK8S_TEST_LOG_5
feature4 log collector verified
```

**Go program output:**
```
── Container Logs: test-logger / logger (last 100 lines) ─────────────
Lines retrieved: 6

│ STACK8S_TEST_LOG_1
│ STACK8S_TEST_LOG_2
│ STACK8S_TEST_LOG_3
│ STACK8S_TEST_LOG_4
│ STACK8S_TEST_LOG_5
│ feature4 log collector verified
```
All 6 lines match exactly. ✅

#### Test B — `test-nginx` (nginx:1.27-alpine, `Running` pod)

**kubectl reference:**
```
/docker-entrypoint.sh: /docker-entrypoint.d/ is not empty, will attempt to perform configuration
...
2026/09/30 06:58:56 [notice] 1#1: nginx/1.27.5
2026/09/30 06:58:56 [notice] 1#1: start worker processes
2026/09/30 06:58:56 [notice] 1#1: start worker process 30
...
```

**Go program output:**
```
── Container Logs: test-nginx / nginx (last 100 lines) ─────────────
Lines retrieved: 19

│ /docker-entrypoint.sh: /docker-entrypoint.d/ is not empty, will attempt to perform configuration
│ /docker-entrypoint.sh: Looking for shell scripts in /docker-entrypoint.d/
│ ...
│ 2026/09/30 06:58:56 [notice] 1#1: nginx/1.27.5
│ 2026/09/30 06:58:56 [notice] 1#1: start worker processes
│ 2026/09/30 06:58:56 [notice] 1#1: start worker process 30
│ 2026/09/30 06:58:56 [notice] 1#1: start worker process 31
│ 2026/09/30 06:58:56 [notice] 1#1: start worker process 32
│ 2026/09/30 06:58:56 [notice] 1#1: start worker process 33
```
19 lines retrieved, content matches kubectl. ✅

### Limitations
- Only `default` namespace is queried (MVP scope).
- Log line limit is fixed at 100 per container (configurable by changing `defaultLogTailLines`).
- No log persistence — diagnostic run only.
- If events have expired from Kubernetes TTL, they will not appear (Kubernetes behaviour, not a collector bug).

### Test pods

Both test pods are left in the `default` namespace. To clean up:
```bash
kubectl delete pod test-nginx test-logger -n default
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
