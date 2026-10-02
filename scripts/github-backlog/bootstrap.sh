#!/usr/bin/env bash
# One-time bootstrap: labels, GitHub Project, epics, stories (sub-issues).
# Safe to re-run only on empty repo issues; skips if epic label issues exist.
set -euo pipefail

REPO="mmbspk/jobifai"
OWNER="@me"

existing_epics="$(gh issue list --repo "$REPO" --label epic --state all --limit 1 --json number --jq 'length')"
if [[ "$existing_epics" != "0" ]]; then
  echo "Epic issues already exist on $REPO — aborting to avoid duplicates."
  echo "Open: https://github.com/$REPO/issues?q=label%3Aepic"
  exit 1
fi

create_label() {
  local name="$1" color="$2" desc="$3"
  gh label create "$name" --repo "$REPO" --color "$color" --description "$desc" 2>/dev/null || true
}

create_label epic "5319E7" "Epic — outcome-sized bucket; stories are sub-issues"
create_label story "0E8A16" "User story — shippable slice of an epic"
create_label "area:automation" "1D76DB" "Bot, platforms, form filling"
create_label "area:llm" "BFDADC" "Eval, policies, LLM tasks"
create_label "area:billing" "FBCA04" "Stripe, quota, credits"
create_label "area:product" "D4C5F9" "End-user UX flows"
create_label "area:platform-eng" "C5DEF5" "CI, deploy, tests, docs"
create_label icebox "EDEDED" "Ideas captured for later — not scheduled"

project_json="$(gh project create --owner "$OWNER" --title "jobifai Roadmap" --format json)"
project_num="$(echo "$project_json" | jq -r '.number')"
project_url="$(echo "$project_json" | jq -r '.url')"
echo "Created project #$project_num — $project_url"

repo_id="$(gh api graphql -f query='query($o:String!,$n:String!){repository(owner:$o,name:$n){id}}' -f o=mmbspk -f n=jobifai --jq '.data.repository.id')"

create_issue() {
  local title="$1"
  shift
  local url num id
  url="$(gh issue create --repo "$REPO" "$@" --title "$title")"
  num="${url##*/}"
  id="$(gh issue view "$num" --repo "$REPO" --json id --jq .id)"
  jq -n --arg id "$id" --arg url "$url" --argjson number "$num" '{id: $id, url: $url, number: $number}'
}

add_to_project() {
  local url="$1"
  if ! gh project item-add "$project_num" --owner "$OWNER" --url "$url" >/dev/null 2>&1; then
    : # already on project
  fi
}

link_subissue() {
  local parent_id="$1"
  local child_id="$2"
  gh api graphql -f query='mutation($p:ID!,$c:ID!){addSubIssue(input:{issueId:$p,subIssueId:$c}){subIssue{id}}}' \
    -f p="$parent_id" -f c="$child_id" >/dev/null
}

# --- Epic 1 ---
ep1="$(create_issue "Epic: Job platform automation (LinkedIn & Seek)" \
  -l epic -l "area:automation" \
  -b "$(cat <<'EOF'
## Outcome
Users can run a reliable end-to-end loop on **LinkedIn** and **Seek**: search → score → tailor documents → fill forms → apply or queue for review, with clear handling when automation cannot finish.

## Scope (repo anchors)
- Runners: `internal/bot/bot.go` (LinkedIn), `internal/bot/seek.go`, `registerRunner` in `internal/bot/bot.go`
- Orchestration: `internal/bot/manager.go`, `apply_routing.go`, `form.go`
- Platforms enum: `internal/domain/types.go` (`SupportedPlatforms`)

## Consolidation rule
All LinkedIn/Seek bot bugs, Easy Apply edge cases, search targets, session warm-up, and apply-by-URL work **belongs in stories under this epic** — not separate one-off issues.

## Success signals
- Fewer cannot-apply entries for known fixable reasons
- Stable sessions via Settings → Platforms / noVNC Docker path
- Documented platform-specific limits in user guide
EOF
)")"
ep1_id="$(echo "$ep1" | jq -r '.id')"
ep1_url="$(echo "$ep1" | jq -r '.url')"
add_to_project "$ep1_url"

