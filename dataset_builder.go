package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ── Needle Fine-Tuning Schema Types ──────────────────────────────────────────

type ToolCallAnswer struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type NeedleExample struct {
	Query     string           `json:"query"`
	Tools     []any            `json:"tools"`
	Answers   []ToolCallAnswer `json:"answers"`
	Reasoning string           `json:"reasoning,omitempty"`

	// Internal metadata (not written to official Needle JSONL output)
	CaseID        string `json:"-"`
	CaseType      string `json:"-"` // "REAL_VERIFIED" | "SYNTHETIC_GROUNDED"
	DiagnosisCode string `json:"-"` // "IMAGE_PULL_FAILURE" | "OOM_KILLED" | "UNKNOWN" | "NONE"
}

// ── Case Definition ──────────────────────────────────────────────────────────

type CaseDefinition struct {
	CaseID        string
	CaseType      string // "REAL_VERIFIED" | "SYNTHETIC_GROUNDED"
	DiagnosisCode string
	Split         string // "train" | "validation" | "test"
	Query         string
	Answers       []ToolCallAnswer
	Reasoning     string
}

func getToolsSchemaJSON() []any {
	toolsBytes, err := os.ReadFile("dataset/tools.json")
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: failed to read dataset/tools.json: %v\n", err)
		os.Exit(1)
	}
	var tools []any
	if err := json.Unmarshal(toolsBytes, &tools); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: failed to parse dataset/tools.json: %v\n", err)
		os.Exit(1)
	}
	return tools
}

// ── Master List of Dataset Cases ─────────────────────────────────────────────

