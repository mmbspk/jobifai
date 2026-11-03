package documents

import (
	"strings"
	"testing"
)

// TestApplicationPack_HoldStageRoundTrip verifies the structured diagnostic fields
// (HoldStage/HoldFile) and runtime form capabilities survive a serialize→parse round trip,
// so a failed preparation preserves the exact stage and real form requirements instead of
// collapsing to "form requirements unknown".
func TestApplicationPack_HoldStageRoundTrip(t *testing.T) {
	pack := ApplicationDocumentPack{
		HoldReason: "linkedin: resume upload identity could not be verified — please confirm selection manually",
		HoldStage:  "accepted_filename_not_visible",
		HoldFile:   "resume_2b6a6874.pdf",
		FormCaps:   FormDocumentCapabilities{Detected: true, ResumeFileSlots: 1},
		Selection:  ApplicationDocumentSelection{ResumeVersionID: "2b6a6874-baa1-412f-a5c1-0e88684b0f77"},
	}
	raw := WriteApplicationPackJSON(pack)
	if raw == "" {
		t.Fatal("pack with a hold stage must serialize to non-empty JSON")
	}
	if !strings.Contains(raw, "hold_stage") || !strings.Contains(raw, "hold_file") {
		t.Fatalf("serialized pack must include structured diagnostic fields: %s", raw)
	}

	got := ParseApplicationPackJSON(raw)
	if got.HoldStage != pack.HoldStage {
		t.Errorf("HoldStage: want %q, got %q", pack.HoldStage, got.HoldStage)
	}
	if got.HoldFile != pack.HoldFile {
		t.Errorf("HoldFile: want %q, got %q", pack.HoldFile, got.HoldFile)
	}
	if !got.FormCaps.Detected || got.FormCaps.ResumeFileSlots != 1 {
		t.Errorf("FormCaps must survive round trip, got %+v", got.FormCaps)
	}
	if got.Selection.ResumeVersionID != pack.Selection.ResumeVersionID {
		t.Errorf("Selection must survive round trip, got %+v", got.Selection)
	}
	if got.HoldReason != pack.HoldReason {
		t.Errorf("HoldReason must survive round trip, got %q", got.HoldReason)
	}
}
