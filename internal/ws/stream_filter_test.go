package ws_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	jobws "github.com/user/jobifai/internal/ws"
)

func TestVisibleOnDashboard_ProductionCustomer(t *testing.T) {
	t.Parallel()
	assert.False(t, jobws.VisibleOnDashboard(map[string]any{"level": "debug", "message": "seek: url"}))
	assert.False(t, jobws.VisibleOnDashboard(map[string]any{"level": "info", "llm_call": true, "message": "llm: claude"}))
	assert.False(t, jobws.VisibleOnDashboard(map[string]any{"level": "info", "event": "llm_call", "message": "done"}))
	assert.False(t, jobws.VisibleOnDashboard(map[string]any{"level": "info", "message": "database ready"}))

	assert.True(t, jobws.VisibleOnDashboard(map[string]any{"level": "info", "message": "seek: found 3 jobs for \"engineer\", processing"}))
	assert.True(t, jobws.VisibleOnDashboard(map[string]any{"level": "info", "message": "linkedin: score 8/10, \"Role\" @ Co"}))
	assert.True(t, jobws.VisibleOnDashboard(map[string]any{"level": "error", "message": "seek: reconnect failed, stopping"}))
	assert.True(t, jobws.VisibleOnDashboard(map[string]any{"level": "error", "message": "browser launch failed"}))
	assert.True(t, jobws.VisibleOnDashboard(map[string]any{"level": "warn", "message": "automation paused due to quota"}))
}