func buildMasterCases() []CaseDefinition {
	return []CaseDefinition{
		// ═════════════════════════════════════════════════════════════════════
		// TRAIN SET (Cases 1 - 25) ~70%
		// ═════════════════════════════════════════════════════════════════════

		// ── Real Verified Cases (from F6 / F7 local cluster) ─────────────────
		{
			CaseID:        "CASE-REAL-001",
			CaseType:      "REAL_VERIFIED",
			DiagnosisCode: "IMAGE_PULL_FAILURE",
			Split:         "train",
			Query:         "Why is pod test-image-failure failing in namespace default?",
			Answers: []ToolCallAnswer{
				{
					Name: "get_pod_status",
					Arguments: map[string]any{
						"namespace": "default",
						"pod":       "test-image-failure",
					},
				},
				{
					Name: "get_pod_events",
					Arguments: map[string]any{
						"namespace": "default",
						"pod":       "test-image-failure",
					},
				},
				{
					Name: "get_container_logs",
					Arguments: map[string]any{
						"container": "app",
						"namespace": "default",
						"pod":       "test-image-failure",
					},
				},
			},
			Reasoning: "The pod name 'test-image-failure', namespace 'default', and container 'app' are explicitly specified in the query and workload spec.",
		},
		{
			CaseID:        "CASE-REAL-002",
			CaseType:      "REAL_VERIFIED",
			DiagnosisCode: "OOM_KILLED",
			Split:         "train",
			Query:         "Investigate failure for pod test-oom-failure in default namespace.",
			Answers: []ToolCallAnswer{
				{
					Name: "get_pod_status",
					Arguments: map[string]any{
						"namespace": "default",
						"pod":       "test-oom-failure",
					},
				},
				{
					Name: "get_pod_events",
					Arguments: map[string]any{
						"namespace": "default",
						"pod":       "test-oom-failure",
					},
				},
			},
			Reasoning: "The pod name 'test-oom-failure' and namespace 'default' come directly from the user request.",
		},
		{
			CaseID:        "CASE-REAL-003",
			CaseType:      "REAL_VERIFIED",
			DiagnosisCode: "UNKNOWN",
			Split:         "train",
			Query:         "Diagnose completed pod test-logger in default namespace.",
			Answers: []ToolCallAnswer{
				{
					Name: "get_pod_status",
					Arguments: map[string]any{
						"namespace": "default",
						"pod":       "test-logger",
					},
				},
				{
					Name: "get_container_logs",
					Arguments: map[string]any{
						"container": "logger",
						"namespace": "default",
						"pod":       "test-logger",
					},
				},
			},
			Reasoning: "The target pod 'test-logger' and container 'logger' are provided in the query context.",
		},

		// ── Synthetic Grounded Diagnostic Cases (Train) ──────────────────────
		{
			CaseID:        "CASE-SYN-001",
			CaseType:      "SYNTHETIC_GROUNDED",
			DiagnosisCode: "IMAGE_PULL_FAILURE",
			Split:         "train",
			Query:         "Troubleshoot image pull issue on pod auth-service-pod in namespace prod.",
			Answers: []ToolCallAnswer{
				{
					Name: "get_pod_status",
					Arguments: map[string]any{
						"namespace": "prod",
						"pod":       "auth-service-pod",
					},
				},
				{
					Name: "get_pod_events",
					Arguments: map[string]any{
						"namespace": "prod",
						"pod":       "auth-service-pod",
					},
				},
			},
			Reasoning: "The pod name 'auth-service-pod' and namespace 'prod' are grounded in the user's inquiry.",
		},
		{
			CaseID:        "CASE-SYN-002",
			CaseType:      "SYNTHETIC_GROUNDED",
			DiagnosisCode: "OOM_KILLED",
			Split:         "train",
			Query:         "Pod cache-worker-0 in namespace redis terminated unexpectedly. Check memory limits.",
			Answers: []ToolCallAnswer{
				{
					Name: "get_pod_status",
					Arguments: map[string]any{
						"namespace": "redis",
						"pod":       "cache-worker-0",
					},
				},
				{
					Name: "get_pod_events",
					Arguments: map[string]any{
						"namespace": "redis",
						"pod":       "cache-worker-0",
					},
				},
			},
			Reasoning: "The namespace 'redis' and pod 'cache-worker-0' are derived from the explicit query arguments.",
		},
		{
			CaseID:        "CASE-SYN-003",
			CaseType:      "SYNTHETIC_GROUNDED",
			DiagnosisCode: "UNKNOWN",
			Split:         "train",
			Query:         "Pod order-processor in namespace checkout is restarting repeatedly (CrashLoopBackOff). Find root cause.",
			Answers: []ToolCallAnswer{
				{
					Name: "get_pod_status",
					Arguments: map[string]any{
						"namespace": "checkout",
						"pod":       "order-processor",
					},
				},
				{
					Name: "get_pod_events",
					Arguments: map[string]any{
						"namespace": "checkout",
						"pod":       "order-processor",
					},
				},
				{
					Name: "get_container_logs",
					Arguments: map[string]any{
						"container": "processor",
						"namespace": "checkout",
						"pod":       "order-processor",
					},
				},
			},
			Reasoning: "The pod 'order-processor', namespace 'checkout', and container 'processor' are grounded in the prompt context.",
		},

		// ── Selective Tool Use Cases (Train) ─────────────────────────────────
		{
			CaseID:        "CASE-SYN-004",
			CaseType:      "SYNTHETIC_GROUNDED",
			DiagnosisCode: "NONE",
			Split:         "train",
			Query:         "Show status of pod web-gateway in namespace ingress.",
			Answers: []ToolCallAnswer{
				{
					Name: "get_pod_status",
					Arguments: map[string]any{
						"namespace": "ingress",
						"pod":       "web-gateway",
					},
				},
			},
			Reasoning: "User requested status specifically, calling get_pod_status with grounded parameters.",
		},
		{
			CaseID:        "CASE-SYN-005",
			CaseType:      "SYNTHETIC_GROUNDED",
			DiagnosisCode: "NONE",
			Split:         "train",
			Query:         "Fetch events for pod event-dispatcher in namespace messaging.",
			Answers: []ToolCallAnswer{
				{
					Name: "get_pod_events",
					Arguments: map[string]any{
						"namespace": "messaging",
						"pod":       "event-dispatcher",
					},
				},
			},
			Reasoning: "User requested events specifically, invoking get_pod_events for messaging/event-dispatcher.",
		},
		{
			CaseID:        "CASE-SYN-006",
			CaseType:      "SYNTHETIC_GROUNDED",
			DiagnosisCode: "NONE",
			Split:         "train",
			Query:         "Get last 100 log lines from container server in pod api-server namespace default.",
			Answers: []ToolCallAnswer{
				{
					Name: "get_container_logs",
					Arguments: map[string]any{
						"container": "server",
						"namespace": "default",
						"pod":       "api-server",
					},
				},
			},
			Reasoning: "User asked for logs directly. Tool get_container_logs called with server, default, api-server.",
		},

		// ── Negative / Non-Diagnostic Cases (Train) ─────────────────────────
		{
			CaseID:        "CASE-SYN-007",
			CaseType:      "SYNTHETIC_GROUNDED",
			DiagnosisCode: "NONE",
			Split:         "train",
			Query:         "What is the difference between a Kubernetes Pod and a Deployment?",
			Answers:       []ToolCallAnswer{},
			Reasoning:     "General conceptual question; no diagnostic tool execution is appropriate.",
		},
		{
			CaseID:        "CASE-SYN-008",
			CaseType:      "SYNTHETIC_GROUNDED",
			DiagnosisCode: "NONE",
			Split:         "train",
			Query:         "Hello! How can you help me with my Kubernetes cluster?",
			Answers:       []ToolCallAnswer{},
			Reasoning:     "Greeting; no cluster diagnostic tool needed.",
		},
		{
			CaseID:        "CASE-SYN-009",
			CaseType:      "SYNTHETIC_GROUNDED",
			DiagnosisCode: "NONE",
			Split:         "train",
			Query:         "Explain how memory limits work in cgroups v2.",
			Answers:       []ToolCallAnswer{},
			Reasoning:     "Informational request; no tool calls required.",
		},

		// ── Additional Diverse Grounded Diagnostic Cases (Train) ─────────────
		{
			CaseID:        "CASE-SYN-010",
			CaseType:      "SYNTHETIC_GROUNDED",
			DiagnosisCode: "IMAGE_PULL_FAILURE",
			Split:         "train",
			Query:         "Pod image-worker-alpha in namespace batch failed with ErrImagePull. Investigate events.",
			Answers: []ToolCallAnswer{
				{
					Name: "get_pod_status",
					Arguments: map[string]any{
						"namespace": "batch",
						"pod":       "image-worker-alpha",
					},
				},
				{
					Name: "get_pod_events",
					Arguments: map[string]any{
						"namespace": "batch",
						"pod":       "image-worker-alpha",
					},
				},
			},
			Reasoning: "Target image-worker-alpha in batch namespace extracted directly from query.",
		},
		{
			CaseID:        "CASE-SYN-011",
			CaseType:      "SYNTHETIC_GROUNDED",
			DiagnosisCode: "OOM_KILLED",
			Split:         "train",
			Query:         "Container memory-crunch in pod analytics-node-1 namespace data exit code 137. Diagnose failure.",
			Answers: []ToolCallAnswer{
				{
					Name: "get_pod_status",
					Arguments: map[string]any{
						"namespace": "data",
						"pod":       "analytics-node-1",
					},
				},
				{
					Name: "get_pod_events",
					Arguments: map[string]any{
						"namespace": "data",
						"pod":       "analytics-node-1",
					},
				},
			},
			Reasoning: "Grounded parameters: namespace 'data', pod 'analytics-node-1'.",
		},
		{
			CaseID:        "CASE-SYN-012",
			CaseType:      "SYNTHETIC_GROUNDED",
			DiagnosisCode: "UNKNOWN",
			Split:         "train",
			Query:         "Pod background-sync in namespace system is stuck in Pending. Determine why.",
			Answers: []ToolCallAnswer{
				{
					Name: "get_pod_status",
					Arguments: map[string]any{
						"namespace": "system",
						"pod":       "background-sync",
					},
				},
				{
					Name: "get_pod_events",
					Arguments: map[string]any{
						"namespace": "system",
						"pod":       "background-sync",
					},
				},
			},
			Reasoning: "Grounded arguments system and background-sync.",
		},
		{
			CaseID:        "CASE-SYN-013",
			CaseType:      "SYNTHETIC_GROUNDED",
			DiagnosisCode: "NODE_NOT_READY",
			Split:         "train",
			Query:         "Pod worker-node-app in namespace kube-system is evicted. Check cluster events.",
			Answers: []ToolCallAnswer{
				{
					Name: "get_pod_status",
					Arguments: map[string]any{
						"namespace": "kube-system",
						"pod":       "worker-node-app",
					},
				},
				{
					Name: "get_pod_events",
					Arguments: map[string]any{
						"namespace": "kube-system",
						"pod":       "worker-node-app",
					},
				},
			},
			Reasoning: "Grounded parameters kube-system and worker-node-app.",
		},
		{
			CaseID:        "CASE-SYN-014",
			CaseType:      "SYNTHETIC_GROUNDED",
			DiagnosisCode: "NONE",
			Split:         "train",
			Query:         "Fetch status of pod database-primary in namespace db.",
			Answers: []ToolCallAnswer{
				{
					Name: "get_pod_status",
					Arguments: map[string]any{
						"namespace": "db",
						"pod":       "database-primary",
					},
				},
			},
			Reasoning: "Single tool call get_pod_status grounded in query context.",
		},

		// ═════════════════════════════════════════════════════════════════════
		// VALIDATION SET (Cases 26 - 32) ~15%
		// ═════════════════════════════════════════════════════════════════════
		{
			CaseID:        "CASE-VAL-001",
			CaseType:      "REAL_VERIFIED",
			DiagnosisCode: "UNKNOWN",
			Split:         "validation",
			Query:         "Verify healthy pod test-nginx in default namespace.",
			Answers: []ToolCallAnswer{
				{
					Name: "get_pod_status",
					Arguments: map[string]any{
						"namespace": "default",
						"pod":       "test-nginx",
					},
				},
				{
					Name: "get_container_logs",
					Arguments: map[string]any{
						"container": "nginx",
						"namespace": "default",
						"pod":       "test-nginx",
					},
				},
			},
			Reasoning: "Real verified pod test-nginx in default namespace.",
		},
		{
			CaseID:        "CASE-VAL-002",
			CaseType:      "SYNTHETIC_GROUNDED",
			DiagnosisCode: "IMAGE_PULL_FAILURE",
			Split:         "validation",
			Query:         "Frontend pod web-frontend-v2 in namespace staging fails to start image pull. Check why.",
			Answers: []ToolCallAnswer{
				{
					Name: "get_pod_status",
					Arguments: map[string]any{
						"namespace": "staging",
						"pod":       "web-frontend-v2",
					},
				},
				{
					Name: "get_pod_events",
					Arguments: map[string]any{
						"namespace": "staging",
						"pod":       "web-frontend-v2",
					},
				},
			},
			Reasoning: "Validation case for web-frontend-v2 in staging.",
		},
		{
			CaseID:        "CASE-VAL-003",
			CaseType:      "SYNTHETIC_GROUNDED",
			DiagnosisCode: "OOM_KILLED",
			Split:         "validation",
			Query:         "Data processing pod stream-processor-5 in namespace analytics killed by OOM. Collect status and events.",
			Answers: []ToolCallAnswer{
				{
					Name: "get_pod_status",
					Arguments: map[string]any{
						"namespace": "analytics",
						"pod":       "stream-processor-5",
					},
				},
				{
					Name: "get_pod_events",
					Arguments: map[string]any{
						"namespace": "analytics",
						"pod":       "stream-processor-5",
					},
				},
			},
			Reasoning: "Validation case stream-processor-5 in analytics.",
		},
		{
			CaseID:        "CASE-VAL-004",
			CaseType:      "SYNTHETIC_GROUNDED",
			DiagnosisCode: "UNKNOWN",
			Split:         "validation",
			Query:         "Pod legacy-monolith in namespace ops is failing. Investigate root cause.",
			Answers: []ToolCallAnswer{
				{
					Name: "get_pod_status",
					Arguments: map[string]any{
						"namespace": "ops",
						"pod":       "legacy-monolith",
					},
				},
				{
					Name: "get_pod_events",
					Arguments: map[string]any{
						"namespace": "ops",
						"pod":       "legacy-monolith",
					},
				},
				{
					Name: "get_container_logs",
					Arguments: map[string]any{
						"container": "app",
						"namespace": "ops",
						"pod":       "legacy-monolith",
					},
				},
			},
			Reasoning: "Validation case legacy-monolith in ops.",
		},
		{
			CaseID:        "CASE-VAL-005",
			CaseType:      "SYNTHETIC_GROUNDED",
			DiagnosisCode: "NONE",
			Split:         "validation",
			Query:         "Can you give me a command to check pod CPU utilization?",
			Answers:       []ToolCallAnswer{},
			Reasoning:     "General shell usage question; no diagnostic tool execution.",
		},

		// ═════════════════════════════════════════════════════════════════════
		// HOLDOUT TEST SET (Cases 33 - 40) ~15% - Zero Data Leakage!
		// ═════════════════════════════════════════════════════════════════════
		{
			CaseID:        "CASE-TEST-001",
			CaseType:      "SYNTHETIC_GROUNDED",
			DiagnosisCode: "IMAGE_PULL_FAILURE",
			Split:         "test",
			Query:         "Microservice invoice-generator in namespace production-eu cannot pull container image. Diagnose error.",
			Answers: []ToolCallAnswer{
				{
					Name: "get_pod_status",
					Arguments: map[string]any{
						"namespace": "production-eu",
						"pod":       "invoice-generator",
					},
				},
				{
					Name: "get_pod_events",
					Arguments: map[string]any{
						"namespace": "production-eu",
						"pod":       "invoice-generator",
					},
				},
			},
			Reasoning: "Holdout test case with unseen pod invoice-generator and namespace production-eu.",
		},
		{
			CaseID:        "CASE-TEST-002",
			CaseType:      "SYNTHETIC_GROUNDED",
			DiagnosisCode: "OOM_KILLED",
			Split:         "test",
			Query:         "Heavy task search-indexer-pod in namespace billing-v2 crashed with exit status 137. Inspect failure.",
			Answers: []ToolCallAnswer{
				{
					Name: "get_pod_status",
					Arguments: map[string]any{
						"namespace": "billing-v2",
						"pod":       "search-indexer-pod",
					},
				},
				{
					Name: "get_pod_events",
					Arguments: map[string]any{
						"namespace": "billing-v2",
						"pod":       "search-indexer-pod",
					},
				},
			},
			Reasoning: "Holdout test case with unseen pod search-indexer-pod and namespace billing-v2.",
		},
		{
			CaseID:        "CASE-TEST-003",
			CaseType:      "SYNTHETIC_GROUNDED",
			DiagnosisCode: "UNKNOWN",
			Split:         "test",
			Query:         "Pod metrics-exporter in namespace monitoring-prod is stuck pending without error details. Investigate.",
			Answers: []ToolCallAnswer{
				{
					Name: "get_pod_status",
					Arguments: map[string]any{
						"namespace": "monitoring-prod",
						"pod":       "metrics-exporter",
					},
				},
				{
					Name: "get_pod_events",
					Arguments: map[string]any{
						"namespace": "monitoring-prod",
						"pod":       "metrics-exporter",
					},
				},
			},
			Reasoning: "Holdout test case with unseen pod metrics-exporter and namespace monitoring-prod.",
		},
		{
			CaseID:        "CASE-TEST-004",
			CaseType:      "SYNTHETIC_GROUNDED",
			DiagnosisCode: "NONE",
			Split:         "test",
			Query:         "Show logs for container sidecar in pod log-collector namespace infrastructure.",
			Answers: []ToolCallAnswer{
				{
					Name: "get_container_logs",
					Arguments: map[string]any{
						"container": "sidecar",
						"namespace": "infrastructure",
						"pod":       "log-collector",
					},
				},
			},
			Reasoning: "Holdout test case for log retrieval on unseen pod log-collector and container sidecar.",
		},
		{
			CaseID:        "CASE-TEST-005",
			CaseType:      "SYNTHETIC_GROUNDED",
			DiagnosisCode: "NONE",
			Split:         "test",
			Query:         "Summarize the best practices for setting Kubernetes memory requests vs limits.",
			Answers:       []ToolCallAnswer{},
			Reasoning:     "Holdout test case for non-diagnostic request; empty answers expected.",
		},
	}
}

