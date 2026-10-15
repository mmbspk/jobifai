package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/user/jobifai/internal/auth"
	"github.com/user/jobifai/internal/bot"
	jobdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/documents"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/quota"
)

// E2EResourceFixtureHandlers exposes resource-metering test seams when JOBIFAI_E2E=1.
type E2EResourceFixtureHandlers struct{ svc *Services }

// NewE2EResourceFixtureHandlers constructs the handler group. Call only when JOBIFAI_E2E=1.
func NewE2EResourceFixtureHandlers(svc *Services) *E2EResourceFixtureHandlers {
	return &E2EResourceFixtureHandlers{svc: svc}
}

// ─── e2eMeterService ──────────────────────────────────────────────────────────

// e2eMeterService wraps the real resource.Service and acknowledges each
// RecordBrowserIncrement call via ackCh so the fixture can synchronize
// with the liveBrowserMeter's tick goroutine.
type e2eMeterService struct {
	real  bot.MeterService
	ackCh chan struct{} // buffered(1) — send never blocks on the first Activate call
}

func (e *e2eMeterService) BeforeBrowserIncrement(ctx context.Context, userID string) error {
	return e.real.BeforeBrowserIncrement(ctx, userID)
}

func (e *e2eMeterService) RecordBrowserIncrement(ctx context.Context, userID, idempotencyKey, automationRunID, jobID string, durationMS int64) error {
	err := e.real.RecordBrowserIncrement(ctx, userID, idempotencyKey, automationRunID, jobID, durationMS)
	// Signal acknowledgment regardless of error so the fixture never hangs waiting
	// for a RecordBrowserIncrement that already failed.
	select {
	case e.ackCh <- struct{}{}:
	default:
	}
	return err
}

func (e *e2eMeterService) BrowserIncrementDuration() (time.Duration, error) {
	return e.real.BrowserIncrementDuration()
}

// ─── quotaBurner (local interface for seed-llm-event transactional burn) ─────

type e2eQuotaBurner interface {
	PrepareTransactionalBurnStrict(userID string, credits int64) (quota.BurnPrepare, error)
	CommitTransactionalBurn(tx *sql.Tx, userID string, credits int64, prep quota.BurnPrepare) (int64, error)
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func (h *E2EResourceFixtureHandlers) assertE2EUser(userID string) error {
	if h.svc.Users == nil {
		return fmt.Errorf("user store unavailable")
	}
	u, err := h.svc.Users.ByID(userID)
	if err != nil {
		return fmt.Errorf("user not found: %w", err)
	}
	if !strings.HasSuffix(u.Email, "@e2e.test") {
		return fmt.Errorf("user_id does not belong to an @e2e.test account")
	}
	return nil
}

// ─── Admin-only handlers ──────────────────────────────────────────────────────

// POST /api/e2e/resource/setup-metering — sets deterministic test economics.
// CreditsPerUSD=1000, ServiceMarkup=0.5; Browser=2c/increment; Render=3c/render.
func (h *E2EResourceFixtureHandlers) SetupMetering(w http.ResponseWriter, r *http.Request) {
	if !e2eFixturesEnabled() {
		notFound(w, "not found")
		return
	}
	if h.svc.Quota == nil || h.svc.Resource == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"message": "quota or resource service not available"})
		return
	}

	if err := h.svc.Quota.SaveDefaults(domain.QuotaDefaults{
		EnforcementDefault: true,
		CreditsPerUSD:      1000,
		ServiceMarkup:      0.5,
		TrialCredits:       500,
		TrialDays:          7,
	}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "failed to save quota defaults: " + err.Error()})
		return
	}

	if err := h.svc.Resource.SaveDefaults(domain.ResourceMeteringDefaults{
		Enabled: true,
		Browser: domain.BrowserMeteringConfig{
			Enabled:                true,
			RawCostUSDMicroPerHour: 180_000,
			BillingIncrementSecs:   30,
		},
		Render: domain.RenderMeteringConfig{
			Enabled:                  true,
			RawCostUSDMicroPerRender: 2_000,
		},
	}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "failed to save resource defaults: " + err.Error()})
		return
	}

	browserCredits, err := h.svc.Resource.EstimateBrowserIncrementCredits()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "failed to estimate browser credits: " + err.Error()})
		return
	}
	renderCredits, err := h.svc.Resource.EstimateRenderCredits()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "failed to estimate render credits: " + err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                      true,
		"browser_increment_credits": browserCredits,
		"render_credits":          renderCredits,
		"billing_increment_secs":  30,
	})
}

