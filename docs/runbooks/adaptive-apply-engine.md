# Adaptive Apply Engine — Operator Runbook

Epic #127, stories #128–#137. Qualification is recorded in
[the validation record](adaptive-apply-validation.md). Production defaults to
Legacy; enabling a pilot does not establish production readiness.

## Resolution and execution

LinkedIn Easy Apply, SEEK Quick Apply and verified Greenhouse hosted forms use
one dispatch path for batch, approved queue and direct submissions. Discovery,
scoring and the requirement to review a listing retain their existing contracts.
Unsupported destinations continue to require manual application.

The resolver reads the active application, resolves simple candidate fields
locally, then looks up verified question mappings. It sends unresolved controls
to the configured form-answer model in one batch. The model can select a supported
option or compose a first-person answer from current profile facts. Local code
checks its cited profile evidence and a separate batched verifier checks question
meaning, factual support and qualifiers before execution. Verified answers are
learned automatically. The model cannot grant navigation, upload or submit authority.

Mappings retain typed bindings and form conditions. Verified derived answers are
candidate scoped and carry evidence and a profile digest; profile changes prevent
stale reuse. Direct bindings reread current facts. Structured salary takes
precedence over older free text; sponsorship uses the applicable country. A
supplied visa expiry prevents reuse of an affirmative work-rights answer after
its date boundary. Browser acceptance alone cannot
promote a model proposal. **Settings → Profile → Application question mappings**
supports optional overrides and disabling reuse. Missing facts, unsupported
controls and sensitive declarations hold instead of fabricating an answer.
Explicit override confirmation stores the candidate answer and promotes its
mapping in one transaction. Shadow observations cannot activate mappings.

Set common facts and custom questions in **Settings → Profile → Application
answers** when creating or editing a profile. See the
[profile answer guide](application-profile-answers.md).

Native mappings use a verified issuer link when available; otherwise they stay
within the listing. Greenhouse mappings use the verified tenant. No display-name
substring match grants cross-employer reuse. Role-dependent questions carry a
hashed vacancy scope, including in Shadow. Control
signatures include native input type, numeric bounds, step, pattern and locale;
browser validity remains the final authority for HTML-specific constraints.
Large option sets stay local with a
hash covering every choice; the model receives a bounded list. Only local or
verified answers can resolve omitted choices. For an unresolved live dropdown,
interpretation can receive the complete list up to 512 choices and 16 KiB of
labels; the stored snapshot remains compact and retains the full choice hash.
Verified semantic category answers are cached for later local replay.
Autocomplete requires a unique
associated suggestion and observed selection acceptance. Ordinary fields require
an exact answer match. Candidate city fields also allow the saved city with an
explicit matching saved country in the suggestion; unrelated lists, a different
country and multiple matching locations hold.

Actual approved documents are uploaded and their attachment identity is checked
again on the final SEEK review immediately before Submit. If SEEK's saved résumé
library is full, the worker removes one eligible document by exact saved ID and
retries the approved upload. It uses last-use evidence where available and saved
dates as a fallback. Current, default and approved attachments are protected;
changed selection, ambiguous ownership or an unverified deletion stops recovery.
The final visibility wait is followed by fresh selection and rollback checks.

Application exports include their job ID and retain the existing version suffix,
for example `resume_job_12345678_abc12345.pdf` and
`cover_letter_job_12345678_abcdef12.pdf`. Stored versions, cached renders and
previous application paths retain their identity. Applied-document downloads
also identify the job; original-upload downloads retain their supplied filename.
Controls are re-read before filling. Navigation requires observable advancement.
Before each step action the worker checks rollback, available browser quota and
application identity. Submit intent is durable before the single submit click.
Only scoped positive confirmation produces a confirmed application. Missing
confirmation leaves an unknown outcome for manual reconciliation; retrying cannot
submit the same unresolved application again. Already prepared document
references remain available when the operator reconciles the outcome. A current
SEEK application-sent banner requires the expected native success route and exact
employer; recommendations and filenames elsewhere cannot prove submission.

## Configuration and rollback

Use **Admin → Automation → AI Apply Engine** to select Legacy, Shadow or Adaptive,
set platform overrides and pilot user IDs, and configure Greenhouse follow-through.
Engine changes and their audit entries commit together; a reason is optional.
External destinations are disabled by
default. Keep the production default Legacy until qualification and canary review
are complete.

Rollback by saving Legacy in the same control. In-flight adaptive work rechecks
policy before later form actions and stops. An already dispatched submit retains
its reconciliation status. Hot rollback needs no schema downgrade.

## Migrations and existing installs

Migrations 053–060 add engine audit/dimensions, mapping schemas and independent
mapping verification, and external application identity on attempts. Migrations
056 and 057 replace older mapping tables. Migration 058 invalidates previous
mappings and clears their saved answers; candidate profiles and application
history are preserved. Migration 060 conditionally repairs missing version-053
audit/engine objects on existing installations without rewriting their history.
Those retired answers cannot be restored by a Down
migration. Back up SQLite using its backup API before upgrading an existing
installation. A filesystem copy of a live WAL database is insufficient.

Keep the upgraded database for hot rollback. For a binary downgrade, test the
specific old binary against a copy of the database; do not delete columns or
mapping tables from the live database as an informal rollback procedure.

## Limits, accounting and evidence

Adaptive attempts have a ten-minute time limit and at most ten reserved form-answer
batches, including semantic verification. Steps allow at most three fill attempts
and ninety seconds. The native
loops cap form steps; configured provider retries and per-call time/cost ceilings
also apply. Account quota and browser metering are separate from these bounds
and the shared development ceiling.

Model events are attributed to the form-answer task, engine, source application
attempt and platform. External interpretation is attributed to Greenhouse while
the attempt retains its discovery platform and verified external identity.
Identical concurrent requests use the existing user-scoped generation lease;
profile, issuer or generation configuration changes prevent response reuse.
Pending generation responses are proposals, not active form mappings.

Fixture token counts and declared mock rates are synthetic. They do not establish
production cost, invoiced spend, completion rates or latency. Report actual
provider requests/retries, tokens, rate version, browser duration and confirmed
outcomes separately. A warm form with zero interpretation requests still incurs
browser, document generation, scoring and review costs where applicable.

## Live validation

Use the operator's selected account and approved profile/preferences. Start with
bounded runs, inspect the current profile, approved documents, sessions and
runtime budgets, then verify each outcome in both the jobsite and Jobifai history.
Record the platform, attempt, confirmation evidence and any failure requiring a
regression. Preserve unknown outcomes until reconciled. Interactive sign-in and
CAPTCHA challenges require the operator when encountered.

Both native sites have confirmed real applications in the
validation record. Final-source release checks are recorded there separately
from those live observations. The controlled paired replay is recorded separately and
does not estimate production billing. No release criterion should be marked complete from a helper
test or a simulated confirmation alone.