s="$(create_issue "Story: LinkedIn runner — search, Easy Apply, and session lifecycle" \
  -l story -l "area:automation" \
  -b "$(cat <<'EOF'
## Context
LinkedIn logic lives primarily in `internal/bot/bot.go` (`runLinkedIn`). Manager special-cases LinkedIn in `internal/bot/manager.go` (setup, apply-url, cookies).

## Acceptance criteria
- [ ] Search respects preferences (titles, locations, filters) with regression tests where feasible
- [ ] Easy Apply path: score → halal (if enabled) → tailor → review/submit behaves consistently
- [ ] Session save/load and warm-up cookie persistence documented and reliable for local + Docker/noVNC
- [ ] Known failure modes surface actionable bot logs (zerolog) and user-visible status on Dashboard

## Out of scope
- New platforms (see Epic: New job platforms)
- LLM model changes (see Epic: LLM eval & policies)
EOF
)")"; link_subissue "$ep1_id" "$(echo "$s" | jq -r '.id')"; add_to_project "$(echo "$s" | jq -r '.url')"

s="$(create_issue "Story: Seek runner — search, apply detection, and AU location handling" \
  -l story -l "area:automation" \
  -b "$(cat <<'EOF'
## Context
`internal/bot/seek.go`, location formatting `internal/domain/location.go`, Seek-specific manager branches.

## Acceptance criteria
- [ ] Search + apply flows parity with LinkedIn where product promises parity
- [ ] Apply detection tests (`seek_apply_detect_test.go`) extended for regressions found in production
- [ ] Remote debug / browser port behavior clear for Seek when not using embedded browser
- [ ] Top matches / manual apply handoff documented for Seek-only listings

## Related tests
`internal/bot/seek_score_quota_test.go`, `search_targets_test.go`
EOF
)")"; link_subissue "$ep1_id" "$(echo "$s" | jq -r '.id')"; add_to_project "$(echo "$s" | jq -r '.url')"

s="$(create_issue "Story: Form filling pipeline (form_answer + form_vision) on live application pages" \
  -l story -l "area:automation" -l "area:llm" \
  -b "$(cat <<'EOF'
## Context
- Domain tasks: `form_answer`, `form_vision` in `internal/domain/llm_tasks.go`
- Bot: `internal/bot/form.go`
- Eval fixtures: `eval/datasets/synthetic/form_answer/`, `form_vision/`

## Acceptance criteria
- [ ] End-to-end apply uses vision + answer tasks with quota/billing hooks intact
- [ ] Failure leaves job in cannot-apply or pending-review with explicit reason (not silent skip)
- [ ] Synthetic eval smoke runs documented in `docs/MODEL_EVALUATION.md` for both tasks
- [ ] Reduce duplicate LLM calls within a single application (token/cost)

## Consolidation
Form field bugs, vision misreads, and multi-page wizards → **this story only**.
EOF
)")"; link_subissue "$ep1_id" "$(echo "$s" | jq -r '.id')"; add_to_project "$(echo "$s" | jq -r '.url')"

s="$(create_issue "Story: Cannot-apply queue, retries, and manual completion workflows" \
  -l story -l "area:automation" -l "area:product" \
  -b "$(cat <<'EOF'
## Context
API: `internal/handler/router.go` (`/api/jobs/cannot-apply/*`), UI: `web/src/pages/JobsCannotApply.tsx`, migrations `019`, `021`.

## Acceptance criteria
- [ ] Retry/requeue/mark-applied flows match API and UI with e2e or handler tests
- [ ] Bulk retry-all safe under quota and bot concurrency rules
- [ ] User-facing copy explains why a job landed in cannot-apply and next steps
- [ ] Admin operational-errors view correlates with cannot-apply reasons where useful

## Consolidation
All cannot-apply UX + backend retry semantics → this story.
EOF
)")"; link_subissue "$ep1_id" "$(echo "$s" | jq -r '.id')"; add_to_project "$(echo "$s" | jq -r '.url')"

s="$(create_issue "Story: Apply-by-URL and single-job automation entry points" \
  -l story -l "area:automation" -l "area:product" \
  -b "$(cat <<'EOF'
## Context
`POST /api/bot/apply-url`, `Manager.ApplyURL` in `internal/bot/manager.go`, platform detect from URL.

## Acceptance criteria
- [ ] Supported URL patterns documented (LinkedIn + Seek)
- [ ] Same scoring/tailoring/review gates as batch bot
- [ ] UI entry point (if any) or documented API usage for power users
- [ ] Handler tests cover auth, quota, unsupported platform errors
EOF
)")"; link_subissue "$ep1_id" "$(echo "$s" | jq -r '.id')"; add_to_project "$(echo "$s" | jq -r '.url')"

# --- Epic 2 ---
ep2="$(create_issue "Epic: New job platforms & runner extensibility" \
  -l epic -l "area:automation" \
  -b "$(cat <<'EOF'
## Outcome
Adding a third job platform is a **repeatable** exercise: runner interface, registration, settings UI, session storage, tests — without forking manager logic.

## Repo pattern
- `platformRunner` + `registerRunner` in `internal/bot/bot.go`
- Reference impl: `test_automation` in `internal/bot/testplatform.go`
- `SupportedPlatforms` in `internal/domain/types.go`

## Consolidation
Spikes, POCs, and production rollout for **any** new board (Indeed, Greenhouse, Lever, etc.) roll into stories here.
EOF
)")"
ep2_id="$(echo "$ep2" | jq -r '.id')"
add_to_project "$(echo "$ep2" | jq -r '.url')"