// POST /api/e2e/resource/reset-metering-config — deletes persisted metering
// configuration to restore the "unreviewed" state for lifecycle tests.
func (h *E2EResourceFixtureHandlers) ResetMeteringConfig(w http.ResponseWriter, r *http.Request) {
	if !e2eFixturesEnabled() {
		notFound(w, "not found")
		return
	}
	for _, key := range []string{"resource_metering_defaults", "quota_defaults"} {
		if _, err := h.svc.DB.ExecContext(r.Context(),
			`DELETE FROM settings WHERE user_id = ? AND key = ?`,
			domain.SystemUserID, key); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "failed to reset " + key + ": " + err.Error()})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// POST /api/e2e/resource/set-user-credits — overwrites a test user's trial balance.
func (h *E2EResourceFixtureHandlers) SetUserCredits(w http.ResponseWriter, r *http.Request) {
	if !e2eFixturesEnabled() {
		notFound(w, "not found")
		return
	}
	var req struct {
		UserID  string `json:"user_id"`
		Credits int64  `json:"credits"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid body"})
		return
	}
	if err := h.assertE2EUser(req.UserID); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}
	if h.svc.Quota == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"message": "quota service not available"})
		return
	}
	ctx := r.Context()
	if err := h.svc.Quota.InitTrial(ctx, req.UserID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "init trial failed: " + err.Error()})
		return
	}
	if _, err := h.svc.DB.ExecContext(ctx,
		`UPDATE user_quota SET trial_remaining_micro = ? WHERE user_id = ?`,
		req.Credits, req.UserID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "update balance failed: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "credits": req.Credits})
}

// POST /api/e2e/resource/seed-llm-event — inserts an AI usage event with a real
// transactional quota burn. Returns 402 when the user's balance is insufficient.
func (h *E2EResourceFixtureHandlers) SeedLLMEvent(w http.ResponseWriter, r *http.Request) {
	if !e2eFixturesEnabled() {
		notFound(w, "not found")
		return
	}
	var req struct {
		UserID  string `json:"user_id"`
		Credits int64  `json:"credits"`
		Task    string `json:"task"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid body"})
		return
	}
	if req.Task == "" {
		req.Task = "e2e_seed"
	}
	if err := h.assertE2EUser(req.UserID); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}
	if h.svc.Quota == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"message": "quota service not available"})
		return
	}

	burner, ok := h.svc.Quota.(e2eQuotaBurner)
	if !ok {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"message": "quota service does not expose transactional burn"})
		return
	}

	prep, err := burner.PrepareTransactionalBurnStrict(req.UserID, req.Credits)
	if errors.Is(err, quota.ErrExceeded) {
		writeJSON(w, http.StatusPaymentRequired, map[string]string{"message": "insufficient credits"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "quota check failed: " + err.Error()})
		return
	}

	tx, err := h.svc.DB.BeginTx(r.Context(), nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "begin tx failed: " + err.Error()})
		return
	}
	defer func() { _ = tx.Rollback() }()

	capturedPrep := prep
	eventID, err := jobdb.RecordBillingTx(tx, jobdb.LLMUsageEventInput{
		UserID:         req.UserID,
		Task:           req.Task,
		Provider:       "e2e",
		RequestedModel: "e2e-seed",
		ActualModel:    "e2e-seed",
		CreditsBurned:  req.Credits,
		Success:        true,
		LogicalOp:      true, // required so RecordBillingTx calls the quota burn callback
	}, req.Credits, func(tx *sql.Tx, userID string, credits int64) error {
		_, burnErr := burner.CommitTransactionalBurn(tx, userID, credits, capturedPrep)
		return burnErr
	})
	if err != nil {
		if errors.Is(err, quota.ErrExceeded) {
			writeJSON(w, http.StatusPaymentRequired, map[string]string{"message": "insufficient credits"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "record billing failed: " + err.Error()})
		return
	}

	if err := tx.Commit(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "commit failed: " + err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"credits_burned": req.Credits,
		"event_id":      eventID,
	})
}

// ─── User-authenticated handlers ──────────────────────────────────────────────

