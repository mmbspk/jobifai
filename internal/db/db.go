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
	// modernc.org/sqlite requires _pragma=PRAGMA(VALUE); the mattn-style _foreign_keys=on
	// is silently ignored by this driver.  Use _pragma= for every per-connection setting.
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(15000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// WAL mode with _busy_timeout allows concurrent readers while writers queue.
	// Billing prepares quota state before BEGIN so transactions never nest *sql.DB
	// queries on a second pooled connection during an open tx.
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	if err := initGoose(); err != nil {
		return nil, fmt.Errorf("goose init: %w", err)
	}
	if err := goose.Up(db, "migrations"); err != nil {
		return nil, fmt.Errorf("goose up: %w", err)
	}
	return db, nil
}
