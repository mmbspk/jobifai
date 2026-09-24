package handler

import (
	"net/http"
	"os"
	"time"

	"github.com/user/jobifai/internal/auth"
)

// E2EFixtureHandlers exposes test-only job seeding when JOBIFAI_E2E=1.
type E2EFixtureHandlers struct{ svc *Services }

func NewE2EFixtureHandlers(svc *Services) *E2EFixtureHandlers {
	return &E2EFixtureHandlers{svc: svc}
}

func e2eFixturesEnabled() bool {
	return os.Getenv("JOBIFAI_E2E") == "1"
}

type e2eSeedJobsResponse struct {
	Applied  int `json:"applied"`
	Skipped  int `json:"skipped"`
	Pending  int `json:"pending"`
	CannotApply int `json:"cannot_apply"`
}

// POST /api/e2e/seed-jobs — inserts a fixed fixture set for the authenticated user.
func (h *E2EFixtureHandlers) SeedJobs(w http.ResponseWriter, r *http.Request) {
	if !e2eFixturesEnabled() {
		notFound(w, "not found")
		return
	}
	if h.svc.DB == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "database not available"})
		return
	}

	userID := auth.UserIDFromCtx(r.Context())
	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := h.svc.DB.BeginTx(r.Context(), nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	defer func() { _ = tx.Rollback() }()

	applied := []struct {
		id, company, role, platform, link string
	}{
		{"e2e-applied-1", "Northwind Traders", "Operations Coordinator", "seek", "https://example.com/jobs/e2e-applied-1"},
		{"e2e-applied-2", "Contoso Ltd", "Client Success Lead", "linkedin", "https://example.com/jobs/e2e-applied-2"},
	}
	for _, row := range applied {
		_, err = tx.ExecContext(r.Context(),
			`INSERT OR REPLACE INTO jobs_applied(id,user_id,platform,company,role,link,applied_at)
			 VALUES(?,?,?,?,?,?,?)`,
			row.id, userID, row.platform, row.company, row.role, row.link, now,
		)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
			return
		}
	}

	skipped := []struct {
		id, company, role, platform, link, reason string
	}{
		{"e2e-skipped-1", "Fabrikam", "Analyst", "seek", "https://example.com/jobs/e2e-skipped-1", "Below suitability threshold"},
	}
	for _, row := range skipped {
		_, err = tx.ExecContext(r.Context(),
			`INSERT OR REPLACE INTO jobs_skipped(id,user_id,platform,company,role,link,skip_reason,viewed_at)
			 VALUES(?,?,?,?,?,?,?,?)`,
			row.id, userID, row.platform, row.company, row.role, row.link, row.reason, now,
		)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
			return
		}
	}

	cannotApply := []struct {
		id, company, role, platform, link string
	}{
		{"e2e-cannot-1", "Manual Steps Co", "Project Lead", "linkedin", "https://example.com/jobs/e2e-cannot-1"},
	}
	for _, row := range cannotApply {
		_, err = tx.ExecContext(r.Context(),
			`INSERT OR REPLACE INTO jobs_skipped(id,user_id,platform,company,role,link,skip_reason,suitability_score,viewed_at)
			 VALUES(?,?,?,?,?,?,?,?,?)`,
			row.id, userID, row.platform, row.company, row.role, row.link,
			"easy apply: additional steps required", 8, now,
		)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
			return
		}
	}

	pending := []struct {
		id, company, role, platform, link string
		score                               int
	}{
		{"e2e-pending-1", "Review Gate Inc", "Program Manager", "seek", "https://example.com/jobs/e2e-pending-1", 9},
	}
	for _, row := range pending {
		_, err = tx.ExecContext(r.Context(),
			`INSERT OR REPLACE INTO jobs_pending_review(job_id,user_id,company,role,platform,link,resume_path,cover_letter_path,suitability_score,easy_apply,created_at)
			 VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
			row.id, userID, row.company, row.role, row.platform, row.link, "", "", row.score, 1, now,
		)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
			return
		}
	}

	if err := tx.Commit(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, e2eSeedJobsResponse{
		Applied:     len(applied),
		Skipped:     len(skipped),
		Pending:     len(pending),
		CannotApply: len(cannotApply),
	})
}

// POST /api/e2e/clear-jobs — removes fixture rows for the authenticated user.
func (h *E2EFixtureHandlers) ClearJobs(w http.ResponseWriter, r *http.Request) {
	if !e2eFixturesEnabled() {
		notFound(w, "not found")
		return
	}
	if h.svc.DB == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "database not available"})
		return
	}

	userID := auth.UserIDFromCtx(r.Context())
	prefixes := []string{"e2e-applied-%", "e2e-skipped-%", "e2e-cannot-%", "e2e-pending-%"}
	for _, p := range prefixes {
		if _, err := h.svc.DB.ExecContext(r.Context(), `DELETE FROM jobs_applied WHERE user_id = ? AND id LIKE ?`, userID, p); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
			return
		}
		if _, err := h.svc.DB.ExecContext(r.Context(), `DELETE FROM jobs_skipped WHERE user_id = ? AND id LIKE ?`, userID, p); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
			return
		}
		if _, err := h.svc.DB.ExecContext(r.Context(), `DELETE FROM jobs_pending_review WHERE user_id = ? AND job_id LIKE ?`, userID, p); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
			return
		}
	}

	okMsg(w, "cleared")
}