s="$(create_issue "Story: Runner extension guide + test_automation as template" \
  -l story -l "area:automation" -l "area:platform-eng" \
  -b "$(cat <<'EOF'
## Deliverables
- [ ] Short dev doc: how to add a platform (init register, cookies, search, apply hooks)
- [ ] Checklist mirrored in PR template or `CLAUDE.md` pointer
- [ ] `test_automation` runner documents expected manager callbacks
- [ ] Settings → Platforms UI pattern for N platforms (not hard-coded two only in new code)

## Acceptance
New platform PR touches predictable files; reviewer checklist exists.
EOF
)")"; link_subissue "$ep2_id" "$(echo "$s" | jq -r '.id')"; add_to_project "$(echo "$s" | jq -r '.url')"

s="$(create_issue "Story: Next platform — candidate selection through MVP apply path" \
  -l story -l "area:automation" -l icebox \
  -b "$(cat <<'EOF'
## Note
Pick **one** external platform after spike; keep all implementation work in this story until MVP ships.

## Spike acceptance
- [ ] Written comparison: TAM, auth model, form complexity, ToS risk, geo focus
- [ ] Decision record in issue comments (not scattered docs)

## MVP acceptance
- [ ] Runner registered, session save/load, at least search OR apply-url path
- [ ] User guide section + feature flag or admin gate if needed
- [ ] Handler/bot tests; no Rod in unit tests where mock runner suffices
EOF
)")"; link_subissue "$ep2_id" "$(echo "$s" | jq -r '.id')"; add_to_project "$(echo "$s" | jq -r '.url')"

# --- Epic 3 ---
ep3="$(create_issue "Epic: LLM quality, evaluation & production policies" \
  -l epic -l "area:llm" \
  -b "$(cat <<'EOF'
## Outcome
Every production LLM task (`internal/domain/llm_tasks.go`) has eval coverage, an approvable policy, and safe rollback — without spending customer credits on experiments.

## Docs
`docs/MODEL_EVALUATION.md`, `eval/datasets/BENCHMARK.md`, Admin → Models

## Known gap
Cross-provider production routing is explicitly **not implemented** (`internal/eval/policy/approve.go`).

## Consolidation
All task-specific prompt tweaks, eval datasets, policy approval, and admin Models UI → stories under this epic.
EOF
)")"
ep3_id="$(echo "$ep3" | jq -r '.id')"
add_to_project "$(echo "$ep3" | jq -r '.url')"

s="$(create_issue "Story: Full synthetic datasets + validators for all 8 LLM tasks" \
  -l story -l "area:llm" \
  -b "$(cat <<'EOF'
## Tasks
resume_extract, job_scoring, employment_ethics, resume_tailoring, cover_letter, form_answer, form_vision, application_questions

## Acceptance
- [ ] Each task has smoke + full dataset under `eval/datasets/synthetic/<task>/`
- [ ] Critical validators block bad recommendations (`internal/eval/validators`)
- [ ] `cmd/generate-eval-datasets` regenerates fixtures deterministically
- [ ] Job scoring ground truth process documented (`eval/datasets/synthetic/job_scoring/SCORING_GROUND_TRUTH.md`)

## Consolidation
New eval cases for a task → extend this story (or sub-task checklist in comments), not new issues per case.
EOF
)")"; link_subissue "$ep3_id" "$(echo "$s" | jq -r '.id')"; add_to_project "$(echo "$s" | jq -r '.url')"

s="$(create_issue "Story: Eval automation in CI and release gates" \
  -l story -l "area:llm" -l "area:platform-eng" \
  -b "$(cat <<'EOF'
## Acceptance
- [ ] CI job (or scheduled workflow) runs bounded-cost smoke evals (`cmd/eval`)
- [ ] Failures block merge or open admin alert — policy TBD in PR
- [ ] Artifacts: metrics summary retained for trend comparison
- [ ] Document operator workflow alongside `make test` expectations

## Related
`.github/workflows/ci.yml` currently: lint, coverage, Playwright — no eval yet.
EOF
)")"; link_subissue "$ep3_id" "$(echo "$s" | jq -r '.id')"; add_to_project "$(echo "$s" | jq -r '.url')"

