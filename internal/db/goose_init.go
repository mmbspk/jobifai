package db

import (
	"database/sql"
	"sync"

	"github.com/pressly/goose/v3"
)

var (
	gooseInitOnce sync.Once
	gooseInitErr  error
	gooseRunMu    sync.Mutex
)

func initGoose() error {
	gooseInitOnce.Do(func() {
		goose.SetBaseFS(migrations)
		goose.AddNamedMigrationContext("060_reconcile_apply_engine.go", reconcileApplyEngineSchema, keepApplyEngineSchema)
		gooseInitErr = goose.SetDialect("sqlite3")
	})
	return gooseInitErr
}

// The legacy Goose runner mutates its registered Go migration metadata. Keep
// concurrent database opens from racing over that shared registry.
func applyMigrations(database *sql.DB) error {
	gooseRunMu.Lock()
	defer gooseRunMu.Unlock()
	return goose.Up(database, "migrations")
}
