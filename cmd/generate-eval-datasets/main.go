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
			"job_scoring": 90, "employment_ethics": 45, "form_answer": 70, "form_vision": 25,
			"resume_extract": 22, "resume_tailoring": 22, "cover_letter": 22, "application_questions": 35,
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

var professions = []string{
	"nurse", "accountant", "chef", "mechanical engineer", "HR specialist", "data analyst",
	"construction manager", "marketing manager", "cybersecurity analyst", "office administrator",
	"product manager", "sales executive", "graduate trainee", "project manager", "teacher",
}

func synthCase(task string, i int) map[string]any {
	id := fmt.Sprintf("%s-%04d", task, i+1)
	switch task {
	case "job_scoring":
		prof := professions[i%len(professions)]
		pass := i%4 != 0
		exp := map[string]any{"pass_threshold": 7}
		if pass {
			exp["expect_pass"] = true
			exp["min_score"] = 7
			exp["max_score"] = 10
		} else {
			exp["expect_skip"] = true
			exp["max_score"] = 4
		}
		return map[string]any{
			"id": id, "task": task, "classification": "synthetic", "critical": pass,
			"input": map[string]any{
				"profile": map[string]any{
					"skills": []string{prof, fmt.Sprintf("skill-%d", i)},
					"experience_details": []map[string]any{{"position": prof, "company": fmt.Sprintf("Co-%d", i)}},
				},
				"job_description": fmt.Sprintf("Seeking %s with focus area %d and location region-%d", prof, i, i%5),
			},
			"expect": exp,
		}
	case "employment_ethics":
		scenarios := []struct {
			title, company, desc, verdict string
			critical                      bool
		}{
			{"Software Engineer", "City Hospital", "Maintain patient scheduling systems", "HALAL", true},
			{"Loan Officer", "Retail Bank", "Set interest rates for consumer loans", "HARAM", true},
			{"Logistics Coordinator", "Mixed Retail Group", "Warehouse operations for varied consumer goods", "DOUBTFUL", false},
			{"Content Editor", "Media Studio", "Edit educational videos", "DOUBTFUL", false},
		}
		s := scenarios[i%len(scenarios)]
		return map[string]any{
			"id": id, "task": task, "classification": "synthetic", "critical": s.critical,
			"input": map[string]any{"title": s.title, "company": s.company, "description": s.desc + fmt.Sprintf(" ref-%d", i)},
			"expect": map[string]any{"verdict": s.verdict},
		}
	case "form_answer":
		questions := []struct {
			q, exact string
			opts     []string
		}{
			{"Are you authorized to work in this country?", "Yes", []string{"Yes", "No"}},
			{"Do you require employer sponsorship?", "No", []string{"Yes", "No"}},
			{"What is your notice period?", "4 weeks", nil},
			{"How many years of project management experience do you have?", "7", nil},
			{"Preferred phone number?", "+61400111222", nil},
		}
		qq := questions[i%len(questions)]
		in := map[string]any{
			"question": qq.q,
			"profile_json": map[string]any{
				"application_defaults": map[string]any{"requires_sponsorship": false, "notice_period": "4 weeks"},
				"personal_information": map[string]any{"phone": "+61400111222"},
				"experience_details":   []map[string]any{{"position": "Project Manager", "employment_period": "2017 – Present"}},
			},
		}
		if len(qq.opts) > 0 {
			in["options"] = qq.opts
		}
		exp := map[string]any{"exact": qq.exact}
		return map[string]any{"id": id, "task": task, "classification": "synthetic", "input": in, "expect": exp}
	default:
		return map[string]any{
			"id": id, "task": task, "classification": "synthetic",
			"input": map[string]any{"case_index": i, "task": task}, "expect": map[string]any{"non_empty": true},
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
