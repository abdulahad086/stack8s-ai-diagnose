package provider

import (
	"context"
	"fmt"
	"strings"
)

// ToolCall represents a tool call requested by Needle 3.
type ToolCall struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// ToolResult holds the structured evidence output returned from executing a ToolCall.
type ToolResult struct {
	Tool      string         `json:"tool"`
	Arguments map[string]any `json:"arguments"`
	Result    any            `json:"result,omitempty"`
	Error     string         `json:"error,omitempty"`
}

// ValidTools lists all 9 supported diagnostic tools.
var ValidTools = map[string][]string{
	"get_pod_status":           {"namespace", "pod"},
	"get_pod_events":           {"namespace", "pod"},
	"get_container_logs":       {"namespace", "pod", "container"},
	"get_node_status":          {"node_name"},
	"get_gpu_status":           {"node_name"},
	"get_service_status":       {"namespace"},
	"get_storage_status":       {"namespace"},
	"get_workload_status":      {"namespace"},
	"get_configuration_status": {"namespace", "pod"},
}

// ValidateToolCall checks that the tool exists, required arguments are present and non-empty.
func ValidateToolCall(call ToolCall) error {
	requiredArgs, ok := ValidTools[call.Name]
	if !ok {
		return fmt.Errorf("unknown tool %q: execution rejected", call.Name)
	}

	if call.Arguments == nil {
		if len(requiredArgs) > 0 {
			return fmt.Errorf("tool %q missing arguments map", call.Name)
		}
		return nil
	}

	for _, argName := range requiredArgs {
		val, exists := call.Arguments[argName]
		if !exists || val == nil {
			return fmt.Errorf("tool %q missing required argument %q", call.Name, argName)
		}
		strVal, ok := val.(string)
		if ok && strings.TrimSpace(strVal) == "" {
			return fmt.Errorf("tool %q required argument %q is empty", call.Name, argName)
		}
	}

	return nil
}

// ToolDispatcher receives tool name & arguments and routes them to InfrastructureProvider methods.
type ToolDispatcher struct {
	provider InfrastructureProvider
}

// NewToolDispatcher initializes a ToolDispatcher with a Provider.
func NewToolDispatcher(prov InfrastructureProvider) *ToolDispatcher {
	return &ToolDispatcher{provider: prov}
}

// Execute validates and runs a single tool call against the Provider.
func (d *ToolDispatcher) Execute(ctx context.Context, call ToolCall) ToolResult {
	res := ToolResult{
		Tool:      call.Name,
		Arguments: call.Arguments,
	}

	if err := ValidateToolCall(call); err != nil {
		res.Error = err.Error()
		return res
	}

	out, err := d.dispatch(ctx, call)
	if err != nil {
		res.Error = err.Error()
		return res
	}

	res.Result = out
	return res
}

