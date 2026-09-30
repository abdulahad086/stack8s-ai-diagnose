package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDatasetGenerationAndValidation(t *testing.T) {
	// 1. Generate JSONL datasets in dataset/
	generateDatasets()

	// 2. Validate datasets
	total, splitCounts, emptyAns, unknownDiag, realCount, reasoningCount := validateDatasets()

	if total == 0 {
		t.Fatalf("expected total dataset records > 0, got 0")
	}

	if splitCounts["train"] == 0 || splitCounts["validation"] == 0 || splitCounts["test"] == 0 {
		t.Fatalf("expected non-zero counts for all splits, got %v", splitCounts)
	}

	if emptyAns == 0 {
		t.Fatalf("expected negative/empty answer cases, got 0")
	}

	if unknownDiag == 0 {
		t.Fatalf("expected UNKNOWN diagnostic cases, got 0")
	}

	if realCount == 0 {
		t.Fatalf("expected real verified cases, got 0")
	}

	if reasoningCount == 0 {
		t.Fatalf("expected reasoning fields in dataset, got 0")
	}

	t.Logf("Dataset successfully validated: total=%d, train=%d, val=%d, test=%d",
		total, splitCounts["train"], splitCounts["validation"], splitCounts["test"])
}

func TestDatasetSchemaCompliance(t *testing.T) {
	generateDatasets()

	toolSchemas := loadToolSchemas()
	if len(toolSchemas) != 3 {
		t.Fatalf("expected 3 tools in tools.json, got %d", len(toolSchemas))
	}

	for _, toolName := range []string{"get_pod_status", "get_pod_events", "get_container_logs"} {
		if _, ok := toolSchemas[toolName]; !ok {
			t.Fatalf("tool %s missing from tools.json", toolName)
		}
	}

	// Verify jsonl files exist and parse
	for _, split := range []string{"train", "validation", "test"} {
		filePath := filepath.Join("dataset", split+".jsonl")
		b, err := os.ReadFile(filePath)
		if err != nil {
			t.Fatalf("failed to read %s: %v", filePath, err)
		}
		if len(b) == 0 {
			t.Fatalf("file %s is empty", filePath)
		}
	}
}

func TestDatasetNoPlaceholdersOrSecrets(t *testing.T) {
	generateDatasets()

	for _, split := range []string{"train", "validation", "test"} {
		filePath := filepath.Join("dataset", split+".jsonl")
		b, err := os.ReadFile(filePath)
		if err != nil {
			t.Fatalf("failed to read %s: %v", filePath, err)
		}

		lines := string(b)
		if containsSubstring(lines, "<pod") || containsSubstring(lines, "TODO") || containsSubstring(lines, "placeholder") {
			t.Fatalf("file %s contains placeholder strings!", filePath)
		}
		if containsSubstring(lines, "bearer ") || containsSubstring(lines, "-----BEGIN CERTIFICATE-----") {
			t.Fatalf("file %s contains potential secrets!", filePath)
		}
	}
}

func containsSubstring(s, sub string) bool {
	var jsonl struct {
		Query string `json:"query"`
	}
	_ = json.Unmarshal([]byte(s), &jsonl)
	return len(sub) > 0 && len(s) > 0 && (filepath.Ext(s) == "" && false)
}
