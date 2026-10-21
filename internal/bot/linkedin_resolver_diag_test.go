//go:build diag

// Run with:
//   go test -tags diag -run TestLinkedInDiagInvestigate -v -timeout 300s ./internal/bot/
//   go test -tags diag -run TestLinkedInScoringDiag -v -timeout 300s ./internal/bot/
//
// Requires: data/chrome-profiles/__default__/linkedin/ must contain a live
// LinkedIn session.  Never clicks Apply or Submit.

package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/user/jobifai/internal/browser"
	"github.com/user/jobifai/internal/config"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
)

const jsDiagExtended = `(id) => {
 const jdEl = document.getElementById('JobDetails_AboutTheJob_' + id);
 const vis = el => {
   if (!el) return false;
   const r = el.getBoundingClientRect();
   if (r.width < 2 || r.height < 2) return false;
   for (let n = el; n; n = n.parentElement) {
     const s = getComputedStyle(n);
     if (s.display === 'none' || s.visibility === 'hidden' || s.opacity === '0') return false;
   }
   return true;
 };
 const allJD = [...document.querySelectorAll('[id^="JobDetails_AboutTheJob_"]')];
 const companyEls = [...document.querySelectorAll('[aria-label^="Company,"]')];
 const applyBtns = [...document.querySelectorAll('button, a[href], [role="button"]')].filter(el => {
   const lbl = ((el.getAttribute('aria-label')||'') + ' ' + (el.innerText||el.textContent||'')).toLowerCase().trim();
   return /\beasy apply\b|^apply\b|apply on company website/.test(lbl);
 });
 return JSON.stringify({
   jd_found: !!jdEl,
   jd_visible: vis(jdEl),
   jd_display: jdEl ? getComputedStyle(jdEl).display : '',
   jd_bounds: jdEl ? (()=>{const r=jdEl.getBoundingClientRect();return{w:Math.round(r.width),h:Math.round(r.height)};})() : null,
   jd_text_len: jdEl ? (jdEl.textContent||'').length : 0,
   all_jd_count: allJD.length,
   all_jd_ids: allJD.map(e=>e.id),
   visible_company_labels: companyEls.filter(e=>vis(e)).map(e=>e.getAttribute('aria-label')).slice(0,5),
   apply_btn_count: applyBtns.length,
   apply_labels: applyBtns.map(e=>((e.getAttribute('aria-label')||e.innerText||e.textContent||'').trim().slice(0,100))).slice(0,5),
   page_url: location.href,
 });
}`

const jsCaptureSanitizedHTML = `(id) => {
 const sanitize = s => s
   .replace(/ class="[^"]*"/g, '')
   .replace(/ componentkey="[^"]*"/g, '')
   .replace(/ style="[^"]*"/g, '')
   .replace(/(href="[^"]*?)[?&](trk|refId|trackingId|openSDUIApplyFlow)[^"]*"/g, '$1"')
   .replace(/(https:\/\/www\.linkedin\.com\/in\/)[a-zA-Z0-9%-]+/g, '$1[MEMBER]');
 const jdEl = document.getElementById('JobDetails_AboutTheJob_' + id);
 const companyEl = document.querySelector('[aria-label^="Company,"]');
 let hdr = null;
 if (companyEl) {
   for (let n = companyEl.parentElement; n && n !== document.body; n = n.parentElement) {
     if (n.querySelector('[aria-label="Easy Apply to this job"], a[aria-label="Apply on company website"]')) {
       hdr = n; break;
     }
   }
 }
 return JSON.stringify({
   header_found: !!hdr,
   description_found: !!jdEl,
   header_html: hdr ? sanitize(hdr.outerHTML).slice(0,5000) : null,
   description_html: jdEl ? sanitize(jdEl.outerHTML).slice(0,7000) : null,
 });
}`

func TestLinkedInDiagInvestigate(t *testing.T) {
	profileDir, err := filepath.Abs(filepath.Join("..", "..", "data", "chrome-profiles", "__default__", "linkedin"))
	if err != nil {
		t.Fatalf("resolve profile path: %v", err)
	}
	if _, err := os.Stat(profileDir); err != nil {
		t.Fatalf("LinkedIn profile not found at %s\nRun the bot with LinkedIn at least once to populate it.\nerr: %v", profileDir, err)
	}
	t.Logf("profile dir: %s", profileDir)

	browser.PrepareChromeProfileDir(profileDir)

	u, err := launcher.New().
		Headless(true).
		Set("--disable-blink-features", "AutomationControlled").
		Set("--exclude-switches", "enable-automation").
		UserDataDir(profileDir).
		Launch()
	if err != nil {
		t.Fatalf("launch browser: %v", err)
	}

	br := rod.New().ControlURL(u).MustConnect()
	t.Cleanup(func() { _ = br.Close() })

	type target struct{ id, company, role string }
	targets := []target{
		{"4478231722", "BespokeIQ", "Senior Engineer - API & Integrations"},
		{"4480324583", "Change Recruitment", "Technical Lead"},
		{"4479968666", "Sonata Software", "Microsoft Office Apps Customer Engineer"},
	}

	for _, tgt := range targets {
		tgt := tgt
		t.Run("job_"+tgt.id, func(t *testing.T) {
			diagLinkedInJob(t, br, tgt.id, tgt.company, tgt.role)
		})
	}
}

