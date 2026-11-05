package bot

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFilterPageValidationMessages_DropsSuccessToasts(t *testing.T) {
	t.Parallel()
	require.Nil(t, filterPageValidationMessages([]string{"Resume uploaded successfully"}))
	require.Equal(t, []string{"Enter a valid phone number"},
		filterPageValidationMessages([]string{"Resume uploaded successfully", "Enter a valid phone number"}))
}