// ── Generator & Exporter ─────────────────────────────────────────────────────

func generateDatasets() {
	tools := getToolsSchemaJSON()
	cases := buildMasterCases()

	if err := os.MkdirAll("dataset", 0755); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: failed to create dataset directory: %v\n", err)
		os.Exit(1)
	}

	files := map[string]*os.File{}
	writers := map[string]*bufio.Writer{}

	for _, split := range []string{"train", "validation", "test"} {
		filePath := filepath.Join("dataset", split+".jsonl")
		f, err := os.Create(filePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: failed to create %s: %v\n", filePath, err)
			os.Exit(1)
		}
		files[split] = f
		writers[split] = bufio.NewWriter(f)
	}

	for _, c := range cases {
		rec := NeedleExample{
			Query:     c.Query,
			Tools:     tools,
			Answers:   c.Answers,
			Reasoning: c.Reasoning,
		}

		b, err := json.Marshal(rec)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: failed to marshal example %s: %v\n", c.CaseID, err)
			os.Exit(1)
		}

		w, ok := writers[c.Split]
		if !ok {
			fmt.Fprintf(os.Stderr, "ERROR: unknown split %s for case %s\n", c.Split, c.CaseID)
			os.Exit(1)
		}

		w.WriteString(string(b) + "\n")
	}

	for split, w := range writers {
		w.Flush()
		files[split].Close()
	}
}