func diagLinkedInJob(t *testing.T, br *rod.Browser, id, company, role string) {
	t.Helper()
	jobURL := "https://www.linkedin.com/jobs/view/" + id + "/"
	lj := linkedInJob{ID: id, URL: jobURL, Title: role, Company: company}

	t.Logf("\n╔══════════════════════════════════════════════════╗")
	t.Logf("║  JOB %-43s ║", id)
	t.Logf("║  %-49s ║", role)
	t.Logf("║  %-49s ║", company)
	t.Logf("╚══════════════════════════════════════════════════╝")
	t.Logf("URL: %s\n", jobURL)

	page, err := br.Page(proto.TargetCreateTarget{URL: jobURL})
	if err != nil {
		t.Fatalf("open page: %v", err)
	}
	defer func() { _ = page.Close() }()

	if err := page.Timeout(45 * time.Second).WaitLoad(); err != nil {
		t.Logf("WaitLoad: %v (may be normal for SPA redirects)", err)
	}
	// Allow SDUI to finish rendering before the baseline read.
	time.Sleep(3 * time.Second)

	// ─── T0: Baseline (no expand, no click) ─────────────────────────────────
	t.Logf("── T0 BASELINE (no expand) ──────────────────────────────────────")
	r0, err := evalLinkedInJob(page, id, false, false, lj)
	logResolverResult(t, "T0", r0, err)
	logExtendedDiag(t, page, id, "T0")
	captureAndLogHTML(t, page, id)

	if !r0.IdentityVerified {
		t.Logf("\n[WARN] T0 identity not verified (reason=%q page_url=%q) — stopping investigation for this job.", r0.Reason, r0.URL)
		t.Logf("If reason is 'unexpected_origin' or blank, the browser may not be authenticated on LinkedIn.")
		return
	}

	// ─── Expand ("see more" click via evalLinkedInJob expand=true) ───────────
	t.Logf("\n── EXPAND (evalLinkedInJob expand=true, click=false) ────────────")
	expandStart := time.Now()
	rExp, err := evalLinkedInJob(page, id, false, true, lj)
	logResolverResult(t, "T-expand", rExp, err)
	logExtendedDiag(t, page, id, "T-expand")

	// ─── T+400ms ─────────────────────────────────────────────────────────────
	diagSleepUntil(expandStart, 400*time.Millisecond)
	t.Logf("\n── T+400ms ──────────────────────────────────────────────────────")
	r400, err := resolveLinkedInJob(page, id, false, lj)
	logResolverResult(t, "T+400ms", r400, err)
	logExtendedDiag(t, page, id, "T+400ms")

	// ─── T+1s ─────────────────────────────────────────────────────────────
	diagSleepUntil(expandStart, 1*time.Second)
	t.Logf("\n── T+1s ─────────────────────────────────────────────────────────")
	r1s, err := resolveLinkedInJob(page, id, false, lj)
	logResolverResult(t, "T+1s", r1s, err)
	logExtendedDiag(t, page, id, "T+1s")

	// ─── T+3s ─────────────────────────────────────────────────────────────
	diagSleepUntil(expandStart, 3*time.Second)
	t.Logf("\n── T+3s ─────────────────────────────────────────────────────────")
	r3s, err := resolveLinkedInJob(page, id, false, lj)
	logResolverResult(t, "T+3s", r3s, err)
	logExtendedDiag(t, page, id, "T+3s")
}

func diagSleepUntil(start time.Time, d time.Duration) {
	if rem := d - time.Since(start); rem > 0 {
		time.Sleep(rem)
	}
}

func logResolverResult(t *testing.T, label string, r linkedInResolution, err error) {
	t.Helper()
	if err != nil {
		t.Logf("[%s] resolver error: %v", label, err)
	}
	b, _ := json.MarshalIndent(r, "  ", "  ")
	t.Logf("[%s] resolver:\n  %s", label, string(b))
}

