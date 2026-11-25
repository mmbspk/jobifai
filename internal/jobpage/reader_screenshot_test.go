package jobpage

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

type optionalVisionModel struct {
	*orchestrationModel
	vision bool
}

func (m *optionalVisionModel) SupportsScreenshot() bool { return m.vision }

type ownedScreenshotPage struct {
	*orchestrationPage
	screenshots int
}

func (p *ownedScreenshotPage) ScreenshotFor(context.Context, Target, Snapshot, Limits) ([]byte, error) {
	p.screenshots++
	return []byte("owned fixture image"), nil
}
func TestReaderSuppliesOwnedScreenshotOnlyToSupportedModel(t *testing.T) {
	for _, vision := range []bool{false, true} {
		t.Run(map[bool]string{false: "text", true: "vision"}[vision], func(t *testing.T) {
			s := readSnapshot("123", "A current and complete supported role with technical requirements.")
			p := &ownedScreenshotPage{orchestrationPage: &orchestrationPage{snapshot: s}}
			m := &optionalVisionModel{vision: vision, orchestrationModel: &orchestrationModel{onPropose: func(_ context.Context, observed Snapshot) (Plan, error) {
				if vision {
					require.NotEmpty(t, observed.Screenshot)
				} else {
					require.Empty(t, observed.Screenshot)
				}
				return Plan{Version: SnapshotVersion, SnapshotID: observed.ID, DescriptionRefs: observed.Candidates}, nil
			}}}
			reader := NewReader(nil, enabledReaderConfig, func(context.Context, Limits) (Model, error) { return m, nil })
			_, err := reader.Read(context.Background(), p, s.Target)
			require.NoError(t, err)
			if vision {
				require.Equal(t, 1, p.screenshots)
			} else {
				require.Zero(t, p.screenshots)
			}
		})
	}
}
