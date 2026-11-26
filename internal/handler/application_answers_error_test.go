package handler

import (
	"errors"
	"fmt"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/applicationanswers"
	"net/http/httptest"
	"testing"
)

func TestAnswerErrorDistinguishesValidationAndInfrastructure(t *testing.T) {
	for _, test := range []struct {
		err      error
		status   int
		contains string
	}{{fmt.Errorf("%w: invalid topic", applicationanswers.ErrInvalidInput), 400, "invalid topic"}, {errors.New("database secret connection details"), 500, "Could not complete"}, {applicationanswers.ErrConflict, 409, ""}} {
		w := httptest.NewRecorder()
		answerError(w, test.err)
		require.Equal(t, test.status, w.Code)
		require.Contains(t, w.Body.String(), test.contains)
		require.NotContains(t, w.Body.String(), "secret connection")
	}
}
