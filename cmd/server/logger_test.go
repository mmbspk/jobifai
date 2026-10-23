package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildBaseWriter_JSONFormat verifies that LOG_FORMAT=json writes raw JSON
// lines that are parseable and not console-decorated.
func TestBuildBaseWriter_JSONFormat(t *testing.T) {
	buf := &bytes.Buffer{}
	w, err := buildBaseWriter("json", false, buf)
	require.NoError(t, err)

	logger := buildLogger("test", "abc1234", w)
	logger.Info().Str("probe", "hello").Msg("test message")

	line := strings.TrimSpace(buf.String())
	require.NotEmpty(t, line, "expected a log line")

	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(line), &m), "LOG_FORMAT=json must produce valid JSON: %s", line)
}

// TestBuildBaseWriter_ConsoleFormat verifies that LOG_FORMAT=console produces
// human-readable output (not JSON-parseable as a bare object).
func TestBuildBaseWriter_ConsoleFormat(t *testing.T) {
	buf := &bytes.Buffer{}
	w, err := buildBaseWriter("console", false, buf)
	require.NoError(t, err)

	logger := buildLogger("test", "abc1234", w)
	logger.Info().Msg("console test")

	line := strings.TrimSpace(buf.String())
	require.NotEmpty(t, line, "expected a log line")

	// Console writer adds ANSI codes or at minimum emits a non-JSON line.
	var m map[string]any
	assert.Error(t, json.Unmarshal([]byte(line), &m),
		"LOG_FORMAT=console should not produce bare JSON objects")
}

// TestBuildBaseWriter_DefaultIsJSON verifies that an empty LOG_FORMAT selects
// json when isProduction=true.
func TestBuildBaseWriter_DefaultIsJSON(t *testing.T) {
	buf := &bytes.Buffer{}
	w, err := buildBaseWriter("", true, buf)
	require.NoError(t, err)

	logger := buildLogger("production", "sha999", w)
	logger.Info().Msg("prod line")

	line := strings.TrimSpace(buf.String())
	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(line), &m),
		"isProduction default must be json: %s", line)
}

// TestBuildBaseWriter_InvalidFormat verifies that an unrecognized LOG_FORMAT
// returns an error.
func TestBuildBaseWriter_InvalidFormat(t *testing.T) {
	_, err := buildBaseWriter("xml", false, &bytes.Buffer{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid LOG_FORMAT")
}

// TestBuildLogger_BaseFieldsPresent verifies that service, env, and build_sha
// are stamped on every line.
func TestBuildLogger_BaseFieldsPresent(t *testing.T) {
	buf := &bytes.Buffer{}
	w, err := buildBaseWriter("json", false, buf)
	require.NoError(t, err)

	logger := buildLogger("staging", "deadbeef", w)
	logger.Info().Msg("check base fields")

	var m map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &m))

	assert.Equal(t, "jobifai", m["service"], "service field")
	assert.Equal(t, "staging", m["env"], "env field")
	assert.Equal(t, "deadbeef", m["build_sha"], "build_sha field")
	assert.NotEmpty(t, m["time"], "time field must be present")
}

// TestBuildLogger_EmptyEnvDefaultsDevelopment verifies that an empty logEnv
// is filled with "development".
func TestBuildLogger_EmptyEnvDefaultsDevelopment(t *testing.T) {
	buf := &bytes.Buffer{}
	w, err := buildBaseWriter("json", false, buf)
	require.NoError(t, err)

	logger2 := buildLogger("", "sha123", w)
	logger2.Info().Msg("default env")

	var m map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &m))
	assert.Equal(t, "development", m["env"], "empty env must default to development")
}
