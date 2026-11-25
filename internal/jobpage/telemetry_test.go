package jobpage

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestReaderTelemetryKeepsCapturesOwnedBoundedAndSeparate(t *testing.T) {
	store, _ := recipeStoreFixture(t)
	ctx := context.Background()
	s := fixtureSnapshot()
	s.Target.UserID = "reader-user"
	e := ReadEvent{UserID: "reader-user", Scope: s.Scope, Source: SourceModel, Metrics: Metrics{ProposalRequests: 1, VerificationRequests: 1, HTTPAttempts: 2, InputTokens: 100, OutputTokens: 20, CostMicroUSD: 1000, CostKnown: true}}
	_, err := store.RecordRead(ctx, e, &s)
	require.Error(t, err, "capture opt-in is required")
	c, err := store.GetConfig(ctx)
	require.NoError(t, err)
	c.PrivateCaptures = true
	_, err = store.SetConfig(ctx, "reader-admin", c.Revision, c)
	require.NoError(t, err)
	for i := 0; i < 25; i++ {
		_, err = store.RecordRead(ctx, e, &s)
		require.NoError(t, err)
	}
	var count int
	require.NoError(t, store.DB.QueryRow("SELECT COUNT(*) FROM job_page_private_captures WHERE user_id='reader-user'").Scan(&count))
	require.Equal(t, 20, count)
	events, err := store.ListReadEvents(ctx, "reader-user", 100)
	require.NoError(t, err)
	require.Len(t, events, 25)
	require.Equal(t, int64(100), events[0].Metrics.InputTokens)
	var raw string
	require.NoError(t, store.DB.QueryRow("SELECT metrics_json FROM job_page_read_events ORDER BY id DESC LIMIT 1").Scan(&raw))
	require.NotContains(t, raw, "description")
	require.NotContains(t, raw, "Fixture")
	var id int64
	require.NoError(t, store.DB.QueryRow("SELECT MAX(id) FROM job_page_private_captures").Scan(&id))
	_, err = store.GetPrivateCapture(ctx, "other-user", id)
	require.ErrorIs(t, err, ErrForbidden)
	capture, err := store.GetPrivateCapture(ctx, "reader-user", id)
	require.NoError(t, err)
	require.Contains(t, string(capture.SnapshotJSON), "supported current job description")
	_, err = store.GetPrivateCapture(ctx, "reader-admin", id)
	require.NoError(t, err)
	_, err = store.DB.Exec("UPDATE job_page_private_captures SET expires_at_ns=? WHERE id=?", time.Now().Add(-time.Hour).UnixNano(), id)
	require.NoError(t, err)
	_, err = store.GetPrivateCapture(ctx, "reader-user", id)
	require.ErrorIs(t, err, ErrForbidden)
	_, err = store.RecordRead(ctx, e, nil)
	require.NoError(t, err)
	require.NoError(t, store.DB.QueryRow("SELECT COUNT(*) FROM job_page_private_captures WHERE id=?", id).Scan(&count))
	require.Zero(t, count)
}

func TestReaderTelemetryRejectsCrossOwnerAndRollsBackPersistenceFailure(t *testing.T) {
	store, _ := recipeStoreFixture(t)
	ctx := context.Background()
	s := fixtureSnapshot()
	s.Target.UserID = "other-user"
	e := ReadEvent{UserID: "reader-user", Scope: s.Scope}
	c, err := store.GetConfig(ctx)
	require.NoError(t, err)
	c.PrivateCaptures = true
	_, err = store.SetConfig(ctx, "reader-admin", c.Revision, c)
	require.NoError(t, err)
	_, err = store.RecordRead(ctx, e, &s)
	require.ErrorIs(t, err, ErrForbidden)
	s.Target.UserID = e.UserID
	s.Screenshot = make([]byte, DefaultLimits().MaxImageBytes+1)
	_, err = store.RecordRead(ctx, e, &s)
	require.Equal(t, FailureBudget, ErrorCode(err))
	s.Screenshot = nil
	_, err = store.DB.Exec("DROP TABLE job_page_private_captures")
	require.NoError(t, err)
	_, err = store.RecordRead(ctx, e, &s)
	require.Equal(t, FailurePersistence, ErrorCode(err))
	var count int
	require.NoError(t, store.DB.QueryRow("SELECT COUNT(*) FROM job_page_read_events").Scan(&count))
	require.Zero(t, count)
	e.RecordID = strings.Repeat("x", 129)
	_, err = store.RecordRead(ctx, e, nil)
	require.Error(t, err)
}
