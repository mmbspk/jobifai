package dataset

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const SchemaVersion = 1

// Manifest describes a versioned eval dataset on disk.
type Manifest struct {
	Task         string `json:"task"`
	Version      string `json:"version"`
	Description  string `json:"description"`
	CaseCount    int    `json:"case_count"`
	Classification string `json:"classification"` // synthetic | private
	CreatedAt    string `json:"created_at"`
	SchemaVersion int   `json:"schema_version"`
	SHA256       string `json:"sha256"`
	CasesFile    string `json:"cases_file"`
}

// Case is one eval input + expectations (task-specific payload in Input/Expect).
type Case struct {
	ID             string          `json:"id"`
	Task           string          `json:"task"`
	Classification string          `json:"classification"`
	Input          json.RawMessage `json:"input"`
	Expect         json.RawMessage `json:"expect"`
	Critical       bool            `json:"critical"`
	Tags           []string        `json:"tags,omitempty"`
}

// Bundle is manifest + loaded cases.
type Bundle struct {
	Manifest Manifest
	Cases    []Case
	RootDir  string
}

// ResolveRoot finds eval/datasets from repo root or working directory.
func ResolveRoot() (string, error) {
	candidates := []string{"eval/datasets"}
	if wd, err := os.Getwd(); err == nil {
		for dir := wd; dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
			candidates = append(candidates, filepath.Join(dir, "eval", "datasets"))
		}
	}
	seen := map[string]bool{}
	for _, c := range candidates {
		if seen[c] {
			continue
		}
		seen[c] = true
		if st, err := os.Stat(c); err == nil && st.IsDir() {
			return c, nil
		}
	}
	return "", fmt.Errorf("eval datasets directory not found")
}

func loadDir(dir string) (Bundle, error) {
	mPath := filepath.Join(dir, "manifest.json")
	raw, err := os.ReadFile(mPath)
	if err != nil {
		return Bundle{}, fmt.Errorf("read manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return Bundle{}, err
	}
	casesPath := filepath.Join(dir, m.CasesFile)
	if m.CasesFile == "" {
		casesPath = filepath.Join(dir, "cases.jsonl")
	}
	cases, err := loadJSONL(casesPath)
	if err != nil {
		return Bundle{}, err
	}
	if m.CaseCount > 0 && len(cases) != m.CaseCount {
		return Bundle{}, fmt.Errorf("case count mismatch: manifest=%d file=%d", m.CaseCount, len(cases))
	}
	sum := sha256.Sum256(mustRead(casesPath))
	if m.SHA256 != "" && !strings.EqualFold(m.SHA256, hex.EncodeToString(sum[:])) {
		return Bundle{}, fmt.Errorf("dataset hash mismatch")
	}
	return Bundle{Manifest: m, Cases: cases, RootDir: dir}, nil
}

func loadJSONL(path string) ([]Case, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var out []Case
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1024*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var c Case
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			return nil, fmt.Errorf("jsonl %s: %w", path, err)
		}
		out = append(out, c)
	}
	return out, sc.Err()
}

func mustRead(path string) []byte {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return b
}
