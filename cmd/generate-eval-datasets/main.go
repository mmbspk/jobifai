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
	if err := ensureFormVisionFixtures(root); err != nil {
		fmt.Fprintf(os.Stderr, "form_vision fixtures: %v\n", err)
		os.Exit(1)
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
	if task == "form_vision" {
		if err := copyFormVisionFixtures(root, dir); err != nil {
			return err
		}
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
		seniority := []string{"junior", "mid", "senior", "lead"}[i%4]
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
		remote := i%3 == 0
		arrange := "onsite"
		if remote {
			arrange = "remote"
		}
		return map[string]any{
			"id": id, "task": task, "classification": "synthetic", "critical": pass,
			"input": map[string]any{
				"profile": map[string]any{
					"skills": []string{prof, fmt.Sprintf("%s tooling", prof)},
					"experience_details": []map[string]any{{
						"position": fmt.Sprintf("%s %s", seniority, prof),
						"company":  fmt.Sprintf("%s Partners %d", prof, i%7),
						"employment_period": fmt.Sprintf("%d – Present", 2015+(i%8)),
					}},
				},
				"job_description": fmt.Sprintf("%s %s role (%s, %s). Requires %s experience and qualification track %d.",
					seniority, prof, arrange, []string{"Sydney", "Melbourne", "Brisbane", "Perth"}[i%4], prof, i%6),
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
		extra := []string{
			"Operates outpatient clinics.", "Processes consumer lending.", "Mixed retail supply chain.", "Educational media production.",
		}[i%4]
		return map[string]any{
			"id": id, "task": task, "classification": "synthetic", "critical": s.critical,
			"input": map[string]any{
				"title": s.title, "company": fmt.Sprintf("%s %d", s.company, i%5),
				"description": s.desc + ". " + extra,
			},
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
	case "form_vision":
		fx := formVisionFixtures[i%len(formVisionFixtures)]
		return map[string]any{
			"id": id, "task": task, "classification": "synthetic", "critical": true,
			"input":  map[string]any{"fixture_png": fx.file},
			"expect": map[string]any{"fields": fx.fields},
		}
	case "resume_extract":
		prof := professions[i%len(professions)]
		name := fmt.Sprintf("Alex %s", prof)
		company := fmt.Sprintf("Northline %s", prof)
		text := fmt.Sprintf("%s\nEmail: alex.%d@example.com\n\nExperience\n%s at %s (2019 – Present)\nSkills: %s, reporting, stakeholder communication",
			name, i, prof, company, prof)
		return map[string]any{
			"id": id, "task": task, "classification": "synthetic", "critical": true,
			"input": map[string]any{"resume_text": text},
			"expect": map[string]any{
				"personal": map[string]any{"full_name": name, "email": fmt.Sprintf("alex.%d@example.com", i)},
				"employers": []map[string]any{{"company": company, "title": prof, "start": "2019", "end": "Present"}},
				"skills": []string{prof},
			},
		}
	case "resume_tailoring":
		prof := professions[i%len(professions)]
		profile := map[string]any{
			"personal_information": map[string]any{"full_name": fmt.Sprintf("Sam %s", prof)},
			"experience_details":   []map[string]any{{"company": "Harbor Logistics", "position": prof, "employment_period": "2018 – Present"}},
			"skills":               []string{"Java", "AWS", "PostgreSQL"},
			"publications":         []map[string]any{{"title": "Efficient routing for regional deliveries"}},
		}
		return map[string]any{
			"id": id, "task": task, "classification": "synthetic", "critical": true,
			"input": map[string]any{
				"profile": profile,
				"job_description": fmt.Sprintf("Role requires Go, Kubernetes, and GCP leadership for %s programs.", prof),
			},
			"expect": map[string]any{
				"preserve_name":          fmt.Sprintf("Sam %s", prof),
				"forbidden_terms":        []string{"kubernetes", " golang", " gcp"},
				"required_publications":  []string{"Efficient routing for regional deliveries"},
				"required_employers":     []string{"Harbor Logistics"},
			},
		}
	case "cover_letter":
		prof := professions[i%len(professions)]
		profile := map[string]any{
			"personal_information": map[string]any{"full_name": fmt.Sprintf("Jordan %s", prof)},
			"experience_details":   []map[string]any{{"company": "Brightfield Co", "position": prof}},
			"skills":               []string{prof, "client communication"},
		}
		return map[string]any{
			"id": id, "task": task, "classification": "synthetic", "critical": true,
			"input": map[string]any{
				"profile": profile,
				"job_description": fmt.Sprintf("Hiring a %s to support enterprise accounts in healthcare logistics.", prof),
			},
			"expect": map[string]any{
				"grounded_facts":  []string{"Brightfield Co", prof},
				"forbidden_terms": []string{"kubernetes"},
				"min_paragraphs":  3, "max_paragraphs": 4, "max_words": 450,
			},
		}
	case "application_questions":
		prof := professions[i%len(professions)]
		qs := []map[string]any{
			{"id": "auth", "text": "Are you authorized to work?"},
			{"id": "years", "text": fmt.Sprintf("Years of %s experience?", prof)},
			{"id": "reloc", "text": "Willing to relocate?"},
		}
		return map[string]any{
			"id": id, "task": task, "classification": "synthetic", "critical": true,
			"input": map[string]any{
				"profile": map[string]any{
					"personal_information": map[string]any{"full_name": fmt.Sprintf("Casey %s", prof)},
					"experience_details":   []map[string]any{{"position": prof, "employment_period": "2016 – Present"}},
				},
				"questions": qs,
			},
			"expect": map[string]any{
				"answer_count": 3,
				"answers": []map[string]any{
					{"question_id": "auth", "match": "exact", "value": "Yes"},
					{"question_id": "years", "match": "contains", "contains": "2016"},
					{"question_id": "reloc", "match": "exact", "value": "Yes", "max_words": 12},
				},
			},
		}
	default:
		panic("unknown task " + task)
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
