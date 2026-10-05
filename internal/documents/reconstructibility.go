package documents

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/user/jobifai/internal/domain"
)

// ValidateReconstructionInputs conservatively checks every persisted input needed
// to recreate a PDF before retention can remove its last durable copy.
func ValidateReconstructionInputs(kind, content, css, renderer, snapshot string) error {
	if strings.TrimSpace(css) == "" || renderer == "" || strings.TrimSpace(snapshot) == "" {
		return fmt.Errorf("missing immutable render inputs")
	}
	if err := AssertSupportedRenderer(renderer); err != nil {
		return err
	}
	snap, err := ParseRenderSnapshot(snapshot)
	if err != nil {
		return err
	}
	if snap.RendererVersion == "" || snap.RendererVersion != renderer {
		return fmt.Errorf("missing or inconsistent snapshot renderer")
	}
	switch kind {
	case ContentResumeJSON:
		var c ResumeContent
		if err := json.Unmarshal([]byte(content), &c); err != nil {
			return err
		}
		return domain.ValidateResumeContent(&c.Profile)
	case ContentCoverText:
		var c CoverContent
		if err := json.Unmarshal([]byte(content), &c); err != nil {
			return err
		}
		if strings.TrimSpace(c.Body) == "" {
			return fmt.Errorf("missing cover content")
		}
		return nil
	default:
		return fmt.Errorf("content kind cannot be reconstructed")
	}
}