s="$(create_issue "Story: Admin Models UI — eval runs, approve, rollback operator UX" \
  -l story -l "area:llm" -l "area:product" \
  -b "$(cat <<'EOF'
## Context
`web/src/pages/admin/Models.tsx`, handlers `internal/handler/admin_models*.go`

## Acceptance
- [ ] Operators can start eval, inspect detail, approve same-provider candidate, rollback with preview
- [ ] Empty/error states for catalog refresh and LiteLLM staging
- [ ] Playwright or handler tests for critical admin paths
- [ ] Copy uses field-agnostic language (any profession)

## Consolidation
All admin model policy UX tweaks → this story.
EOF
)")"; link_subissue "$ep3_id" "$(echo "$s" | jq -r '.id')"; add_to_project "$(echo "$s" | jq -r '.url')"

s="$(create_issue "Story: Cross-provider failover and effort-capable routing (design + implementation)" \
  -l story -l "area:llm" -l icebox \
  -b "$(cat <<'EOF'
## Context
Blocked today: `cross-provider production routing not implemented` in eval approve/recommend paths.

## Acceptance
- [ ] Architecture doc: billing catalog, quota, eval approval constraints
- [ ] Implementation behind explicit admin flag or policy field
- [ ] Eval gate proves safety before enable
- [ ] `internal/llm/capabilities.go` effort errors handled per provider

## Consolidation
Any provider-mixing production change → only this story.
EOF
)")"; link_subissue "$ep3_id" "$(echo "$s" | jq -r '.id')"; add_to_project "$(echo "$s" | jq -r '.url')"

# --- Epic 4 ---
ep4="$(create_issue "Epic: Commercial — Stripe billing, quota & unit economics" \
  -l epic -l "area:billing" \
  -b "$(cat <<'EOF'
## Outcome
Paid plans, top-ups, and credit enforcement are trustworthy in production; admins can reconcile Stripe drift; users understand usage.

## Docs
`docs/billing.md`, `docs/LLM_BILLING_ARCHITECTURE.md`, `web/src/pages/Pricing.tsx`, `settings/Plan.tsx`

## Consolidation
Webhook idempotency bugs, portal/checkout UX, quota math, admin billing screens → stories here.
EOF
)")"
ep4_id="$(echo "$ep4" | jq -r '.id')"
add_to_project "$(echo "$ep4" | jq -r '.url')"

s="$(create_issue "Story: Stripe webhook hardening, monitoring, and reconcile runbooks" \
  -l story -l "area:billing" \
  -b "$(cat <<'EOF'
## Context
Migrations `025`, `029`, `030`, `031`; admin routes reconcile + webhook events; `scripts/stripe-test-mode/`

## Acceptance
- [ ] Production checklist: secrets, `APP_BASE_URL`, insecure webhook flag off
- [ ] Admin can filter webhook events and run per-user reconcile safely
- [ ] E2E scenario log kept current (`scripts/stripe-test-mode/E2E_SCENARIO_LOG.md`)
- [ ] Tests cover ordering, lease reclaim, top-up idempotency

## Consolidation
All Stripe backend correctness → this story.
EOF
)")"; link_subissue "$ep4_id" "$(echo "$s" | jq -r '.id')"; add_to_project "$(echo "$s" | jq -r '.url')"

s="$(create_issue "Story: User plan, quota, and usage surfaces (Settings + Pricing)" \
  -l story -l "area:billing" -l "area:product" \
  -b "$(cat <<'EOF'
## Context
`web/src/pages/settings/Plan.tsx`, `Usage.tsx`, `Pricing.tsx`, `GET /api/quota/status`, billing checkout/portal routes

## Acceptance
- [ ] Trial/expired/past_due/cancel-at-period-end states match `docs/billing.md`
- [ ] Top-up gated correctly in UI (matches server rules)
- [ ] Usage/credits copy aligns with LLM billing architecture
- [ ] e2e or component tests for plan states where feasible

## Consolidation
User-visible billing/quota copy and flows → this story only.
EOF
)")"; link_subissue "$ep4_id" "$(echo "$s" | jq -r '.id')"; add_to_project "$(echo "$s" | jq -r '.url')"

s="$(create_issue "Story: Admin economics, LLM usage, and quota operations" \
  -l story -l "area:billing" -l "area:llm" \
  -b "$(cat <<'EOF'
## Context
Admin pages: `Economics.tsx`, `LlmUsage.tsx`, `Quota.tsx`, handlers under `admin_llm_usage.go`, economics routes

## Acceptance
- [ ] Per-task/per-model/per-user cost views match ledger events
- [ ] Quota defaults admin edits propagate to new users correctly
- [ ] Drill-down to single application (`economics/application/{job_id}`) usable for support
- [ ] Field-agnostic labels in UI

## Consolidation
Admin cost/usage dashboards → this story.
EOF
)")"; link_subissue "$ep4_id" "$(echo "$s" | jq -r '.id')"; add_to_project "$(echo "$s" | jq -r '.url')"

