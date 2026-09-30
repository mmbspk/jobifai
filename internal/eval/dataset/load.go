package dataset

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/user/jobifai/internal/domain"
)

// SourceSynthetic or SourcePrivate.
const (
	SourceSynthetic = "synthetic"
	SourcePrivate   = "private"
)

var allowedTasks = map[string]bool{
	domain.TaskJobScoring: true, domain.TaskEmploymentEthics: true,
	domain.TaskResumeExtract: true, domain.TaskResumeTailoring: true,
	domain.TaskCoverLetter: true, domain.TaskFormAnswer: true,
	domain.TaskFormVision: true, domain.TaskApplicationQuestions: true,
}

// LoadRequest selects a dataset on disk.
type LoadRequest struct {
	Task    string
	Version string
	Source  string
}

// Load opens and validates a dataset bundle.
func Load(req LoadRequest) (Bundle, error) {
	if !allowedTasks[req.Task] {
		return Bundle{}, fmt.Errorf("unknown task %q", req.Task)
	}
	if err := ValidateIdentifier("version", req.Version); err != nil {
		return Bundle{}, err
	}
	src := req.Source
	if src == "" {
		src = SourceSynthetic
	}
	if src != SourceSynthetic && src != SourcePrivate {
		return Bundle{}, fmt.Errorf("invalid dataset source")
	}
	root, err := datasetRoot(src)
	if err != nil {
		return Bundle{}, err
	}
	dir, err := safeJoin(root, req.Task, req.Version)
	if err != nil {
		return Bundle{}, err
	}
	b, err := loadDir(dir)
	if err != nil {
		return Bundle{}, err
	}
	if b.Manifest.Task != "" && b.Manifest.Task != req.Task {
		return Bundle{}, fmt.Errorf("manifest task mismatch")
	}
	if err := ValidateBundle(b); err != nil {
		return Bundle{}, err
	}
	return b, nil
}

func datasetRoot(source string) (string, error) {
	if source == SourcePrivate {
		for _, base := range privateRoots() {
			p := filepath.Join(base, "eval", "private")
			if st, err := os.Stat(p); err == nil && st.IsDir() {
				return p, nil
			}
		}
		return "", fmt.Errorf("private eval root not found")
	}
	r, err := ResolveRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(r, "synthetic"), nil
}

func privateRoots() []string {
	var out []string
	if wd, err := os.Getwd(); err == nil {
		for dir := wd; dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
			out = append(out, dir)
		}
	}
	out = append(out, "data")
	return out
}

func safeJoin(root string, parts ...string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	p := absRoot
	for _, part := range parts {
		if err := ValidateIdentifier("path", part); err != nil {
			return "", err
		}
		p = filepath.Join(p, part)
	}
	absP, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(absP, absRoot+string(os.PathSeparator)) && absP != absRoot {
		return "", fmt.Errorf("path escapes dataset root")
	}
	return absP, nil
}
