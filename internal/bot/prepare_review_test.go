package bot

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/documents"
	"github.com/user/jobifai/internal/domain"
)

func TestApplyPrimaryPrepareNavigationPolicy(t *testing.T) {
	t.Parallel()
	require.True(t, applyPrimaryIsSubmit("submit"))
	require.False(t, applyPrimaryAllowsPrepareNav("submit"))
	require.True(t, applyPrimaryAllowsPrepareNav("next"))
	require.True(t, applyPrimaryAllowsPrepareNav("continue"))
	require.True(t, applyPrimaryAllowsPrepareNav("review"))
}

func TestPoliciesRequireVerifiedDocuments_TailorResume(t *testing.T) {
	t.Parallel()
	var svc documents.Service
	b := &Bot{cfg: Config{Documents: &svc}}
	b.cfg.Settings.DocumentPolicies.ResumeMode = domain.ResumeDocumentModeTailorJob
	require.True(t, b.policiesRequireVerifiedDocuments())
}