# --- Epic 5 ---
ep5="$(create_issue "Epic: Product UX — discovery through apply lifecycle" \
  -l epic -l "area:product" \
  -b "$(cat <<'EOF'
## Outcome
A user can onboard, run the bot, review tailored materials, manage top matches and job lists, without hunting through settings.

## Key pages
`Dashboard.tsx`, `Review.tsx`, `TopMatches.tsx`, `JobsApplied.tsx`, `JobsSkipped.tsx`, `Generate.tsx`

## Consolidation
Dashboard widgets, review queue, job list filters, navigation — one epic; split by story below only.
EOF
)")"
ep5_id="$(echo "$ep5" | jq -r '.id')"
add_to_project "$(echo "$ep5" | jq -r '.url')"

s="$(create_issue "Story: Dashboard — bot control, live logs, and status clarity" \
  -l story -l "area:product" \
  -b "$(cat <<'EOF'
## Context
`web/src/pages/Dashboard.tsx`, WebSocket `/ws/logs`, bot status API

## Acceptance
- [ ] Start/stop/pause/resume states obvious; errors surfaced
- [ ] Live logs usable (verbose mode respects user setting migration `032`)
- [ ] Playwright `web/e2e/dashboard.spec.ts` extended for regressions
- [ ] Mobile-friendly control layout

## Consolidation
All Dashboard-only UX → this story.
EOF
)")"; link_subissue "$ep5_id" "$(echo "$s" | jq -r '.id')"; add_to_project "$(echo "$s" | jq -r '.url')"

s="$(create_issue "Story: Review queue — approve/reject tailored documents before submit" \
  -l story -l "area:product" \
  -b "$(cat <<'EOF'
## Context
`Review.tsx`, `/api/bot/review/*`, pending review tables + PDF file serving

## Acceptance
- [ ] Preview resume/cover letter PDFs securely (`/api/files/*` ownership check)
- [ ] Approve triggers submit; reject returns job to appropriate state
- [ ] Halal/ethics info visible when enabled
- [ ] e2e coverage or handler tests for review flow

## Consolidation
Review queue UX + API semantics → this story.
EOF
)")"; link_subissue "$ep5_id" "$(echo "$s" | jq -r '.id')"; add_to_project "$(echo "$s" | jq -r '.url')"

s="$(create_issue "Story: Top matches and manual-apply tracking" \
  -l story -l "area:product" -l "area:automation" \
  -b "$(cat <<'EOF'
## Context
`TopMatches.tsx`, `/api/jobs/top-matches`, migrations for top match fields

## Acceptance
- [ ] High-scoring non-Easy Apply roles visible with clear manual next steps
- [ ] Mark applied / blacklist company from pending review flows consistent
- [ ] `web/e2e/jobs-populated.spec.ts` covers populated states

## Consolidation
Top matches + manual apply UX → this story.
EOF
)")"; link_subissue "$ep5_id" "$(echo "$s" | jq -r '.id')"; add_to_project "$(echo "$s" | jq -r '.url')"

s="$(create_issue "Story: Job history — applied, skipped, search & filters" \
  -l story -l "area:product" \
  -b "$(cat <<'EOF'
## Context
`JobsApplied.tsx`, `JobsSkipped.tsx`, stats endpoint, job detail `GET /api/jobs/{job_id}`

## Acceptance
- [ ] Consistent cards/tables, delete actions, suitability score display
- [ ] Stats on Dashboard align with list endpoints
- [ ] e2e `jobs.spec.ts` / populated fixtures maintained

## Consolidation
Applied/skipped list UX → this story (cannot-apply is separate automation epic story).
EOF
)")"; link_subissue "$ep5_id" "$(echo "$s" | jq -r '.id')"; add_to_project "$(echo "$s" | jq -r '.url')"

s="$(create_issue "Story: Ad-hoc Generate page (evaluate, tailor, cover letter, Q&A)" \
  -l story -l "area:product" -l "area:llm" \
  -b "$(cat <<'EOF'
## Context
`Generate.tsx`, `/api/resume/*` routes

## Acceptance
- [ ] Tools work without running full bot; quota enforced
- [ ] Halal check and answer-questions usable standalone
- [ ] `web/e2e/generate.spec.ts` passing and extended for new fields

## Consolidation
Generate page features → this story.
EOF
)")"; link_subissue "$ep5_id" "$(echo "$s" | jq -r '.id')"; add_to_project "$(echo "$s" | jq -r '.url')"

