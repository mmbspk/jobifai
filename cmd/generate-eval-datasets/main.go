// Generates synthetic eval datasets (non-PII) under eval/datasets/synthetic/.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	root := filepath.Join("eval", "datasets", "synthetic")
	targets := map[string]map[string]int{
		"smoke": {
			"job_scoring": 5, "employment_ethics": 4, "form_answer": 5, "form_vision": 3,
			"resume_extract": 3, "resume_tailoring": 3, "cover_letter": 3, "application_questions": 4,
		},
		"full": {
			"job_scoring": 100, "employment_ethics": 50, "form_answer": 80, "form_vision": 30,
			"resume_extract": 25, "resume_tailoring": 25, "cover_letter": 25, "application_questions": 40,
		},
	}
	for version, tasks := range targets {
		for task, n := range tasks {
			if err := writeDataset(root, task, version, n); err != nil {
				fmt.Fprintf(os.Stderr, "%s/%s: %v\n", task, version, err)
				os.Exit(1)
			}
		}
	}
	fmt.Println("datasets generated")
}

func writeDataset(root, task, version string, n int) error {
	dir := filepath.Join(root, task, version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	casesPath := filepath.Join(dir, "cases.jsonl")
	f, err := os.Create(casesPath)
	if err != nil {
		return err
	}
	for i := 0; i < n; i++ {
		c := synthCase(task, i)
		b, _ := json.Marshal(c)
		if _, err := f.Write(append(b, '\n')); err != nil {
			_ = f.Close()
			return err
		}
	}
	_ = f.Close()
	sum := sha256File(casesPath)
	m := map[string]any{
		"task": task, "version": version,
		"description": fmt.Sprintf("Synthetic %s dataset (%s)", task, version),
		"case_count": n, "classification": "synthetic",
		"created_at": time.Now().UTC().Format(time.RFC3339),
		"schema_version": 1, "sha256": sum, "cases_file": "cases.jsonl",
	}
	mb, _ := json.MarshalIndent(m, "", "  ")
	return os.WriteFile(filepath.Join(dir, "manifest.json"), mb, 0o644)
}

func synthCase(task string, i int) map[string]any {
	id := fmt.Sprintf("%s-%04d", task, i+1)
	switch task {
	case "job_scoring":
		pass := i%5 != 0
		exp := map[string]any{"min_score": 0, "max_score": 10, "pass_threshold": 7}
		if pass {
			exp["expect_pass"] = true
			exp["min_score"] = 7
		} else {
			exp["expect_skip"] = true
			exp["max_score"] = 4
		}
		return map[string]any{
			"id": id, "task": task, "classification": "synthetic", "critical": pass,
			"tags": []string{"synthetic"},
			"input": map[string]any{"profile": map[string]any{"skills": []string{"project management"}}, "job_description": "Role requiring coordination"},
			"expect": exp,
		}
	case "employment_ethics":
		verdicts := []string{"HALAL", "HARAM", "DOUBTFUL"}
		v := verdicts[i%3]
		return map[string]any{
			"id": id, "task": task, "classification": "synthetic", "critical": v != "DOUBTFUL",
			"input": map[string]any{"title": "Analyst", "company": "Example Co", "description": "Permissible services"},
			"expect": map[string]any{"verdict": v},
		}
	case "form_answer":
		return map[string]any{
			"id": id, "task": task, "classification": "synthetic",
			"input":  map[string]any{"question": "Are you authorized to work?", "options": []string{"Yes", "No"}},
			"expect": map[string]any{"exact": "Yes"},
		}
	default:
		return map[string]any{
			"id": id, "task": task, "classification": "synthetic",
			"input": map[string]any{"note": "placeholder"}, "expect": map[string]any{},
		}
	}
}

func sha256File(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	s := sha256.Sum256(b)
	return strings.ToLower(hex.EncodeToString(s[:]))
}
