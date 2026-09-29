package db

import (
	"sync"

	"github.com/pressly/goose/v3"
)

var (
	gooseInitOnce sync.Once
	gooseInitErr error
)

func initGoose() error {
	gooseInitOnce.Do(func() {
		goose.SetBaseFS(migrations)
		gooseInitErr = goose.SetDialect("sqlite3")
	})
	return gooseInitErr
}
