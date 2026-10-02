package documents

import (
	"encoding/json"
	"fmt"

	"github.com/user/jobifai/internal/resume"
)

// SupportedRendererVersions lists renderer_version values this binary can reconstruct.
var SupportedRendererVersions = []string{RendererVersion}

type RenderSnapshot struct {
	RendererVersion string               `json:"renderer_version"`
	SectionLabels   resume.SectionLabels `json:"section_labels"`
}

func BuildRenderSnapshot(market, marketDir string) (RenderSnapshot, error) {
	snap := RenderSnapshot{RendererVersion: RendererVersion}
	if market != "" && marketDir != "" {
		m := resume.LoadMarketByName(marketDir, market)
		if m != nil {
			snap.SectionLabels = m.LabelsOrDefault()
		} else {
			snap.SectionLabels = resume.DefaultSectionLabels()
		}
	} else {
		snap.SectionLabels = resume.DefaultSectionLabels()
	}
	return snap, nil
}

func (s RenderSnapshot) JSON() (string, error) {
	b, err := json.Marshal(s)
	return string(b), err
}

func ParseRenderSnapshot(raw string) (RenderSnapshot, error) {
	if raw == "" {
		return RenderSnapshot{}, fmt.Errorf("missing render snapshot")
	}
	var s RenderSnapshot
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return RenderSnapshot{}, err
	}
	return s, nil
}

func AssertSupportedRenderer(version string) error {
	if version == "" {
		version = RendererVersion
	}
	for _, v := range SupportedRendererVersions {
		if v == version {
			return nil
		}
	}
	return fmt.Errorf("unsupported renderer version %q (supported: %v)", version, SupportedRendererVersions)
}

func RenderOptsFromSnapshot(s RenderSnapshot) *resume.RenderOptions {
	return &resume.RenderOptions{SectionLabels: s.SectionLabels}
}
