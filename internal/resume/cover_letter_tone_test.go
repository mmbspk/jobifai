package resume_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/resume"
)

func TestBuildCoverLetterMessages_DefaultTone_NoToneInstruction(t *testing.T) {
	msgs, err := resume.BuildCoverLetterMessages(&domain.ResumeProfile{}, "accounting manager role", "")
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	assert.NotContains(t, msgs[0].Content, "TONE:", "empty tone must not inject a tone instruction")
}

func TestBuildCoverLetterMessages_NamedTone_InjectsInstruction(t *testing.T) {
	for _, tone := range []string{"formal", "conversational", "confident"} {
		t.Run(tone, func(t *testing.T) {
			msgs, err := resume.BuildCoverLetterMessages(&domain.ResumeProfile{}, "accounting manager role", tone)
			require.NoError(t, err)
			require.Len(t, msgs, 2)
			sys := msgs[0].Content
			assert.True(t, strings.Contains(sys, "TONE:"), "system prompt must contain TONE: for tone %q", tone)
			assert.Contains(t, sys, tone)
		})
	}
}
