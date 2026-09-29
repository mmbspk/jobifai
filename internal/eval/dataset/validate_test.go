package dataset

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateBundle_ConflictingExpectationsSameInput(t *testing.T) {
	t.Parallel()
	in := json.RawMessage(`{"title":"Analyst","company":"Example Co","description":"Permissible services"}`)
	b := Bundle{
		Manifest: Manifest{Task: "employment_ethics", SchemaVersion: SchemaVersion},
		Cases: []Case{
			{ID: "a", Task: "employment_ethics", Input: in, Expect: json.RawMessage(`{"verdict":"HALAL"}`)},
			{ID: "b", Task: "employment_ethics", Input: in, Expect: json.RawMessage(`{"verdict":"HARAM"}`)},
		},
	}
	err := ValidateBundle(b)
	require.Error(t, err)
	require.Contains(t, err.Error(), "conflicting expectations")
}

func TestValidateBundle_DuplicateCaseID(t *testing.T) {
	t.Parallel()
	b := Bundle{
		Cases: []Case{
			{ID: "dup", Input: json.RawMessage(`{"x":1}`), Expect: json.RawMessage(`{}`)},
			{ID: "dup", Input: json.RawMessage(`{"x":2}`), Expect: json.RawMessage(`{}`)},
		},
	}
	require.Error(t, ValidateBundle(b))
}