// POST /api/e2e/resource/run-browser-session — drives a liveBrowserMeter with
// manually-controlled tick delivery for deterministic billing tests.
func (h *E2EResourceFixtureHandlers) RunBrowserSession(w http.ResponseWriter, r *http.Request) {
	if !e2eFixturesEnabled() {
		notFound(w, "not found")
		return
	}
	var req struct {
		RunID   string   `json:"run_id"`
		Actions []string `json:"actions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid body"})
		return
	}
	if req.RunID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "run_id required"})
		return
	}
	if h.svc.Resource == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"message": "resource service not available"})
		return
	}

	ms, ok := h.svc.Resource.(bot.MeterService)
	if !ok {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"message": "resource service does not implement MeterService"})
		return
	}

	increment, err := ms.BrowserIncrementDuration()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "failed to get billing increment: " + err.Error()})
		return
	}

	userID := auth.UserIDFromCtx(r.Context())
	ackCh := make(chan struct{}, 1)
	tickCh := make(chan time.Time) // unbuffered — receiver must be in runLoop goroutine
	goroutineStoppedCh := make(chan struct{}, 1)

	wrapper := &e2eMeterService{real: ms, ackCh: ackCh}
	newTicker := func(_ time.Duration) (<-chan time.Time, func()) {
		return tickCh, func() {
			select {
			case goroutineStoppedCh <- struct{}{}:
			default:
			}
		}
	}
	meter := bot.NewLiveBrowserMeter(wrapper, userID, req.RunID, increment, newTicker)

	var (
		bucketsCharged       int
		ticksDropped         int
		meterErrStr          string
		billingCtxCancelled  bool // true only when billing ctx cancelled due to quota exhaustion
	)

	ctx := r.Context()
	for _, action := range req.Actions {
		switch action {
		case "activate":
			activateErr := meter.Activate(ctx)
			// Drain the buffered ack left by the synchronous RecordBrowserIncrement call.
			// If BeforeBrowserIncrement failed, RecordBrowserIncrement was not called — no ack.
			select {
			case <-ackCh:
				if activateErr == nil {
					bucketsCharged++
				}
			default:
				// BeforeBrowserIncrement failed — meter.Activate returned an error.
			}
			if activateErr != nil && meterErrStr == "" {
				if errors.Is(activateErr, quota.ErrExceeded) {
					meterErrStr = "quota_exceeded"
				} else {
					meterErrStr = activateErr.Error()
				}
			}

		case "deactivate":
			meter.Deactivate()
			// Wait for the runLoop goroutine to fully exit before any tick_expect_dropped.
			select {
			case <-goroutineStoppedCh:
			case <-time.After(2 * time.Second):
				writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "timeout waiting for goroutine stop after deactivate"})
				return
			}

		case "tick":
			// Deliver a tick to the running goroutine.
			select {
			case tickCh <- time.Now():
			case <-time.After(2 * time.Second):
				writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "timeout delivering tick (meter goroutine not running?)"})
				return
			}
			// Wait for ack (RecordBrowserIncrement called) or billing context cancellation (quota exhausted).
			select {
			case <-ackCh:
				bucketsCharged++
			case <-meter.BillingCtx().Done():
				billingCtxCancelled = true
				if meterErrStr == "" {
					if errors.Is(meter.Err(), quota.ErrExceeded) {
						meterErrStr = "quota_exceeded"
					} else if meter.Err() != nil {
						meterErrStr = meter.Err().Error()
					}
				}
			case <-time.After(2 * time.Second):
				writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "timeout waiting for tick acknowledgment"})
				return
			}

		case "tick_expect_dropped":
			// Safe only after deactivate confirmed goroutine stopped (goroutineStoppedCh consumed).
			// A non-blocking send to tickCh: if nobody receives it, the tick was dropped correctly.
			select {
			case tickCh <- time.Now():
				// Somebody received it — the goroutine is still running, unexpected.
				writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "tick was NOT dropped — goroutine still running when tick_expect_dropped was executed"})
				return
			default:
				ticksDropped++
			}

		case "stop":
			meter.Stop()

		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "unknown action: " + action})
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"buckets_charged":             bucketsCharged,
		"ticks_dropped":               ticksDropped,
		"err":                         meterErrStr,
		"billing_ctx_cancelled":       billingCtxCancelled,
		"destructive_action_executed": bot.DestructiveActionAllowed(meter),
	})
}

// POST /api/e2e/resource/materialize-application-docs — invokes the production
// MaterializeApplicationDocs path with a frozen saved resume and skipped cover.
// Proves that selecting an existing version in the review flow reuses the artifact
// without creating a new render billing event.
func (h *E2EResourceFixtureHandlers) MaterializeApplicationDocs(w http.ResponseWriter, r *http.Request) {
	if !e2eFixturesEnabled() {
		notFound(w, "not found")
		return
	}
	var req struct {
		VersionID string `json:"version_id"`
		JobID     string `json:"job_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid body"})
		return
	}
	if req.VersionID == "" || req.JobID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "version_id and job_id required"})
		return
	}
	userID := auth.UserIDFromCtx(r.Context())

	res := documents.ResolveResult{
		Resume: documents.ResolvedDocument{
			Kind:      documents.KindResume,
			Outcome:   domain.DocumentOutcomeLocalFile,
			VersionID: req.VersionID,
		},
		Cover: documents.ResolvedDocument{
			Kind:    documents.KindCoverLetter,
			Outcome: domain.DocumentOutcomeSkipped,
			Skip:    true,
		},
	}

	tmpDir, err := os.MkdirTemp("", "e2e-matdocs-")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "temp dir failed: " + err.Error()})
		return
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	deps := documents.MaterializeDeps{
		ExportUpload: func(uid, jid, vid, kind string, payload documents.VersionUploadPayload) (string, error) {
			if len(payload.Data) == 0 {
				return "", fmt.Errorf("empty payload")
			}
			p := filepath.Join(tmpDir, kind+"-"+vid+".pdf")
			if err := os.WriteFile(p, payload.Data, 0600); err != nil {
				return "", err
			}
			return p, nil
		},
	}

	out, err := h.svc.Documents.MaterializeApplicationDocs(
		r.Context(),
		userID, req.JobID, "E2E Co", "E2E Role", "e2e job description",
		res, domain.DocumentPolicies{}, documents.RenderContext{Market: "e2e"},
		nil,
		deps,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "materialize failed: " + err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         !out.Hold,
		"has_resume": out.ResumePath != "",
	})
}