// ── Validator & Leakage Checker ──────────────────────────────────────────────

type ToolSchema struct {
	Name       string `json:"name"`
	Parameters struct {
		Properties map[string]any `json:"properties"`
		Required   []string       `json:"required"`
	} `json:"parameters"`
}

func loadToolSchemas() map[string]ToolSchema {
	b, err := os.ReadFile("dataset/tools.json")
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: dataset/tools.json not found: %v\n", err)
		os.Exit(1)
	}
	var list []ToolSchema
	if err := json.Unmarshal(b, &list); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: failed to parse dataset/tools.json: %v\n", err)
		os.Exit(1)
	}
	m := make(map[string]ToolSchema)
	for _, t := range list {
		m[t.Name] = t
	}
	return m
}

func validateDatasets() (int, map[string]int, int, int, int, int) {
	toolSchemas := loadToolSchemas()
	secretRegex := regexp.MustCompile(`(?i)(bearer\s+[A-Za-z0-9\-\._~\+\/]+=*|password\s*=\s*['"][^'"]+['"]|-----BEGIN CERTIFICATE-----|eyJhbGciOi)`)

	totalCount := 0
	splitCounts := map[string]int{"train": 0, "validation": 0, "test": 0}
	emptyAnswersCount := 0
	unknownDiagCount := 0
	reasoningCount := 0
	realCount := 0

	trainPodNames := make(map[string]bool)
	testPodNames := make(map[string]bool)

	masterCases := buildMasterCases()
	caseMap := make(map[string]CaseDefinition)
	for _, c := range masterCases {
		caseMap[c.CaseID] = c
		if c.CaseType == "REAL_VERIFIED" {
			realCount++
		}
		if c.DiagnosisCode == "UNKNOWN" {
			unknownDiagCount++
		}
	}

	for _, split := range []string{"train", "validation", "test"} {
		filePath := filepath.Join("dataset", split+".jsonl")
		f, err := os.Open(filePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: failed to open %s for validation: %v\n", filePath, err)
			os.Exit(1)
		}
		defer f.Close()

		scanner := bufio.NewScanner(f)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			if strings.TrimSpace(line) == "" {
				continue
			}

			// Secret check
			if secretRegex.MatchString(line) {
				fmt.Fprintf(os.Stderr, "VALIDATION ERROR [%s:L%d]: potential secret pattern detected!\n", split, lineNum)
				os.Exit(1)
			}

			var rec struct {
				Query     string           `json:"query"`
				Tools     []any            `json:"tools"`
				Answers   []ToolCallAnswer `json:"answers"`
				Reasoning string           `json:"reasoning"`
			}

			if err := json.Unmarshal([]byte(line), &rec); err != nil {
				fmt.Fprintf(os.Stderr, "VALIDATION ERROR [%s:L%d]: invalid JSON: %v\n", split, lineNum, err)
				os.Exit(1)
			}

			if rec.Query == "" {
				fmt.Fprintf(os.Stderr, "VALIDATION ERROR [%s:L%d]: query field is empty!\n", split, lineNum)
				os.Exit(1)
			}

			if len(rec.Tools) == 0 {
				fmt.Fprintf(os.Stderr, "VALIDATION ERROR [%s:L%d]: tools field is empty!\n", split, lineNum)
				os.Exit(1)
			}

			if rec.Reasoning != "" {
				reasoningCount++
			}

			if len(rec.Answers) == 0 {
				emptyAnswersCount++
			}

			for ansIdx, ans := range rec.Answers {
				schema, ok := toolSchemas[ans.Name]
				if !ok {
					fmt.Fprintf(os.Stderr, "VALIDATION ERROR [%s:L%d, ans #%d]: unknown tool name %q!\n", split, lineNum, ansIdx+1, ans.Name)
					os.Exit(1)
				}

				// Check required arguments
				for _, reqProp := range schema.Parameters.Required {
					val, exists := ans.Arguments[reqProp]
					if !exists || fmt.Sprintf("%v", val) == "" {
						fmt.Fprintf(os.Stderr, "VALIDATION ERROR [%s:L%d, ans #%d]: required arg %q missing or empty for tool %q!\n", split, lineNum, ansIdx+1, reqProp, ans.Name)
						os.Exit(1)
					}
					// Check placeholders
					strVal := fmt.Sprintf("%v", val)
					if strings.Contains(strVal, "<") || strings.Contains(strVal, "TODO") || strings.Contains(strVal, "placeholder") {
						fmt.Fprintf(os.Stderr, "VALIDATION ERROR [%s:L%d, ans #%d]: ungrounded placeholder arg %q=%q!\n", split, lineNum, ansIdx+1, reqProp, strVal)
						os.Exit(1)
					}
				}

				// Collect pod names for leakage check
				if podVal, ok := ans.Arguments["pod"]; ok {
					podStr := fmt.Sprintf("%v", podVal)
					if split == "train" {
						trainPodNames[podStr] = true
					} else if split == "test" {
						testPodNames[podStr] = true
					}
				}
			}

			totalCount++
			splitCounts[split]++
		}
	}

	// ── Data Leakage Check between train and test ─────────────────────────
	leakageFound := false
	for testPod := range testPodNames {
		if trainPodNames[testPod] {
			fmt.Fprintf(os.Stderr, "LEAKAGE ERROR: Pod %q found in both train and holdout test set!\n", testPod)
			leakageFound = true
		}
	}
	if leakageFound {
		os.Exit(1)
	}

	return totalCount, splitCounts, emptyAnswersCount, unknownDiagCount, realCount, reasoningCount
}