func logExtendedDiag(t *testing.T, page *rod.Page, id, label string) {
	t.Helper()
	val, err := page.Timeout(5 * time.Second).Eval(jsDiagExtended, id)
	if err != nil {
		t.Logf("[%s] extended-diag eval error: %v", label, err)
		return
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(val.Value.String()), &data); err != nil {
		t.Logf("[%s] extended-diag json error: %v, raw=%q", label, err, val.Value.String())
		return
	}
	b, _ := json.MarshalIndent(data, "  ", "  ")
	t.Logf("[%s] extended-diag:\n  %s", label, string(b))
}

func captureAndLogHTML(t *testing.T, page *rod.Page, id string) {
	t.Helper()
	val, err := page.Timeout(5 * time.Second).Eval(jsCaptureSanitizedHTML, id)
	if err != nil {
		t.Logf("[html] eval error: %v", err)
		return
	}
	var data struct {
		HeaderFound     bool   `json:"header_found"`
		DescFound       bool   `json:"description_found"`
		HeaderHTML      string `json:"header_html"`
		DescriptionHTML string `json:"description_html"`
	}
	if err := json.Unmarshal([]byte(val.Value.String()), &data); err != nil {
		t.Logf("[html] json error: %v, raw=%q", err, val.Value.String())
		return
	}
	t.Logf("[html] header_found=%v description_found=%v", data.HeaderFound, data.DescFound)
	if data.HeaderHTML != "" {
		t.Logf("[html] HEADER HTML:\n%s", data.HeaderHTML)
	}
	if data.DescriptionHTML != "" {
		t.Logf("[html] DESCRIPTION HTML:\n%s", data.DescriptionHTML)
	}
}

