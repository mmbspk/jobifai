// Command eval runs Jobifai model evaluations locally (admin/operator tool).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/eval/candidate"
	"github.com/user/jobifai/internal/eval/dataset"
	"github.com/user/jobifai/internal/eval/engine"
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
	fmt.Fprintf(os.Stderr, "Usage:\n  go run ./cmd/eval datasets\n  go run ./cmd/eval run --task job_scoring --dataset smoke --max-cost-usd 1\n  go run ./cmd/eval show <run-id>\n")
}

func cmdDatasets() {
	root, err := dataset.ResolveRoot()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.Name() != "manifest.json" {
			return nil
		}
		b, _ := os.ReadFile(path)
		fmt.Println(string(b))
		return nil
	})
}

func cmdRun(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	task := fs.String("task", domain.TaskJobScoring, "stable task id")
	ds := fs.String("dataset", "smoke", "dataset version")
	maxCost := fs.Float64("max-cost-usd", 1, "max eval budget USD")
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

	svc := &engine.Service{
		DB:     sqldb,
		Run:    engine.FakeRunner{},
		Config: engine.Config{MaxBudgetMicro: int64(*maxCost * 1_000_000), MinCases: 3},
	}
	id, err := svc.CreateRun(context.Background(), *task, *ds,
		candidate.Spec{Provider: "claude", Model: "claude-sonnet-4-6"},
		[]candidate.Spec{{Provider: "claude", Model: "claude-haiku-4-5-20251001"}},
		*maxCost, "cli")
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
	var status, summary string
	_ = sqldb.QueryRow(`SELECT status, summary_json FROM model_eval_runs WHERE id=?`, args[0]).Scan(&status, &summary)
	fmt.Printf("status=%s summary=%s\n", status, summary)
}
