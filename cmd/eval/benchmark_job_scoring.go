package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"

	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/eval/candidate"
	"github.com/user/jobifai/internal/eval/dataset"
	"github.com/user/jobifai/internal/eval/engine"
	"github.com/user/jobifai/internal/eval/providers"
	"github.com/user/jobifai/internal/eval/runmeta"
	"github.com/user/jobifai/internal/pricing"
)

func cmdBenchmarkJobScoring(args []string) {
	fs := flag.NewFlagSet("benchmark-job-scoring", flag.ExitOnError)
	dbPath := fs.String("db", envOr("JOBIFAI_DB", "data/jobifai.db"), "SQLite path")
	maxUSD := fs.Float64("max-usd", 2.0, "hard eval budget cap")
	_ = fs.Parse(args)

	svc, sqldb := evalService(*dbPath)
	defer func() { _ = sqldb.Close() }()

	baseline := candidate.Spec{
		Provider: "claude", Model: "claude-sonnet-4-6", Effort: "medium",
		MaxTokens: 8192, TimeoutSec: 120,
	}
	candidates := []candidate.Spec{
		{Provider: "claude", Model: "claude-haiku-4-5-20251001", MaxTokens: 8192, TimeoutSec: 120},
	}
	sync := false
	id, err := svc.CreateRun(context.Background(), engine.RunParams{
		Task: domain.TaskJobScoring, DatasetVersion: "full", DatasetSource: dataset.SourceSynthetic,
		Purpose: runmeta.PurposeBenchmark, RunnerType: runmeta.RunnerReal,
		Baseline: baseline, Candidates: candidates,
		BudgetUSD: *maxUSD, InitiatedBy: "benchmark-job-scoring-cli",
		StartAsync: &sync,
	})
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	fmt.Printf("benchmark run_id=%s task=job_scoring dataset=full budget=$%.2f\n", id, *maxUSD)
	if err := svc.ExecuteRun(context.Background(), id); err != nil {
		fmt.Printf("ExecuteRun error: %v\n", err)
	}
	printRunReport(sqldb, id, baseline, true)
	printSyntheticFailures(sqldb, id)
	printJobScoringBenchmarkSummary(sqldb, id)
}

func evalService(dbPath string) (*engine.Service, *sql.DB) {
	sqldb, err := db.Open(dbPath)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	machineKey, err := config.MachineKey(sqldb)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	cfgStore := config.NewStore(sqldb)
	secrets := config.NewSecretsStore(sqldb, machineKey)
	gs := config.ResolveOperationalSettings(cfgStore, domain.SystemUserID)
	evalCat := &pricing.EvalCatalog{Approved: pricing.DefaultCatalog(), DB: sqldb}
	factory := &providers.Factory{
		Catalog: pricing.DefaultCatalog(), EvalPricing: evalCat, BaseLLM: gs.LLM,
		KeyResolver: func(provider string) (string, bool) {
			if provider == gs.LLM.Provider {
				k, err := config.ResolveLLMAPIKey(secrets, domain.SystemUserID, gs.LLM.UseProxy)
				return k, err == nil && k != ""
			}
			k, err := secrets.Get(domain.SystemUserID, "eval_"+provider+"_api_key")
			return k, err == nil && k != ""
		},
	}
	svc := &engine.Service{
		DB: sqldb, Run: engine.ProviderRunner{Factory: factory},
		Config: engine.Config{MaxConcurrency: 3}, EvalPricing: evalCat,
	}
	return svc, sqldb
}