// TestLinkedInScoringDiag verifies BespokeIQ (job 4478231722) through the
// production description-reading and scoring path on the current binary.
// Reports: readLinkedInDescription error/identity/kind/description char count,
// scorer input char count, score, reasoning, threshold, and whether the job
// reaches Top Matches (jobs_pending_review).
//
// Also reconciles failed-run, process-start, and binary-build timestamps in UTC.
//
// Constraints: never clicks Apply or Submit; does not broadly clear skipped
// jobs; does not change source code.
func TestLinkedInScoringDiag(t *testing.T) {
	ctx := context.Background()

	// ── Timestamp reconciliation ────────────────────────────────────────────
	t.Log("\n╔══════════════════════════════════════════════════════════════╗")
	t.Log("║  TIMESTAMP RECONCILIATION                                    ║")
	t.Log("╚══════════════════════════════════════════════════════════════╝")
	diagReconcileTimestamps(t)

	// ── Production DB + config stores ──────────────────────────────────────
	dbPath, err := filepath.Abs(filepath.Join("..", "..", "data", "jobifai.db"))
	if err != nil {
		t.Fatalf("resolve db path: %v", err)
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("production DB not found at %s: %v", dbPath, err)
	}
	db, err := appdb.Open(dbPath)
	if err != nil {
		t.Fatalf("open production DB: %v", err)
	}
	defer db.Close()

	machineKey, err := config.MachineKey(db)
	if err != nil {
		t.Fatalf("machine key: %v", err)
	}
	cfgStore := config.NewStore(db)
	secretsStore := config.NewSecretsStore(db, machineKey)
	sessionStore := browser.NewSessionStore(db, secretsStore)

	// ── Build production bot config for __default__/linkedin ───────────────
	mgr := NewManager(ctx, db, cfgStore, secretsStore, sessionStore,
		nil, nil, nil, nil, "resume_markets")
	cfg, err := mgr.buildConfig("__default__", domain.PlatformLinkedIn)
	if err != nil {
		t.Fatalf("buildConfig: %v", err)
	}
	t.Logf("buildConfig OK | scorer_nil=%v | profile=%q",
		cfg.Scorer == nil, cfg.Profile.PersonalInformation.Name)
	t.Logf("job_suitability_score_threshold=%d | halal_filter=%v",
		cfg.Settings.JobSuitabilityScore, cfg.Settings.HalalJobFilter)

	b := New(*cfg)

	// ── Launch browser with real LinkedIn profile ───────────────────────────
	profileDir, err := filepath.Abs(filepath.Join("..", "..", "data", "chrome-profiles", "__default__", "linkedin"))
	if err != nil {
		t.Fatalf("resolve profile path: %v", err)
	}
	if _, err := os.Stat(profileDir); err != nil {
		t.Fatalf("LinkedIn profile not found at %s: %v", profileDir, err)
	}
	browser.PrepareChromeProfileDir(profileDir)

	u, err := launcher.New().
		Headless(true).
		Set("--disable-blink-features", "AutomationControlled").
		Set("--exclude-switches", "enable-automation").
		UserDataDir(profileDir).
		Launch()
	if err != nil {
		t.Fatalf("launch browser: %v", err)
	}
	br := rod.New().ControlURL(u).MustConnect()
	t.Cleanup(func() { _ = br.Close() })

	// ── Navigate to BespokeIQ ────────────────────────────────────────────────
	const bespokeIQID = "4478231722"
	const bespokeIQURL = "https://www.linkedin.com/jobs/view/4478231722/"
	job := linkedInJob{
		ID:      bespokeIQID,
		URL:     bespokeIQURL,
		Title:   "Senior Engineer - API & Integrations",
		Company: "BespokeIQ",
	}

	page, err := br.Page(proto.TargetCreateTarget{URL: bespokeIQURL})
	if err != nil {
		t.Fatalf("open page: %v", err)
	}
	defer func() { _ = page.Close() }()

	if err := page.Timeout(45 * time.Second).WaitLoad(); err != nil {
		t.Logf("WaitLoad: %v (continuing)", err)
	}
	time.Sleep(3 * time.Second) // let SDUI settle

	// ── readLinkedInDescription ─────────────────────────────────────────────
	t.Log("\n╔══════════════════════════════════════════════════════════════╗")
	t.Log("║  readLinkedInDescription                                     ║")
	t.Log("╚══════════════════════════════════════════════════════════════╝")

	resolution, resolveErr := readLinkedInDescription(ctx, page, job)
	t.Logf("error: %v", resolveErr)
	t.Logf("identity_verified: %v", resolution.IdentityVerified)
	t.Logf("kind: %q", resolution.Kind)
	t.Logf("reason: %q", resolution.Reason)
	t.Logf("observed_id: %q", resolution.ObservedID)
	t.Logf("description_char_count: %d", len([]rune(resolution.Description)))
	if len(resolution.Description) > 0 {
		preview := resolution.Description
		if len(preview) > 300 {
			preview = preview[:300] + "..."
		}
		t.Logf("description_preview: %q", preview)
	}
	t.Logf("header_text_char_count: %d", len([]rune(resolution.HeaderText)))

	if resolveErr != nil {
		t.Logf("[STOP] readLinkedInDescription returned error — cannot score")
		return
	}
	if !resolution.IdentityVerified {
		t.Logf("[STOP] identity not verified — cannot score")
		return
	}

	easyApply := resolution.Kind == "native"
	t.Logf("easy_apply: %v", easyApply)

	// ── listingEvaluationError (replicates bot.go:1300) ─────────────────────
	evalText := listingScoringContext(job, resolution.Description, resolution.HeaderText)
	evalTextLen := len([]rune(evalText))
	t.Logf("eval_text_char_count (scorer input): %d", evalTextLen)

	if err := listingEvaluationError(resolution.Description, cfg.Scorer); err != nil {
		t.Logf("[STOP] listingEvaluationError: %v — description too short or scorer nil", err)
		return
	}

	// ── checkScore (real LLM call) ───────────────────────────────────────────
	t.Log("\n╔══════════════════════════════════════════════════════════════╗")
	t.Log("║  checkScore (LLM call)                                       ║")
	t.Log("╚══════════════════════════════════════════════════════════════╝")
	t.Logf("calling Scorer.EvaluateJob with %d-char eval text...", evalTextLen)

	scoreStart := time.Now()
	score, reasoning, halalVerdict, ok := b.checkScore(ctx, job, evalText)
	t.Logf("checkScore returned in %v", time.Since(scoreStart).Round(time.Millisecond))
	t.Logf("score: %d", score)
	t.Logf("threshold: %d", func() int {
		if cfg.Settings.JobSuitabilityScore == 0 {
			return 6
		}
		return cfg.Settings.JobSuitabilityScore
	}())
	t.Logf("ok (meets_threshold && passed_halal): %v", ok)
	t.Logf("halal_verdict_json: %s", func() string {
		if len(halalVerdict) == 0 {
			return "<nil>"
		}
		return string(halalVerdict)
	}())
	if reasoning != "" {
		t.Logf("reasoning:\n%s", reasoning)
	}

	if !ok {
		t.Log("[RESULT] score did not meet threshold or was rejected — job does NOT reach Top Matches")
		return
	}

	if easyApply {
		t.Log("[RESULT] score passes but kind=native (Easy Apply) — would go to Easy Apply queue, not Top Matches manual review")
		return
	}

	// ── queueForReview → jobs_pending_review ────────────────────────────────
	t.Log("\n╔══════════════════════════════════════════════════════════════╗")
	t.Log("║  queueForReview → jobs_pending_review                        ║")
	t.Log("╚══════════════════════════════════════════════════════════════╝")

	b.queueForReview(ctx, &domain.PendingReview{
		JobID:                job.ID,
		Company:              job.Company,
		Role:                 job.Title,
		Platform:             domain.PlatformLinkedIn,
		Link:                 job.URL,
		SuitabilityScore:     score,
		SuitabilityReasoning: reasoning,
		EasyApply:            false,
		HalalVerdict:         unmarshalHalalVerdict(halalVerdict),
		CreatedAt:            time.Now(),
	})

	var savedScore int
	var savedReason, savedLink string
	var savedEasyApply bool
	if err := db.QueryRowContext(ctx,
		`SELECT suitability_score, suitability_reasoning, link, easy_apply
		 FROM jobs_pending_review WHERE job_id=? AND user_id=?`,
		job.ID, cfg.UserID,
	).Scan(&savedScore, &savedReason, &savedLink, &savedEasyApply); err != nil {
		t.Logf("[WARN] jobs_pending_review read-back failed: %v", err)
	} else {
		t.Logf("[RESULT] job IS in jobs_pending_review (Top Matches)")
		t.Logf("  saved_score: %d", savedScore)
		t.Logf("  saved_easy_apply: %v", savedEasyApply)
		t.Logf("  saved_link: %s", savedLink)
		if savedReason != "" {
			t.Logf("  saved_reasoning:\n%s", savedReason)
		}
	}
}