# --- Epic 6 ---
ep6="$(create_issue "Epic: Profile, resume markets & document generation" \
  -l epic -l "area:product" -l "area:llm" \
  -b "$(cat <<'EOF'
## Outcome
Profile setup, resume import, market-specific PDF output, and tailoring quality support **any profession**.

## Repo
`internal/resume/`, `resume_markets/`, Settings → Resume/Preferences, halal migrations

## Consolidation
PDF styling, market YAML, extract/tailor/cover prompts → stories here (not scattered).
EOF
)")"
ep6_id="$(echo "$ep6" | jq -r '.id')"
add_to_project "$(echo "$ep6" | jq -r '.url')"

s="$(create_issue "Story: Resume upload, AI extract, and profile settings UX" \
  -l story -l "area:product" -l "area:llm" \
  -b "$(cat <<'EOF'
## Context
`settings/Resume.tsx`, `settings/Preferences.tsx`, `TaskResumeExtract`, resume handlers

## Acceptance
- [ ] PDF/DOCX/TXT extract path reliable; user reviews before save
- [ ] YAML export/import documented in user guide
- [ ] Preferences: titles, locations, halal filter, blacklists saved and used by bot
- [ ] Field-agnostic examples in UI copy

## Consolidation
Profile + preferences → this story.
EOF
)")"; link_subissue "$ep6_id" "$(echo "$s" | jq -r '.id')"; add_to_project "$(echo "$s" | jq -r '.url')"

s="$(create_issue "Story: Resume markets, PDF rendering, and custom CSS styles" \
  -l story -l "area:product" \
  -b "$(cat <<'EOF'
## Context
`resume_markets/market_*.yaml`, settings markets/styles routes, Docker volume `resume_style/`

## Acceptance
- [ ] Markets list matches rendering logic; default market clear in UI
- [ ] Custom CSS overrides documented (README Docker table)
- [ ] Generated PDFs land under `job_applications/` with stable paths for review

## Consolidation
Rendering/markets/styles → this story.
EOF
)")"; link_subissue "$ep6_id" "$(echo "$s" | jq -r '.id')"; add_to_project "$(echo "$s" | jq -r '.url')"

s="$(create_issue "Story: Tailoring & cover letter quality (prompts, scoring alignment, halal)" \
  -l story -l "area:llm" \
  -b "$(cat <<'EOF'
## Context
Tasks: resume_tailoring, cover_letter, employment_ethics; eval datasets for each

## Acceptance
- [ ] Tailored output respects profile facts (no fabricated employers)
- [ ] Cover letter tone configurable via settings where supported
- [ ] Halal/employment ethics integrated in bot path when filter enabled
- [ ] Eval smoke passes before prompt changes merge

## Consolidation
Prompt/content quality for documents → this story.
EOF
)")"; link_subissue "$ep6_id" "$(echo "$s" | jq -r '.id')"; add_to_project "$(echo "$s" | jq -r '.url')"

# --- Epic 7 ---
ep7="$(create_issue "Epic: Identity, admin operations & browser sessions" \
  -l epic -l "area:platform-eng" -l "area:product" \
  -b "$(cat <<'EOF'
## Outcome
Secure multi-user app: auth, Google OAuth, admin tools, platform browser sessions (local + Docker noVNC).

## Repo
`internal/auth/`, admin pages, `Platforms.tsx`, VNC handlers, `internal/browser/`

## Consolidation
Auth bugs, admin user management, noVNC — this epic.
EOF
)")"
ep7_id="$(echo "$ep7" | jq -r '.id')"
add_to_project "$(echo "$ep7" | jq -r '.url')"

s="$(create_issue "Story: Auth — register/login/refresh, Google OAuth, session security" \
  -l story -l "area:platform-eng" \
  -b "$(cat <<'EOF'
## Acceptance
- [ ] JWT refresh/logout flows solid; tests in `internal/auth`, handler auth tests
- [ ] Google OAuth env documented; callback errors user-friendly
- [ ] No secrets in logs; bcrypt + encrypted secrets store unchanged pattern

## Consolidation
All authentication work → this story.
EOF
)")"; link_subissue "$ep7_id" "$(echo "$s" | jq -r '.id')"; add_to_project "$(echo "$s" | jq -r '.url')"

s="$(create_issue "Story: Platform browser connect — local Rod, Docker noVNC, session storage" \
  -l story -l "area:product" -l "area:automation" \
  -b "$(cat <<'EOF'
## Context
`settings/Platforms.tsx`, `/api/auth/launch-browser`, `save-session`, VNC routes, README Docker section

## Acceptance
- [ ] Connect → login → save session works locally and in `make docker-run`
- [ ] `vnc_enabled` system info reflected in UI
- [ ] Credential storage optional and documented
- [ ] e2e settings spec covers platform settings smoke

## Consolidation
Browser session UX → this story.
EOF
)")"; link_subissue "$ep7_id" "$(echo "$s" | jq -r '.id')"; add_to_project "$(echo "$s" | jq -r '.url')"

