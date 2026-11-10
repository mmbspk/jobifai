package bot_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/bot"
)

// TestPrepareSuccessWrite_OlderRunCannotOverwriteNewer locks in operation ownership: the
// prepID-gated terminal write used by PrepareReviewDocuments must affect zero rows when a
// newer run has reclaimed the job, so a stale run cannot clobber the newer selection/result.
func TestPrepareSuccessWrite_OlderRunCannotOverwriteNewer(t *testing.T) {
	db := openTestDB(t)
	const userID, jobID = "u-own", "job-own"
	createUser(t, db, userID)

	_, err := db.Exec(`INSERT INTO jobs_pending_review
		(job_id,user_id,company,role,platform,link,easy_apply,suitability_score,created_at,
		 prepare_status,document_refs_json)
		VALUES (?,?,?,?,?,?,1,9,datetime('now'),?,?)`,
		jobID, userID, "Talent", "Principal Platform", "linkedin", "https://example.com/j",
		"running:NEW", `{"hold_reason":"NEW"}`)
	require.NoError(t, err)

	// This mirrors the gated success write in PrepareReviewDocuments.
	gated := `UPDATE jobs_pending_review
		 SET resume_path = ?, cover_letter_path = ?,
		     resume_content_version_id = ?, cover_letter_content_version_id = ?,
		     document_refs_json = ?
		 WHERE job_id = ? AND user_id = ? AND prepare_status = 'running:' || ?`

	// A stale run (prepID=OLD) must not overwrite anything.
	res, err := db.Exec(gated, "/tmp/old.pdf", "", "v-old", "", `{"hold_reason":"STALE OVERWRITE"}`, jobID, userID, "OLD")
	require.NoError(t, err)
	n, _ := res.RowsAffected()
	require.Equal(t, int64(0), n, "stale prepID must affect zero rows")

	var pack string
	require.NoError(t, db.QueryRow(`SELECT document_refs_json FROM jobs_pending_review WHERE job_id=? AND user_id=?`, jobID, userID).Scan(&pack))
	require.Equal(t, `{"hold_reason":"NEW"}`, pack, "newer selection/result must be untouched by a stale run")

	// The current owner (prepID=NEW) updates exactly one row.
	res, err = db.Exec(gated, "/tmp/new.pdf", "", "v-new", "", `{"prepared":true}`, jobID, userID, "NEW")
	require.NoError(t, err)
	n, _ = res.RowsAffected()
	require.Equal(t, int64(1), n, "current prepID must update its row")
}

// TestPrepareDiagnosticsReachBotRunLogs proves that structured prepare-flow diagnostics,
// emitted under a registered run, are persisted to bot_run_logs (not just stdout): the
// upload-identity hold (with its stage) and the terminal "prepare: run finished" event.
func TestPrepareDiagnosticsReachBotRunLogs(t *testing.T) {
	db := openTestDB(t)
	sink := bot.NewRunLogSink(db)
	const userID, runID = "u-diag", "prepare-abc123"
	createUser(t, db, userID)

	require.NoError(t, sink.Register(context.Background(), userID, runID))

	write := func(level, msg string, extra map[string]any, logID string) {
		m := map[string]any{
			"level":             level,
			"user_id":           userID,
			"automation_run_id": runID,
			"log_id":            logID,
			"message":           msg,
			"time":              time.Now().Format(time.RFC3339),
		}
		for k, v := range extra {
			m[k] = v
		}
		b, _ := json.Marshal(m)
		_, _ = sink.Write(b)
	}

	// The upload-identity hold carries the stable stage as structured data.
	write("warn", "linkedin: upload identity unverifiable",
		map[string]any{"stage": "accepted_filename_not_visible", "filename": "resume_2b6a6874.pdf"}, "log-diag-1")
	// The terminal event closes the run (info level, now dashboard-visible).
	write("info", "prepare: run finished",
		map[string]any{"stage": "accepted_filename_not_visible", "scan_complete": false}, "log-diag-2")

	var count int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM bot_run_logs WHERE user_id=? AND run_id=?`, userID, runID).Scan(&count))
	require.Equal(t, 2, count, "both the hold diagnostic and the terminal event must be persisted")

	var rawHold string
	require.NoError(t, db.QueryRow(
		`SELECT raw_json FROM bot_run_logs WHERE user_id=? AND message LIKE '%upload identity unverifiable%'`,
		userID).Scan(&rawHold))
	require.Contains(t, rawHold, "accepted_filename_not_visible", "the stage must be preserved in persisted storage")
	require.Contains(t, rawHold, "resume_2b6a6874.pdf", "the filename must be preserved in persisted storage")

	var terminalCount int
	require.NoError(t, db.QueryRow(
		`SELECT count(*) FROM bot_run_logs WHERE user_id=? AND message LIKE 'prepare: run finished%'`,
		userID).Scan(&terminalCount))
	require.Equal(t, 1, terminalCount, "the terminal prepare event must be persisted")
}
