# Application answers in your profile

Complete **Settings → Profile → Application answers** when creating or editing
your profile. These facts help Jobifai answer recurring recruiter questions
automatically. You can leave unknown information blank and add details later.
The Common answers panel shows five core topics with one editor open at a time.
Five optional topics and candidate-authored extra answers have separate disclosures.
Onboarding uses the same editable panel and can save the reviewed profile and
answers together. Unanswered recommendations do not prevent starting a bot.

| Information | What to provide |
| --- | --- |
| Notice and availability | Notice period, when you can start, and any supplied earliest start date |
| Salary | Minimum/maximum, currency, pay period, base or total compensation, and whether benefits such as superannuation are included |
| Work rights and sponsorship | Country, declared status, current/future sponsorship independently, and relevant visa type, expiry or restrictions |
| Relocation | Whether you are willing to relocate, destinations and conditions |
| Travel, commute and hours | Your willingness, frequency, locations, notice and shift constraints; reuse existing work preferences |
| Credentials | Reuse profile qualifications and certifications; a qualification does not imply an active licence |
| Additional details | Other factual information you want application answers to use |
| Custom questions | Your own question and answer pairs, with an optional country scope |

Existing education, experience, skills, languages and certifications also support
application answers. Add further information to an answer when a short response
would leave out relevant conditions. A blank sponsorship or relocation choice
means unknown; it does not become No. Work rights in one country do not establish
work rights elsewhere. Salary periods and currencies are not silently converted.

Jobifai reads current profile facts and verified mappings first. For unfamiliar
wording it asks the model to select a supported option or compose an answer,
checks the cited evidence and independently verifies the result. Supported descriptive
phrasing may be used and recorded for later confirmation; sensitive declarations,
eligibility, pay, dates and credentials retain strict supplied-fact guards.
Unknown required facts defer that application while other eligible jobs continue.
Optional missing facts may stay blank. Provider/quota/cancellation/browser and
identity/document failures keep their operational handling.

Review proposals are durable before provisional input. Exact verified fill text is
recorded before advancing or submitting. **Needs confirmation** distinguishes
not-used suggestions, filled answers, confirmed submissions and unknown outcomes.
Compatible repeats group into bounded, paginated decisions. Reading the grouped
notification does not confirm anything; unread and unresolved counts are separate.
Unchanged repeats do not generate another unread alert. Dismissed unchanged
questions stay quiet; changed facts or proposals can reopen review. When a form
requires a shorter replacement answer, the earlier text stays in history as
replaced and only final text can be marked submitted.

Confirm/edit affects future use. It neither rewrites past answers, starts a bot,
submits applications nor retries submission_unknown. Updated facts invalidate
affected reuse. Technical mapping inspection is secondary under advanced
inspection. Review metadata does not grant mapping trust; the PR #138 cache
and its independent verification/confirmed-transition gates remain intact.
Safely blocked fact deferrals can become eligible for normal future evaluation
after correction; active, unknown and confirmed submissions remain held. Fact
deferrals do not consume operational retries. Preparation and vision recovery
also require current, uniquely bound controls and the shared answer checks.

CV extraction reuses one metered call and returns exact supporting excerpts.
Only clear declarations prefill the draft; absent, ambiguous or outdated facts
remain unknown or suggestions. Reviewed save is explicit confirmation. Existing
facts and unsaved edits survive re-import, and conflicting edits retain drafts.
Canonical YAML import/export keeps its zero-call path. Evidence and badges stay
outside factual profile JSON, so metadata updates do not invalidate warm mappings
or generated documents.

These fields cover recurring categories rather than a universal questionnaire.
LinkedIn's [published screening categories](https://www.linkedin.com/help/linkedin/answer/a519651)
include work authorization, visa status, location, education, experience, skills,
languages, certifications, hybrid work and urgent hiring needs. Its
[custom-question guidance](https://www.linkedin.com/help/linkedin/answer/a522418)
recommends clear, job-relevant questions. The native SEEK form observed during
qualification also asked about notice, salary excluding super, work rights and
location/relocation. Sources checked on 23 November 2026.

## Migration and rollback

Migration 061 adds answer-source, import-evidence, review, encounter and grouped
notification records. Migration 060 is the existing registered Go repair.
No existing profile, mapping or application table is reset. Legacy sponsorship
boolean/null requests remain compatible with current/future object updates.
Topic saves patch only supplied facts; full-profile saves use factual revisions.

For a UI rollback, restore the previous frontend while retaining the compatible
backend and additive schema. Older backend profile serializers may omit the new
current/future sponsorship and other facts when saving a profile; a backend
rollback must protect these edits or retain their serialization support. Preserve
candidate facts, review evidence and application histories, and do not downgrade
the schema to undo UI behavior. Engine defaults, document approval and submission
policies remain as configured.
