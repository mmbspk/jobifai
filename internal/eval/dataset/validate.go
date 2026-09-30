package dataset

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
)

var idPattern = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// ValidateBundle checks manifest/cases integrity before an eval run starts.
func ValidateBundle(b Bundle) error {
	if b.Manifest.SchemaVersion != 0 && b.Manifest.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported schema version %d", b.Manifest.SchemaVersion)
	}
	if b.Manifest.Task != "" {
		for _, c := range b.Cases {
			if c.Task != "" && c.Task != b.Manifest.Task {
				return fmt.Errorf("case %s task mismatch", c.ID)
			}
		}
	}
	seenID := map[string]bool{}
	type fp struct {
		hash string
		exp  string
		id   string
	}
	byInput := map[string]fp{}
	for _, c := range b.Cases {
		if c.ID == "" {
			return fmt.Errorf("case missing id")
		}
		if seenID[c.ID] {
			return fmt.Errorf("duplicate case id %s", c.ID)
		}
		seenID[c.ID] = true
		inHash := inputFingerprint(c.Input)
		expNorm := normalizeExpect(c.Expect)
		if prev, ok := byInput[inHash]; ok && prev.exp != expNorm {
			return fmt.Errorf("conflicting expectations for same input (cases %s and %s)", prev.id, c.ID)
		}
		byInput[inHash] = fp{hash: inHash, exp: expNorm, id: c.ID}
		if err := ValidateCaseContent(c); err != nil {
			return err
		}
	}
	return nil
}

func inputFingerprint(raw json.RawMessage) string {
	n := normalizeJSON(raw)
	sum := sha256.Sum256(n)
	return hex.EncodeToString(sum[:])
}

func normalizeExpect(raw json.RawMessage) string {
	return string(normalizeJSON(raw))
}

func normalizeJSON(raw json.RawMessage) []byte {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return raw
	}
	out, err := json.Marshal(v)
	if err != nil {
		return raw
	}
	return out
}

// ValidateIdentifier ensures task/version/source path components are safe.
func ValidateIdentifier(label, s string) error {
	if s == "" {
		return fmt.Errorf("%s required", label)
	}
	if !idPattern.MatchString(s) {
		return fmt.Errorf("invalid %s", label)
	}
	if s == ".." || s == "." {
		return fmt.Errorf("invalid %s", label)
	}
	return nil
}

// ExpectFingerprintForTest exports fingerprint logic for tests.
func ExpectFingerprintForTest(raw json.RawMessage) string {
	return normalizeExpect(raw)
}

