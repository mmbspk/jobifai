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
locally, then looks up independently verified question mappings. It sends only
unresolved controls to the configured form-answer model in one batch. Candidate
facts come from the current profile or explicit reviewed answers. Unknown facts,
ambiguous labels, changed qualifiers and sensitive declarations pause for review.
The model cannot grant navigation, upload or submit authority.

Mappings store typed bindings and form conditions, never candidate answers.
Successful browser validation does not promote a model proposal. Review pending
questions in **Settings → Profile → Application question mappings**, confirm the
question and answer source, or provide a candidate answer. Confirmation stores a
new answer in the candidate profile and promotes its mapping in one transaction.
Clearing a profile fact prevents its later reuse. Invalidation affects the selected
question and employer scope. Review shows the saved answer and cannot confirm a
missing answer. Shadow observations cannot activate mappings.

Native mappings use a verified issuer link when available; otherwise they stay
within the listing. Greenhouse mappings use the verified tenant. No display-name
substring match grants cross-employer reuse. Role-dependent questions require
explicit review and carry a hashed vacancy scope, including in Shadow. Control
signatures include native input type, numeric bounds, step, pattern and locale;
browser validity remains the final authority for HTML-specific constraints.
Large option sets stay local with a
hash covering every choice; the model receives a bounded list. Only local or
verified answers can resolve omitted choices. Autocomplete requires an exact,
unique, associated suggestion and observed selection acceptance.

Actual approved documents are uploaded and their attachment identity is checked.
Controls are re-read before filling. Navigation requires observable advancement.
Before each step action the worker checks rollback, available browser quota and
application identity. Submit intent is durable before the single submit click.
Only scoped positive confirmation produces a confirmed application. Missing
confirmation leaves an unknown outcome for manual reconciliation; retrying cannot
submit the same unresolved application again.

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

Migrations 053–059 add engine audit/dimensions, mapping schemas and independent
mapping verification, and external application identity on attempts. Migrations
056 and 057 replace older mapping tables. Migration 058 invalidates previous
mappings and clears their saved answers; candidate profiles and application
history are preserved. Those retired answers cannot be restored by a Down
migration. Back up SQLite using its backup API before upgrading an existing
installation. A filesystem copy of a live WAL database is insufficient.

Keep the upgraded database for hot rollback. For a binary downgrade, test the
specific old binary against a copy of the database; do not delete columns or
mapping tables from the live database as an informal rollback procedure.

## Limits, accounting and evidence

Adaptive attempts have a ten-minute time limit and at most ten interpretation
batches. Steps allow at most three fill attempts and ninety seconds. The native
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

Live validation and production cost evidence are still pending in the
validation record. The controlled paired replay is recorded separately and
does not estimate production billing. No release criterion should be marked complete from a helper
test or a simulated confirmation alone.