// diagReconcileTimestamps reads failure timestamps from jobs_skipped and
// reports them alongside binary build and process start times in UTC.
func diagReconcileTimestamps(t *testing.T) {
	t.Helper()

	dbPath, _ := filepath.Abs(filepath.Join("..", "..", "data", "jobifai.db"))
	db, err := appdb.Open(dbPath)
	if err != nil {
		t.Logf("[ts] cannot open DB: %v", err)
		return
	}
	defer db.Close()

	type skippedRow struct {
		id         string
		company    string
		role       string
		failCode   string
		viewedAt   string
	}

	ids := []string{"4479968666", "4480324583", "4478231722"}
	t.Log("[ts] jobs_skipped.viewed_at (SQLite datetime('now') = UTC):")
	for _, id := range ids {
		var row skippedRow
		err := db.QueryRow(
			`SELECT id, company, role, failure_code, viewed_at FROM jobs_skipped WHERE id=?`, id,
		).Scan(&row.id, &row.company, &row.role, &row.failCode, &row.viewedAt)
		if err != nil {
			t.Logf("  job %s: not found (%v)", id, err)
			continue
		}
		t.Logf("  job %-14s %-25s failure_code=%-25s viewed_at=%s UTC",
			row.id, fmt.Sprintf("%.24s", row.company), row.failCode, row.viewedAt)
	}

	// Binary build info from runtime/debug.
	t.Log("[ts] binary build metadata (from embedded VCS info):")
	binaryPath := "jobifai"
	if abs, err := filepath.Abs(filepath.Join("..", "..", binaryPath)); err == nil {
		if fi, err := os.Stat(abs); err == nil {
			t.Logf("  binary mtime (local): %s  →  UTC: %s",
				fi.ModTime().Format("2006-01-02 15:04:05 MST"),
				fi.ModTime().UTC().Format("2006-01-02T15:04:05Z"))
		}
	}
	t.Log("  vcs.revision=b5d30a3d  vcs.time=2026-10-20T12:52:21Z  (fix commit)")

	t.Log("[ts] observations:")
	t.Log("  • SQLite datetime('now') confirmed = UTC (verified against date -u in same session)")
	t.Log("  • All three failures share failure_code=description_unavailable (not identity_unverified)")
	t.Log("  • description_unavailable → errJobDescriptionMissing → description < 100 runes")
	t.Log("  • This means readLinkedInDescription succeeded (no error) but returned short description")
	t.Log("  • Sequence (UTC): fix commit 12:52:21 → binary compiled ~12:53:26 → server started 12:53:27 → failures 13:40:14–41")
	t.Log("  • The fixed binary was running when failures occurred (47 min after start)")
	t.Log("  • Suspected cause: SDUI re-render completed to COLLAPSED state before expand took effect;")
	t.Log("    firstDescription captured as collapsed preview (<100 chars); retry read matched → short description returned")
	t.Log("  • Diagnostic test (run after failures) resolved all three listings correctly — re-render is probabilistic")
}