// POST /api/e2e/resource/materialize-document — calls PDFBytes to prove
// existing artifact cache-hit returns data without new billing.
func (h *E2EResourceFixtureHandlers) MaterializeDocument(w http.ResponseWriter, r *http.Request) {
	if !e2eFixturesEnabled() {
		notFound(w, "not found")
		return
	}
	var req struct {
		VersionID string `json:"version_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid body"})
		return
	}
	userID := auth.UserIDFromCtx(r.Context())

	data, _, _, err := h.svc.Documents.PDFBytes(r.Context(), userID, req.VersionID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "materialize failed: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"size_bytes": len(data),
	})
}

// POST /api/e2e/resource/evict-document-artifact — marks the artifact for a
// version as evicted so reconstruction tests can verify no re-billing occurs.
func (h *E2EResourceFixtureHandlers) EvictDocumentArtifact(w http.ResponseWriter, r *http.Request) {
	if !e2eFixturesEnabled() {
		notFound(w, "not found")
		return
	}
	var req struct {
		VersionID string `json:"version_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid body"})
		return
	}
	userID := auth.UserIDFromCtx(r.Context())
	ctx := r.Context()

	// Verify the version belongs to the authenticated user and find its artifact.
	var artifactID string
	err := h.svc.DB.QueryRowContext(ctx, `
		SELECT r.artifact_id
		FROM document_version_artifact_refs r
		JOIN document_content_versions v ON v.id = r.content_version_id
		WHERE r.content_version_id = ? AND v.user_id = ?`,
		req.VersionID, userID,
	).Scan(&artifactID)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "artifact not found for this version and user"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "artifact lookup failed: " + err.Error()})
		return
	}

	if _, err := h.svc.DB.ExecContext(ctx,
		`UPDATE document_render_artifacts SET evicted_at = datetime('now') WHERE id = ?`,
		artifactID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "eviction failed: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// POST /api/e2e/resource/fail-next-render — injects a deterministic render
// failure for the calling user's next PDF generation call.
func (h *E2EResourceFixtureHandlers) FailNextRender(w http.ResponseWriter, r *http.Request) {
	if !e2eFixturesEnabled() {
		notFound(w, "not found")
		return
	}
	userID := auth.UserIDFromCtx(r.Context())
	h.svc.Documents.E2ESetFailNextRender(userID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// POST /api/e2e/resource/record-render-once — atomically records one document
// render billing event with real idempotency protection.
func (h *E2EResourceFixtureHandlers) RecordRenderOnce(w http.ResponseWriter, r *http.Request) {
	if !e2eFixturesEnabled() {
		notFound(w, "not found")
		return
	}
	var req struct {
		IdempotencyKey string `json:"idempotency_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid body"})
		return
	}
	if req.IdempotencyKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "idempotency_key required"})
		return
	}
	if h.svc.Resource == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"message": "resource service not available"})
		return
	}

	userID := auth.UserIDFromCtx(r.Context())
	if err := h.svc.Resource.RecordRender(r.Context(), userID, req.IdempotencyKey); err != nil {
		if errors.Is(err, quota.ErrExceeded) {
			writeJSON(w, http.StatusPaymentRequired, map[string]string{"message": "insufficient credits"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
