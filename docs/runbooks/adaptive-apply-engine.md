# Adaptive Apply Engine — Operator Runbook

Epic #127. Stories #128–#136.

**Production default:** Legacy engine. The adaptive engine requires explicit opt-in; existing installs are unaffected.

---

## 1. What this feature is

The adaptive apply engine replaces the legacy static form-filler with a three-pass pipeline:

1. **Deterministic resolve** — standard fields (name, email, phone, location, notice period, salary) are matched directly from the candidate profile without an LLM call.
2. **Mapping store** — fields seen in a prior verified run are served from SQLite without an LLM call. Cold runs pay the LLM; warm runs (same form signature) are free.
3. **LLM batch** — remaining unresolved fields go to a single batched LLM call.

Each form step is captured as a **privacy-minimized snapshot** (`SnapField` with opaque refs `f0`…`fN`; no candidate data stored). Verified resolutions are written back to `form_field_mappings` for future warm runs.

The engine covers LinkedIn Easy Apply and Greenhouse-hosted external applications. SEEK and other external ATS platforms fall back to legacy behavior. Production default remains Legacy.

---

## 2. Database migrations

Two new migrations ship with this feature:

| Migration | File | Change |
|-----------|------|--------|
| 054 | `054_llm_engine_dimensions.sql` | Adds `engine`, `apply_attempt_id`, `platform` columns to `llm_usage_events` |
| 055 | `055_form_field_mappings.sql` | Creates `form_field_mappings` table for the mapping store |

Both migrations run automatically on startup (`db.Open` calls goose). They are additive-only (no column drops, no data loss) and are safe to apply to an existing install.

**Rollback:** Remove the two columns from `llm_usage_events` with `ALTER TABLE … DROP COLUMN` (requires SQLite ≥ 3.35). The `form_field_mappings` table can be dropped with `DROP TABLE form_field_mappings`. The binary must be downgraded to the pre-epic-127 version simultaneously.

---

## 3. Configuration

### 3.1 Engine mode (admin UI)

Navigate to **Settings → AI Apply Engine**. Three modes:

| Mode | Behavior |
|------|----------|
| `legacy` (default) | Unchanged behavior. No adaptive pipeline runs. Safe for production. |
| `shadow` | Runs legacy path; additionally captures snapshots for analysis. No mapping writes. Read-only. |
| `adaptive` | Full adaptive pipeline. Pilot users only (enforced by `IsInPilot` check). |

The admin switch is reversible at any time and takes effect on the next apply attempt. An in-flight attempt is not interrupted; it completes under the engine mode that was active when the attempt started.

A **kill switch** re-reads the engine mode before each irreversible action (submit click). If the mode was reverted to Legacy during a run, the attempt is cancelled with outcome `cancelled` / `kill_switch_reverted`.

### 3.2 External ATS (Greenhouse)

External ATS follow-through is disabled by default. To enable it for adaptive-mode runs, set `Config.EnableExternalATS = true` in the bot configuration.

**Verification before enabling:**
- The Greenhouse tenant slug in the apply URL must fuzzy-match the declared employer (e.g., `acmeinc` matches "Acme Inc.").
- Requisition ID in the URL must be stable across any redirect chain.
- If either check fails, the attempt aborts with `errATSTenantMismatch` or `errATSJobIDMismatch` and the job is NOT submitted.

---

## 4. LLM cost per apply attempt

Approximate token costs based on a typical LinkedIn Easy Apply form (5–8 steps, 3–6 fields per step):

| Path | Input tokens | Output tokens | Notes |
|------|-------------|---------------|-------|
| Cold run (all LLM) | ~800–1200 | ~200–400 | First run on a new form layout |
| Warm run (all mapping) | 0 | 0 | Form seen before by this user or globally |
| Mixed (some mapping) | ~300–600 | ~100–200 | Some fields cached, some new |

A single LLM call is made per step that has unresolved fields; steps with no unresolved fields make no LLM call. The `llm_usage_events` table records `engine`, `apply_attempt_id`, and `platform` for cost attribution.

---

## 5. Operational invariants

The adaptive engine enforces the following invariants (all tested; failures abort the attempt):

1. **Shadow mode cannot issue browser actions.** `shadowEasyApply()` / `shadowSeekApply()` return `errShadowBrowserAction` immediately if routing sends shadow to the adaptive path.
2. **Submit intent is persisted before click.** `SubmitGuard.PersistIntent()` writes `submit_dispatched` to the attempt row before the submit button is clicked. A crash between persist and click produces `outcome=submission_unknown`, which the reconciler handles.
3. **External ATS tenant verification runs before navigation.** Candidate data is never sent to an unverified employer.
4. **Kill switch is re-read before submit.** Engine mode is re-checked atomically before the irreversible submit click.
5. **Mapping store is read-only in shadow mode.** `DBMappingStore.ReadOnly = true` in shadow runs; `Write()` is a no-op.

