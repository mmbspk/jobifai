# Epic 141 — design and delivery plan

## Product outcome

Prepare a small set of application answers once, use clear CV/profile facts to reduce typing, and review new uncertain answers later while the bot continues its run. Preserve the learned question cache and current native application protections.

## Proposed experience

Settings → Profile contains two views: CV & profile and Application answers.

Application answers defaults to Common answers. A separate Needs confirmation tab shows the number of unresolved decisions; opening a notification does not confirm an answer. Extra candidate-authored questions stay in a collapsed section.

The starter panel has five core topics: right to work, sponsorship, notice, earliest start, and expected pay. Five further topics—relocation, travel, work arrangement/commute, working hours and relevant credentials—are optional and progressively disclosed. Existing profile/preferences answer a topic where possible; the user supplies only missing facts.

Questions are compact rows with an answer summary and source label. One editor opens at a time. Clear CV facts may fill a row; unknown data stays unspecified. Structured pay details appear only when editing pay. A user can add further context to any answer.

The confirmation view shows one candidate decision per compatible question/context. It distinguishes an answer used in an application from a suggestion not used and a required fact that held one job. Confirm and Edit for future use are explicit actions. Previous submitted answers remain unchanged.

## Rough layout

    Profile
    CV & profile | Application answers

    Application answers
    Common answers | Needs confirmation (3)

    Common answers                         3 of 5 filled
    Right to work       saved value        From CV
    Sponsorship         Not specified      Add answer
    Notice period       saved value        From CV
    Earliest start      saved value        From CV
    Expected pay        Not specified      Add answer

    Other common questions (5 optional)     collapsed
    My additional answers                  collapsed
    Save common answers

## Visual direction

- Match Jobifai's existing neutral surfaces and violet action color; no new brand treatment or decorative dashboard metrics.
- Core palette, light/dark: background #f8f9fd/#11131b; surface #ffffff/#1a1d27; text #202632/#edf0f7; secondary text #657083/#abb4c5; border #dfe3ed/#333b4e; action #6252da/#a195f2.
- Use the existing Inter/system sans family. Page titles are clear and compact; body text is readable and left aligned. Source labels are secondary, with meaningful status text rather than confidence percentages.
- Use rows within one bounded answer panel, clear dividers and restrained corner radii. Avoid ten identical cards, broad gradients or a wall of learned variants.
- Reflow to one column at narrow widths, with visible keyboard focus and comfortable touch targets. Motion is limited to direct interaction feedback and respects reduced motion.

### Critique before building

A permanent two-column layout with common questions and all review items would compete for attention and leave too little room for pay/work-rights editors. The proposed focused tabs keep the default page quiet while the review count preserves discoverability. Context and evidence expand only when useful.

## Design states to inspect

1. First visit after CV upload: three clear answers prefilled, two missing, no guessed sponsorship or pay.
2. Common answer editing: false/No remains a real value; blank remains unknown; pay has explicit currency and period.
3. Returning user: a small confirmation count, grouped notifications and no raw mapping list.
4. Supported suggested wording: show the answer actually used and its evidence; confirm or edit for future use.
5. Required missing fact: one application waits for that fact; other eligible jobs can continue.
6. All reviews resolved: a clear completion state and a return to common answers.

The interactive preview uses fictional sample data and local presentation state. It does not save a real profile, call an LLM, start a bot or apply to any job.

## Implementation sequence

| Phase | Stories | Outcome and readiness check |
| --- | --- | --- |
| Design first | #144, #147 | Review the rough layout, names and interaction states; refine any feedback before production UI work. |
| Fact foundation | #142 | Stable common topics and source/review states; adapters preserve existing profile/preferences, saved answers and cache data. Agree the small interface contract first. |
| CV and profile | #143, #144 | Evidence-backed prefill plus the reusable onboarding/profile panel. Imports and saves cannot overwrite newer user edits. |
| Background review | #145, #146 | Supported answers can be used with durable review evidence; missing required facts defer only one job. Grouped, paginated review API and safe corrections. |
| Candidate follow-up | #147 | Quiet notification summary, review view, separate unread/unresolved counts and useful deep links. |
| Qualification | #148 | Migration, bounded lists, accessibility, concurrency, unchanged warm-cache reuse, bot-continuation fixtures and exact-head CI. |

After the foundation is stable, CV extraction and runtime review recording can proceed independently. Profile UI and inbox APIs can then progress in parallel. Use small reviewable PRs for coherent slices rather than another monolithic change.

## Planning boundaries

- One authoritative candidate fact source; technical mapping records remain a separate background mechanism.
- Human confirmation changes future answer use, not past applications or submission authority.
- Evidence-supported low-confidence phrasing can be reviewed later. Missing factual/consent declarations are not invented; only the affected application is deferred.
- Current approval settings, budgets, privacy controls, native document/identity checks and unknown-submission protection remain.
- Required answer facts, optional fields, provider failures and notification delivery errors remain distinct.
- No model call is needed to load the new tab, view a review item or use an already valid warm mapping.

## Verification plan

- Existing user migration: manually saved answers, country scope, false/null values, salary qualifiers, custom answers and mapping-backed facts survive.
- CV extraction: explicit versus absent/contradictory/stale evidence; user edits take precedence; normal supported import/export behavior is retained.
- UI: five core rows by default, optional topics disclosed, edits survive tab changes and failures, narrow/keyboard/dark-mode states are usable.
- Queue: equivalent variants group; incompatible countries, employers, pay periods and sponsorship timing remain distinct; older paginated items remain confirmable.
- Runtime: supported phrasing is recorded truthfully; a missing required fact defers one fixture job while the next job continues; operational failures are not disguised as candidate uncertainty.
- Correction: atomic version-aware update, safe double confirmation, immutable past answer evidence and no automatic retry of an unknown submission.
- Notifications: grouped, durable, per-user and separate read/resolved counts; ignoring notifications does not pause a run.
- Cost and scalability: zero extra provider requests for warm verified reuse and bounded rendering/API payloads with thousands of cached mappings.
- Release: focused Go/frontend/isolated browser fixtures, lint/build and CI on the actual final head. Existing live-test authorization and VPN/session constraints remain applicable if a later canary is useful.

## Existing work items

Epic: https://github.com/mmbspk/jobifai/issues/141

Stories: #142 common fact catalog; #143 CV prefill; #144 profile/onboarding UI; #145 runtime continuation; #146 confirmation API; #147 notifications/review UI; #148 qualification.

The seven existing story bodies contain detailed acceptance criteria. This plan starts with the user-facing design before committing to deeper technical implementation choices.
