package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/eval/candidate"
	"github.com/user/jobifai/internal/eval/dataset"
	"github.com/user/jobifai/internal/eval/engine"
	"github.com/user/jobifai/internal/eval/runmeta"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "datasets":
		cmdDatasets()
	case "run":
		cmdRun(os.Args[2:])
	case "show":
		cmdShow(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "Usage:\n  go run ./cmd/eval datasets\n  go run ./cmd/eval run --task job_scoring --dataset smoke --max-cost-usd 1\n  go run ./cmd/eval run --fake ...\n  go run ./cmd/eval show <run-id>\n")
}

func cmdDatasets() {
	root, err := dataset.ResolveRoot()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	fmt.Println(root)
}

func cmdRun(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	task := fs.String("task", domain.TaskJobScoring, "stable task id")
	ds := fs.String("dataset", "smoke", "dataset version")
	maxCost := fs.Float64("max-cost-usd", 1, "max eval budget USD")
	fake := fs.Bool("fake", false, "use fake runner (CI/dev only)")
	_ = fs.Parse(args)

	dbPath := os.Getenv("JOBIFAI_DB")
	if dbPath == "" {
		dbPath = "data/jobifai.db"
	}
	sqldb, err := db.Open(dbPath)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	defer func() { _ = sqldb.Close() }()

	runnerType := runmeta.RunnerReal
	if *fake {
		runnerType = runmeta.RunnerFake
	}
	svc := &engine.Service{DB: sqldb, Run: engine.FakeRunner{}, Config: engine.Config{MaxConcurrency: 2}}
	if runnerType == runmeta.RunnerReal {
		// Real runner wiring requires server config; use fake with --fake for local smoke without DB secrets.
		fmt.Fprintln(os.Stderr, "real runner requires admin API or server wiring; use --fake for local deterministic runs")
		os.Exit(1)
	}
	purpose := runmeta.PurposeSmoke
	if *ds == "full" {
		purpose = runmeta.PurposeBenchmark
	}
	id, err := svc.CreateRun(context.Background(), engine.RunParams{
		Task: *task, DatasetVersion: *ds, DatasetSource: dataset.SourceSynthetic,
		Purpose: purpose, RunnerType: runnerType,
		Baseline: candidate.Spec{Provider: "claude", Model: "claude-sonnet-4-6"},
		Candidates: []candidate.Spec{{Provider: "claude", Model: "claude-haiku-4-5-20251001"}},
		BudgetUSD: *maxCost, InitiatedBy: "cli",
	})
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	fmt.Println(id)
}

func cmdShow(args []string) {
	if len(args) < 1 {
		fmt.Println("missing run id")
		os.Exit(2)
	}
	dbPath := os.Getenv("JOBIFAI_DB")
	if dbPath == "" {
		dbPath = "data/jobifai.db"
	}
	sqldb, err := db.Open(dbPath)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	defer func() { _ = sqldb.Close() }()
	var status, summary, runnerType string
	_ = sqldb.QueryRow(`SELECT status, summary_json, COALESCE(runner_type,'fake') FROM model_eval_runs WHERE id=?`, args[0]).Scan(&status, &summary, &runnerType)
	fmt.Printf("status=%s runner_type=%s summary=%s\n", status, runnerType, summary)
}
