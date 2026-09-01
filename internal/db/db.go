package db

import (
	"database/sql"
	"embed"
	"fmt"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Open opens (or creates) the SQLite database at path and runs all pending migrations.
func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_foreign_keys=on&_busy_timeout=5000&_synchronous=NORMAL")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// SQLite (WAL or otherwise) only allows one writer at a time. With the
	// default pool size, writers from different goroutines land on different
	// connections and race the WAL writer lock — returning SQLITE_BUSY even
	// while _busy_timeout is set. Pinning the pool to a single connection
	// turns that race into harmless in-process queuing.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	goose.SetBaseFS(migrations)
	if err := goose.SetDialect("sqlite3"); err != nil {
		return nil, fmt.Errorf("goose dialect: %w", err)
	}
	if err := goose.Up(db, "migrations"); err != nil {
		return nil, fmt.Errorf("goose up: %w", err)
	}
	return db, nil
}
