package bot

import (
	"errors"
	"testing"
)

// TestUploadIdentityHold_StageAndWrapping verifies the typed upload-verification hold:
// it preserves errors.Is(…, errUploadIdentityUnverifiable) (so existing control flow is
// unchanged), exposes the stable stage + filename via errors.As (for durable diagnostics),
// and keeps a human-readable Error() with no raw "stage=" text (for the UI).
func TestUploadIdentityHold_StageAndWrapping(t *testing.T) {
	err := error(&uploadIdentityHold{stage: uploadStageAcceptedNotVisible, filename: "resume_2b6a6874.pdf"})

	if !errors.Is(err, errUploadIdentityUnverifiable) {
		t.Fatal("typed hold must satisfy errors.Is(err, errUploadIdentityUnverifiable)")
	}

	var h *uploadIdentityHold
	if !errors.As(err, &h) {
		t.Fatal("typed hold must be extractable via errors.As")
	}
	if h.stage != uploadStageAcceptedNotVisible {
		t.Errorf("stage: want %q, got %q", uploadStageAcceptedNotVisible, h.stage)
	}
	if h.filename != "resume_2b6a6874.pdf" {
		t.Errorf("filename: want %q, got %q", "resume_2b6a6874.pdf", h.filename)
	}

	// Error() must be the readable sentinel text, not raw diagnostic markup.
	if err.Error() != errUploadIdentityUnverifiable.Error() {
		t.Errorf("Error() must be the readable message, got %q", err.Error())
	}
	if got := err.Error(); strContainsAny(got, "stage=", "file=", uploadStageAcceptedNotVisible) {
		t.Errorf("Error() must not leak raw diagnostic markup: %q", got)
	}
}

func strContainsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
	}
	return false
}
