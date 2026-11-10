package documents

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJobApplicationRelRef_AbsoluteAndRelative(t *testing.T) {
	t.Parallel()
	abs := "/Users/me/proj/job_applications/__default__/4484428531/v/resume_d7f99e3f.pdf"
	require.Equal(t, "job_applications/__default__/4484428531/v/resume_d7f99e3f.pdf", JobApplicationRelRef(abs))
	require.Equal(t, "__default__/4484428531/v/resume_d7f99e3f.pdf", JobApplicationFileURLSuffix(abs))
	require.Equal(t, "job_applications/u/j/r.pdf", JobApplicationRelRef("job_applications/u/j/r.pdf"))
}
