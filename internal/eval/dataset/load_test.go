package dataset

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoad_RejectsPathTraversal(t *testing.T) {
	t.Parallel()
	_, err := Load(LoadRequest{Task: "job_scoring", Version: "../secret", Source: SourceSynthetic})
	require.Error(t, err)
}

func TestLoad_RejectsUnknownTask(t *testing.T) {
	t.Parallel()
	_, err := Load(LoadRequest{Task: "../../etc/passwd", Version: "smoke", Source: SourceSynthetic})
	require.Error(t, err)
}

func TestValidateIdentifier_RejectsInvalid(t *testing.T) {
	t.Parallel()
	require.Error(t, ValidateIdentifier("version", "../x"))
	require.Error(t, ValidateIdentifier("version", ""))
	require.NoError(t, ValidateIdentifier("version", "smoke"))
}
