package dataset

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ReadFixtureBytes loads a PNG fixture relative to the dataset bundle root.
func ReadFixtureBytes(c Case, rel string) ([]byte, error) {
	if err := ValidateIdentifier("fixture", rel); err != nil {
		return nil, err
	}
	if strings.Contains(rel, "..") {
		return nil, fmt.Errorf("invalid fixture path")
	}
	// Bundle root is set on load; cases carry no root — resolve via env not ideal.
	// Caller passes relative fixture under task directory fixtures/.
 roots, err := ResolveRoot()
	if err != nil {
		return nil, err
	}
	task := c.Task
	p := filepath.Join(roots, "synthetic", task, "fixtures", rel)
	abs, err := filepath.Abs(p)
	if err != nil {
		return nil, err
	}
	rootAbs, _ := filepath.Abs(filepath.Join(roots, "synthetic", task, "fixtures"))
	if !strings.HasPrefix(abs, rootAbs+string(os.PathSeparator)) {
		return nil, fmt.Errorf("fixture escapes directory")
	}
	return os.ReadFile(abs)
}