// ── Report Printer ───────────────────────────────────────────────────────────

func printDatasetReport(totalCount int, splitCounts map[string]int, emptyAnswersCount, unknownDiagCount, realCount, reasoningCount int) {
	fmt.Println()
	fmt.Println("══════════════════════════════════════════════════════════════")
	fmt.Println("  Diagnostic Dataset Builder — Feature 8 Report")
	fmt.Println("══════════════════════════════════════════════════════════════")
	fmt.Printf("  Total Examples      : %d\n", totalCount)
	fmt.Printf("  Train Split (train) : %d (%.1f%%)\n", splitCounts["train"], float64(splitCounts["train"])/float64(totalCount)*100)
	fmt.Printf("  Val Split (val)     : %d (%.1f%%)\n", splitCounts["validation"], float64(splitCounts["validation"])/float64(totalCount)*100)
	fmt.Printf("  Test Split (test)   : %d (%.1f%%)\n", splitCounts["test"], float64(splitCounts["test"])/float64(totalCount)*100)
	fmt.Println()
	fmt.Printf("  Real Verified Cases : %d\n", realCount)
	fmt.Printf("  Synthetic Cases     : %d\n", totalCount-realCount)
	fmt.Printf("  Empty Answers []    : %d (negative / non-diagnostic)\n", emptyAnswersCount)
	fmt.Printf("  UNKNOWN Cases       : %d (insufficient evidence / symptom-only)\n", unknownDiagCount)
	fmt.Printf("  With Reasoning Field: %d\n", reasoningCount)
	fmt.Println()
	fmt.Println("  Dataset Validation  : PASSED ✅ (No secrets, no placeholders, schema matching)")
	fmt.Println("  Data Leakage Check  : PASSED ✅ (Train/test pod names strictly disjoint)")
	fmt.Println("══════════════════════════════════════════════════════════════")
	fmt.Println()
}
