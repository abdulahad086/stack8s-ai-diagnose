package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDatasetGenerationAndValidation(t *testing.T) {
	// 1. Generate JSONL datasets in dataset/
	generateDatasets()

	// 2. Validate datasets
	total, splitCounts, emptyAns, unknownDiag, realCount, reasoningCount := validateDatasets()

	if total < 300 {
		t.Fatalf("expected total dataset records >= 300, got %d", total)
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
		t.Fatalf("expected controlled kubernetes verified cases, got 0")
	}

	if reasoningCount == 0 {
		t.Fatalf("expected reasoning fields in dataset, got 0")
	}

	t.Logf("Dataset V2 successfully validated: total=%d, train=%d, val=%d, test=%d, controlled_k8s=%d",
		total, splitCounts["train"], splitCounts["validation"], splitCounts["test"], realCount)
}

func TestDatasetSchemaCompliance(t *testing.T) {
	generateDatasets()

	toolSchemas := loadToolSchemas()
	if len(toolSchemas) != 9 {
		t.Fatalf("expected 9 tools in tools.json, got %d", len(toolSchemas))
	}

	expectedTools := []string{
		"get_pod_status",
		"get_pod_events",
		"get_container_logs",
		"get_node_status",
		"get_gpu_status",
		"get_service_status",
		"get_storage_status",
		"get_workload_status",
		"get_configuration_status",
	}

	for _, toolName := range expectedTools {
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

	// Verify manifest.jsonl exists
	manifestPath := filepath.Join("dataset", "incidents", "manifest.jsonl")
	mb, err := os.ReadFile(manifestPath)
	if err != nil || len(mb) == 0 {
		t.Fatalf("expected dataset/incidents/manifest.jsonl to exist and be non-empty")
	}
}

func TestDatasetNoPlaceholdersOrSecrets(t *testing.T) {
	generateDatasets()

	for _, split := range []string{"train", "validation", "test"} {
		filePath := filepath.Join("dataset", split+".jsonl")
		f, err := os.Open(filePath)
		if err != nil {
			t.Fatalf("failed to open %s: %v", filePath, err)
		}
		defer f.Close()

		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.Contains(line, "<pod") || strings.Contains(line, "TODO") || strings.Contains(line, "placeholder") {
				t.Fatalf("file %s contains placeholder string: %s", filePath, line)
			}
			if strings.Contains(strings.ToLower(line), "bearer ") || strings.Contains(line, "-----BEGIN CERTIFICATE-----") {
				t.Fatalf("file %s contains potential secret string: %s", filePath, line)
			}
		}
	}
}
