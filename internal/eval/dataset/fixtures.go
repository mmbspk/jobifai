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
	var base string
	if c.BundleDir != "" {
		base = filepath.Join(c.BundleDir, "fixtures")
	} else {
		roots, err := ResolveRoot()
		if err != nil {
			return nil, err
		}
		base = filepath.Join(roots, "synthetic", c.Task, "fixtures")
	}
	p := filepath.Join(base, rel)
	abs, err := filepath.Abs(p)
	if err != nil {
		return nil, err
	}
	rootAbs, _ := filepath.Abs(base)
	if !strings.HasPrefix(abs, rootAbs+string(os.PathSeparator)) {
		return nil, fmt.Errorf("fixture escapes directory")
	}
	return os.ReadFile(abs)
}