s="$(create_issue "Story: Admin console — users, defaults, automation, audit errors" \
  -l story -l "area:platform-eng" \
  -b "$(cat <<'EOF'
## Context
`web/src/pages/admin/*`, system defaults migration `022`, `AuditErrors.tsx`, `Automation.tsx`

## Acceptance
- [ ] Admin-only routes enforced (`RequireAdmin`)
- [ ] User prune e2e, quota defaults, system secrets operational
- [ ] Overview/health useful for on-call
- [ ] `web/e2e/admin.spec.ts` covers critical paths

## Consolidation
Admin features (except Models/Economics — other epics) → this story.
EOF
)")"; link_subissue "$ep7_id" "$(echo "$s" | jq -r '.id')"; add_to_project "$(echo "$s" | jq -r '.url')"

# --- Epic 8 ---
ep8="$(create_issue "Epic: Engineering excellence — tests, CI, deploy & docs" \
  -l epic -l "area:platform-eng" \
  -b "$(cat <<'EOF'
## Outcome
Contributors and operators can ship with confidence: lint, unit coverage, Playwright e2e, OpenAPI sync, clear deploy path.

## Repo
`Makefile`, `.github/workflows/ci.yml`, `api/openapi.yaml`, `docs/*`

## Consolidation
CI flakes, missing tests, doc drift → this epic.
EOF
)")"
ep8_id="$(echo "$ep8" | jq -r '.id')"
add_to_project "$(echo "$ep8" | jq -r '.url')"

s="$(create_issue "Story: Playwright e2e expansion (billing, review, bot smoke)" \
  -l story -l "area:platform-eng" \
  -b "$(cat <<'EOF'
## Context
`web/e2e/*`, `JOBIFAI_E2E=1` seed routes, `make test-e2e`

## Acceptance
- [ ] Critical user journeys covered: auth, dashboard, jobs populated, settings, admin smoke
- [ ] Document how to run `make test-e2e-ui` locally
- [ ] Reduce flake via fixtures/helpers in `web/e2e/helpers/`

## Consolidation
New e2e specs → extend this story checklist.
EOF
)")"; link_subissue "$ep8_id" "$(echo "$s" | jq -r '.id')"; add_to_project "$(echo "$s" | jq -r '.url')"

s="$(create_issue "Story: Go handler & bot integration test gaps" \
  -l story -l "area:platform-eng" \
  -b "$(cat <<'EOF'
## Context
`internal/handler/*_test.go`, `internal/bot/manager_automation_test.go`, testify patterns in CLAUDE.md

## Acceptance
- [ ] Billing/quota handlers have httptest coverage for regressions
- [ ] Bot manager tests avoid Rod where mock runner suffices
- [ ] `make test-all` remains race-clean

## Consolidation
Backend test debt → this story.
EOF
)")"; link_subissue "$ep8_id" "$(echo "$s" | jq -r '.id')"; add_to_project "$(echo "$s" | jq -r '.url')"

s="$(create_issue "Story: OpenAPI spec, codegen (ogen), and API docs sync" \
  -l story -l "area:platform-eng" -l documentation \
  -b "$(cat <<'EOF'
## Context
`api/openapi.yaml`, `gen/`, `docs/api.md`, router route map

## Acceptance
- [ ] OpenAPI matches `internal/handler/router.go` routes
- [ ] Regenerate gen/ via project makefile target when routes change
- [ ] docs/api.md points to `/api/openapi.yaml`

## Consolidation
API surface changes → update in same PR as this story checklist.
EOF
)")"; link_subissue "$ep8_id" "$(echo "$s" | jq -r '.id')"; add_to_project "$(echo "$s" | jq -r '.url')"

s="$(create_issue "Story: Production deployment, Docker ops, and observability baseline" \
  -l story -l "area:platform-eng" \
  -b "$(cat <<'EOF'
## Acceptance
- [ ] Production runbook: env vars, Stripe, LLM keys, volumes, Xvfb/noVNC
- [ ] Structured logging (zerolog) fields consistent for bot failures
- [ ] Optional: metrics/tracing spike documented as follow-up in comments

## Consolidation
Deploy/ops documentation → this story.
EOF
)")"; link_subissue "$ep8_id" "$(echo "$s" | jq -r '.id')"; add_to_project "$(echo "$s" | jq -r '.url')"

