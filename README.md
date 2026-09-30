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

Retrieves and displays the real status of all pods in the `default` namespace of the live Kubernetes cluster.

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

### Namespace

`default` — hardcoded for the MVP.

### Edge cases handled

| Scenario | Handling |
|----------|----------|
| No pods in namespace | Prints `No pods found in namespace: default` |
| Pod not yet scheduled | Shows `<not yet scheduled>` for node name |
| Pod pending, no container status | Shows spec image, `Ready: false`, `State: Pending` |
| Container Running | Shows start time |
| Container Waiting | Shows reason + message |
| Container Terminated | Shows reason + exit code |
| Multiple containers per pod | All containers iterated and printed |
| Missing last termination info | Section omitted silently |

---

## Feature 3 — Kubernetes Events Collector

### What Kubernetes Events are

Kubernetes Events are structured records that the control plane and kubelet write when something significant happens to a workload — scheduling, image pulling, container starting, OOMKills, back-offs, etc. They are the first place to look when diagnosing why a pod is not working correctly.

### Why Events are useful for diagnosis

- They show the exact sequence of actions Kubernetes took for a pod.
- They surface warnings (e.g. `BackOff`, `Failed`, `OOMKilled`) before a pod enters a crash loop.
- They provide human-readable messages with timing — when the scheduler assigned the pod, when the image was pulled, when the container started.

### Fields collected per event

| # | Field | Source |
|---|-------|--------|
| 1 | Event reason | `event.Reason` |
| 2 | Event type | `event.Type` (`Normal` / `Warning`) |
| 3 | Event message | `event.Message` |
| 4 | Involved object kind | `event.InvolvedObject.Kind` |
| 5 | Involved object name | `event.InvolvedObject.Name` |
| 6 | Namespace | `event.Namespace` |
| 7 | First observed timestamp | `event.FirstTimestamp` |
| 8 | Last observed timestamp | `event.LastTimestamp` |
| 9 | Count | `event.Count` |
| 10 | Source component | `event.Source.Component` |

### How events are filtered

Events are fetched using a Kubernetes API field selector:

```
involvedObject.name=<podName>,involvedObject.namespace=<namespace>,involvedObject.kind=Pod
```

This is the same strategy `kubectl describe pod` uses internally. Only events directly associated with the specific pod are returned — unrelated cluster events are not included.

Events are sorted **chronologically** (oldest → newest) using `LastTimestamp`, falling back to `FirstTimestamp`.

### Security note

- **No Secret values are collected or printed.**
- No credentials, tokens, or kubeconfig contents are printed.
- Event messages from Kubernetes are passed through as-is (they never contain Secret data).

### How to run

```bash
go run main.go
```

All three features (nodes, pod status, events) run in a single invocation.

### How to verify events with kubectl

```bash
# Quick view
kubectl get events -n default --field-selector involvedObject.name=<pod-name> --sort-by='.lastTimestamp'

# Full detail
kubectl describe pod <pod-name> -n default
```

### Actual test result (Feature 3)

Pod tested: **`test-nginx`** (`nginx:1.27-alpine`) in `default` namespace.

**Go program output:**
```
── Kubernetes Events for pod: test-nginx ────────────────────────

    Event 1
      Type      : Normal
      Reason    : Scheduled
      Message   : Successfully assigned default/test-nginx to docker-desktop
      Object    : Pod/test-nginx
      Source    : default-scheduler
      Count     : 1
      First Seen: 2026-09-30 11:58:50 UTC
      Last Seen : 2026-09-30 11:58:50 UTC

    Event 2
      Type      : Normal
      Reason    : Pulling
      Message   : Pulling image "nginx:1.27-alpine"
      Object    : Pod/test-nginx
      Source    : kubelet
      Count     : 1
      First Seen: 2026-09-30 11:58:53 UTC
      Last Seen : 2026-09-30 11:58:53 UTC

    Event 3
      Type      : Normal
      Reason    : Pulled
      Message   : Successfully pulled image "nginx:1.27-alpine" in 2.327s (2.328s including waiting). Image size: 20984244 bytes.
      Object    : Pod/test-nginx
      Source    : kubelet
      Count     : 1
      First Seen: 2026-09-30 11:58:55 UTC
      Last Seen : 2026-09-30 11:58:55 UTC

    Event 4
      Type      : Normal
      Reason    : Created
      Message   : Created container nginx
      Object    : Pod/test-nginx
      Source    : kubelet
      Count     : 1
      First Seen: 2026-09-30 11:58:55 UTC
      Last Seen : 2026-09-30 11:58:55 UTC

    Event 5
      Type      : Normal
      Reason    : Started
      Message   : Started container nginx
      Object    : Pod/test-nginx
      Source    : kubelet
      Count     : 1
      First Seen: 2026-09-30 11:58:56 UTC
      Last Seen : 2026-09-30 11:58:56 UTC
```

**kubectl reference output (verified match):**
```
LAST SEEN   TYPE     REASON      OBJECT           MESSAGE
8m46s       Normal   Scheduled   pod/test-nginx   Successfully assigned default/test-nginx to docker-desktop
8m43s       Normal   Pulling     pod/test-nginx   Pulling image "nginx:1.27-alpine"
8m41s       Normal   Pulled      pod/test-nginx   Successfully pulled image "nginx:1.27-alpine" in 2.327s ...
8m41s       Normal   Created     pod/test-nginx   Created container nginx
8m40s       Normal   Started     pod/test-nginx   Started container nginx
```

All 5 events matched. ✅

### Test pod

`test-nginx` (`nginx:1.27-alpine`) is left running in the `default` namespace as a harmless test workload. To remove it:

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