func (d *ToolDispatcher) dispatch(ctx context.Context, call ToolCall) (any, error) {
	if d.provider == nil {
		return nil, &ProviderError{
			Code:    ErrCodeUnavailable,
			Message: "provider is nil",
		}
	}

	getStr := func(k string) string {
		if v, ok := call.Arguments[k].(string); ok {
			return strings.TrimSpace(v)
		}
		return ""
	}

	getInt64 := func(k string, def int64) int64 {
		if v, ok := call.Arguments[k].(float64); ok {
			return int64(v)
		}
		if v, ok := call.Arguments[k].(int); ok {
			return int64(v)
		}
		if v, ok := call.Arguments[k].(int64); ok {
			return v
		}
		return def
	}

	switch call.Name {
	case "get_pod_status":
		ns := getStr("namespace")
		podName := getStr("pod")
		pods, err := d.provider.GetPods(ctx, ns)
		if err != nil {
			return nil, err
		}
		for _, p := range pods {
			if p.PodName == podName {
				return p, nil
			}
		}
		return nil, &ProviderError{
			Code:    ErrCodeResourceNotFound,
			Message: fmt.Sprintf("pod %q not found in namespace %q", podName, ns),
		}

	case "get_pod_events":
		ns := getStr("namespace")
		podName := getStr("pod")
		return d.provider.GetPodEvents(ctx, ns, podName)

	case "get_container_logs":
		ns := getStr("namespace")
		podName := getStr("pod")
		container := getStr("container")
		if container == "" {
			container = podName
		}
		tail := getInt64("tail_lines", 100)
		return d.provider.GetContainerLogs(ctx, ns, podName, container, tail)

	case "get_node_status":
		nodeName := getStr("node_name")
		return d.provider.GetNode(ctx, nodeName)

	case "get_gpu_status":
		nodeName := getStr("node_name")
		return d.provider.GetNodeGPUResources(ctx, nodeName)

	case "get_service_status":
		ns := getStr("namespace")
		svcName := getStr("service_name")
		if svcName != "" {
			return d.provider.GetService(ctx, ns, svcName)
		}
		return d.provider.GetServices(ctx, ns)

	case "get_storage_status":
		ns := getStr("namespace")
		pvcName := getStr("pvc_name")
		podName := getStr("pod")
		if pvcName != "" {
			return d.provider.GetPersistentVolumeClaim(ctx, ns, pvcName)
		}
		if podName != "" {
			pods, err := d.provider.GetPods(ctx, ns)
			if err == nil {
				for _, p := range pods {
					if p.PodName == podName {
						var claims []PersistentVolumeClaimInfo
						for _, v := range p.Volumes {
							if v.Type == "persistentVolumeClaim" && v.ClaimName != "" {
								if pvc, err := d.provider.GetPersistentVolumeClaim(ctx, ns, v.ClaimName); err == nil {
									claims = append(claims, pvc)
								}
							}
						}
						return map[string]any{
							"pod_name": podName,
							"claims":   claims,
						}, nil
					}
				}
			}
		}
		scs, _ := d.provider.GetStorageClasses(ctx)
		return map[string]any{
			"namespace":       ns,
			"storage_classes": scs,
		}, nil

	case "get_workload_status":
		ns := getStr("namespace")
		kind := strings.ToLower(getStr("workload_kind"))
		name := getStr("workload_name")
		podName := getStr("pod")

		if kind != "" && name != "" {
			switch kind {
			case "deployment":
				return d.provider.GetDeployment(ctx, ns, name)
			case "replicaset":
				return d.provider.GetReplicaSet(ctx, ns, name)
			case "statefulset":
				return d.provider.GetStatefulSet(ctx, ns, name)
			case "daemonset":
				return d.provider.GetDaemonSet(ctx, ns, name)
			case "job":
				return d.provider.GetJob(ctx, ns, name)
			case "cronjob":
				return d.provider.GetCronJob(ctx, ns, name)
			}
		}

		if podName != "" {
			pods, err := d.provider.GetPods(ctx, ns)
			if err == nil {
				for _, p := range pods {
					if p.PodName == podName {
						return map[string]any{
							"pod_name":         podName,
							"owner_references": p.OwnerReferences,
						}, nil
					}
				}
			}
		}
		return nil, &ProviderError{
			Code:    ErrCodeResourceNotFound,
			Message: fmt.Sprintf("workload not specified or pod %q not found in namespace %q", podName, ns),
		}

	case "get_configuration_status":
		ns := getStr("namespace")
		podName := getStr("pod")
		pods, err := d.provider.GetPods(ctx, ns)
		if err != nil {
			return nil, err
		}

		var targetPod *PodInfo
		for _, p := range pods {
			if p.PodName == podName {
				targetPod = &p
				break
			}
		}
		if targetPod == nil {
			return nil, &ProviderError{
				Code:    ErrCodeResourceNotFound,
				Message: fmt.Sprintf("pod %q not found in namespace %q", podName, ns),
			}
		}

		configMaps := make([]ConfigMapInfo, 0)
		secrets := make([]SecretInfo, 0)
		for _, ref := range targetPod.EnvRefs {
			if strings.HasPrefix(ref.SourceType, "configMap") {
				if cm, err := d.provider.GetConfigMap(ctx, ns, ref.ResourceName); err == nil {
					configMaps = append(configMaps, cm)
				}
			} else if strings.HasPrefix(ref.SourceType, "secret") {
				if sec, err := d.provider.GetSecretMetadata(ctx, ns, ref.ResourceName); err == nil {
					secrets = append(secrets, sec)
				}
			}
		}
		for _, ref := range targetPod.ConfigVolumeRefs {
			if ref.SourceType == "configMap" {
				if cm, err := d.provider.GetConfigMap(ctx, ns, ref.ResourceName); err == nil {
					configMaps = append(configMaps, cm)
				}
			} else if ref.SourceType == "secret" {
				if sec, err := d.provider.GetSecretMetadata(ctx, ns, ref.ResourceName); err == nil {
					secrets = append(secrets, sec)
				}
			}
		}

		var saInfo *ServiceAccountInfo
		if targetPod.ServiceAccountName != "" {
			if sa, err := d.provider.GetServiceAccount(ctx, ns, targetPod.ServiceAccountName); err == nil {
				saInfo = &sa
			}
		}

		return map[string]any{
			"pod_name":             podName,
			"service_account":      saInfo,
			"config_maps":          configMaps,
			"secrets":              secrets,
			"image_pull_secrets":   targetPod.ImagePullSecretNames,
		}, nil

	default:
		return nil, fmt.Errorf("unhandled tool: %s", call.Name)
	}
}