# --- Epic 9 — GTM / icebox ---
ep9="$(create_issue "Epic: Growth, trust & go-to-market readiness" \
  -l epic -l icebox \
  -b "$(cat <<'EOF'
## Outcome
Public-facing trust and discovery so real users can adopt jobifai beyond localhost.

## Consolidation
Marketing site tweaks, legal pages, notifications, SEO — capture here so ideas are not lost.

## Not scheduled
Label `icebox` stories may stay in Backlog until product priorities shift.
EOF
)")"
ep9_id="$(echo "$ep9" | jq -r '.id')"
add_to_project "$(echo "$ep9" | jq -r '.url')"

s="$(create_issue "Story: Public marketing site polish (Landing, Pricing, brand)" \
  -l story -l icebox -l "area:product" \
  -b "$(cat <<'EOF'
## Context
`Landing.tsx`, `web/public/brand/jobifai-brand-sheet.md`, public plans API

## Acceptance
- [ ] Messaging field-agnostic; halal filter presented respectfully as optional
- [ ] Pricing aligns with Stripe public plans
- [ ] Accessibility pass on marketing pages

## Ideas bucket
Add comments to this issue for copy/design ideas instead of new issues.
EOF
)")"; link_subissue "$ep9_id" "$(echo "$s" | jq -r '.id')"; add_to_project "$(echo "$s" | jq -r '.url')"

s="$(create_issue "Story: Trust — privacy policy, terms, data export/delete" \
  -l story -l icebox -l "area:platform-eng" \
  -b "$(cat <<'EOF'
## Acceptance
- [ ] Privacy policy covers LLM providers, stored credentials, job history retention
- [ ] User data export/delete flows (GDPR-style) designed and implemented
- [ ] Admin user delete semantics documented

## Consolidation
Legal/trust/privacy → this story.
EOF
)")"; link_subissue "$ep9_id" "$(echo "$s" | jq -r '.id')"; add_to_project "$(echo "$s" | jq -r '.url')"

s="$(create_issue "Story: Idea inbox — capture future features (use issue comments)" \
  -l story -l icebox \
  -b "$(cat <<'EOF'
## Purpose
**Do not open a new issue for every idea.** Comment on this story with:

- One paragraph description
- User value
- Rough area label (automation / llm / billing / product)

Maintainers periodically fold comments into the correct epic story or promote to scheduled work.

## Seed ideas (from repo gaps)
- Email/push when review queue needs attention
- Scheduled bot runs / cron
- Multi-platform parallel search
- Export application history CSV
- Mobile PWA shell
- Self-hosted Ollama-first onboarding preset
EOF
)")"; link_subissue "$ep9_id" "$(echo "$s" | jq -r '.id')"; add_to_project "$(echo "$s" | jq -r '.url')"

# Index issue (unquoted EOF so project_url/num expand; no nested command substitution)
idx_body="$(cat <<EOF
## GitHub Project
**[jobifai Roadmap]($project_url)** (Project #$project_num)

## Hierarchy (GitHub best practice)
- **Epics** = issues labeled \`epic\` (outcome-sized)
- **Stories** = **sub-issues** of an epic (shippable slices)
- **Ideas** = comment on the *Idea inbox* story or use \`icebox\` label — avoid duplicate issues

## Fields to enable in the Project (manual, once)
In Project **Settings → Fields**, show:
- **Parent issue** — group by epic
- **Sub-issue progress** — completion rollup
- **Status** — Backlog / Ready / In progress / Done

Suggested views:
1. **Board** by Status
2. **Table** grouped by Parent issue
3. **Roadmap** by Target date (optional)

## Consolidation rule (save tokens / context)
Before opening work, search existing stories. Same component (e.g. LinkedIn bot, Stripe webhooks, Review queue) → **extend the existing story** with a checklist item or PR linked to that issue.

## Repo map
| Area | Primary paths |
|------|----------------|
| Automation | \`internal/bot/\`, Platforms in domain |
| LLM / eval | \`internal/eval/\`, \`internal/llm/\`, Admin Models |
| Billing | \`internal/billing/\`, \`docs/billing.md\` |
| Product UI | \`web/src/pages/\` |
| Admin | \`web/src/pages/admin/\` |

## Bootstrap script
\`scripts/github-backlog/bootstrap.sh\` — re-run only on empty epic backlog.
EOF
)"
idx="$(create_issue "📋 Backlog index — how we use Epics & Stories" -l documentation -b "$idx_body")"
add_to_project "$(echo "$idx" | jq -r '.url')"

echo ""
echo "Done."
echo "Project: $project_url"
echo "Index issue: $(echo "$idx" | jq -r '.url')"
