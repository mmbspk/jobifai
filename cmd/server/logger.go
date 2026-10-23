package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// buildBaseWriter selects the output writer for a given log format.
// logFmt must be "json" or "console"; an empty string picks the default based
// on isProduction. w is the underlying destination (e.g. os.Stdout or a test buffer).
// The returned writer is safe to use in zerolog.MultiLevelWriter alongside other
// writers (e.g. a WebSocket broadcaster that always receives raw JSON).
func buildBaseWriter(logFmt string, isProduction bool, w io.Writer) (io.Writer, error) {
	logFmt = strings.ToLower(strings.TrimSpace(logFmt))
	if logFmt == "" {
		if isProduction {
			logFmt = "json"
		} else {
			logFmt = "console"
		}
	}
	if logFmt != "json" && logFmt != "console" {
		return nil, fmt.Errorf("invalid LOG_FORMAT=%q (must be \"json\" or \"console\")", logFmt)
	}
	if logFmt == "json" {
		return w, nil
	}
	return zerolog.ConsoleWriter{Out: w, TimeFormat: time.RFC3339}, nil
}

// buildLogger builds a zerolog.Logger with the standard base fields stamped on
// every line. w should be the final destination writer (often a
// zerolog.MultiLevelWriter combining a formatted stdout and a WS broadcaster).
func buildLogger(logEnv, buildSHA string, w io.Writer) zerolog.Logger {
	env := strings.TrimSpace(logEnv)
	if env == "" {
		env = "development"
	}
	return zerolog.New(w).With().
		Timestamp().
		Str("service", "jobifai").
		Str("env", env).
		Str("build_sha", buildSHA).
		Logger()
}

// logEnvFromEnv reads LOG_FORMAT and JOBIFAI_ENV from the process environment.
func logEnvFromEnv() (logFmt, logEnv string) {
	return os.Getenv("LOG_FORMAT"), os.Getenv("JOBIFAI_ENV")
}