---

## 6. Rollback procedure

### Hot rollback (no data loss)

1. In admin UI: change engine mode from `adaptive` to `legacy`.
2. The kill switch fires for any in-flight adaptive attempt and cancels it with `outcome=cancelled`.
3. No database changes needed.

### Full rollback (downgrade binary)

1. Stop the service.
2. Deploy the pre-epic-127 binary.
3. The two new migrations (054, 055) are ignored by the old binary (goose tracks versions; the old binary won't see them).
4. The `form_field_mappings` table remains in SQLite harmlessly (no schema conflict).
5. The new columns in `llm_usage_events` are ignored by the old INSERT (they have DEFAULT values).

---

## 7. Monitoring

| Event | Log field | Action |
|-------|-----------|--------|
| Adaptive step paused | `msg="adaptive: step paused"`, `reason=…` | Inspect `form_field_mappings` for the snapshot sig; consider invalidating stale entries. |
| External ATS tenant mismatch | `msg="external_ats: Greenhouse tenant mismatch"` | Verify employer name in job listing matches Greenhouse URL. No submission occurred. |
| External ATS CAPTCHA | `errATSCaptcha` | Greenhouse blocked the browser. Mark attempt for manual review. |
| Kill switch reverted | `outcome=cancelled`, `code=kill_switch_reverted` | Engine was switched mid-run. Expected behavior. |
| Submit unknown | `outcome=submission_unknown` | Submit was clicked but confirmation absent. Requires manual verification. |

---

## 8. Limitations and outstanding live-validation needs

The following are confirmed limitations of the current release. They do not block deployment but should be tracked:

1. **SEEK adaptive apply not implemented.** SEEK Quick Apply with the adaptive engine falls back to the legacy SEEK runner. Adaptive SEEK support is a follow-on task.
2. **Greenhouse forms only.** External ATS follow-through supports only `boards.greenhouse.io` and `job-boards.greenhouse.io`. Workday, Lever, Smartrecruiters, etc. are unsupported and are queued for manual review as before.
3. **Multi-page Greenhouse forms not validated.** The form runner supports multi-step forms in principle, but live-validation against a real Greenhouse employer has not been performed. The `NavigateNext` and `IsSubmitReady` implementations use CSS selectors (`#submit_app`, `.btn-primary`) that may not match every Greenhouse template.
4. **LLM proposal validation is strict.** Fields with options that do not exactly match the LLM proposal (case-insensitive) fall back to `PendingReview`. This is conservative; some forms use unusual option text. Invalid proposals are logged for analysis.
5. **No CAPTCHA solving.** CAPTCHA-protected pages are detected and the attempt is aborted. This applies to both LinkedIn SDUI bot-protection and Greenhouse Cloudflare protection.
6. **Shadow mode observation analysis.** Shadow mode captures snapshots and runs the interpreter without filling. The captured data (form field distributions, LLM proposals, mapping hit rates) is not yet surfaced in the admin UI. Manual inspection via `form_field_mappings` table is required for analysis.
7. **Cost report baseline.** The LLM cost estimates in section 4 are based on typical form layouts; they have not been validated against a production traffic sample. Enable shadow mode on a subset of users first to gather real token counts before activating adaptive mode.

---

## 9. Pre-activation checklist

Before switching any user from `legacy` to `adaptive`:

- [ ] Run `make lint` — zero new errors from adaptive-engine files.
- [ ] Run `make test` — all Go unit tests pass.
- [ ] Run `make test-web` — frontend unit tests pass.
- [ ] Verify migrations 054 and 055 applied cleanly on the target environment.
- [ ] Confirm `EngineMode` defaults to `legacy` in production config.
- [ ] Confirm `EnableExternalATS` is `false` in production config.
- [ ] Enable shadow mode for 1–2 pilot accounts for ≥ 48 h; review `llm_usage_events` for engine=shadow rows.
- [ ] Review `form_field_mappings` for shadow-mode observation quality.
- [ ] Switch 1 pilot account to `adaptive`; monitor `apply_attempts` outcomes for 1 week.
- [ ] Verify no `submission_unknown` outcomes without corresponding confirmed applications.
- [ ] Review LLM cost per attempt against budget.
- [ ] Document any new blocked-state patterns discovered in live runs and add detection to `checkATSBlockedState`.
