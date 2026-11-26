# Epic 154 production qualification — 26 November 2026

The deployed source is the clean merge of PR 167, ee81432cd133849be33c0563073f8bb6ddf4fcbd.
The operator is admin@jobifai.local (__default__). All live reads below used the
actual deployed worker canary and saved jobsite sessions. They did not score,
generate/upload documents, fill forms or submit applications.

## Live worker results

Recovery/reuse initially off:

| Listing | Result | Characters | Reader latency | Model requests |
|---|---|---:|---:|---:|
| LinkedIn 4491867472, Midnite | verified deterministic read | 5022 | 6896 ms | 0 |
| LinkedIn 4494759546, Sharon AI | verified deterministic read | 5526 | 6086 ms | 0 |
| SEEK 95987525, m-View (previous failed AI Apply listing) | verified deterministic read | 5454 | 8805 ms | 0 |
| SEEK 96103223, RAA | verified deterministic read | 4092 | 11759 ms | 0 |
| SEEK 96007786, G'day Group | verified deterministic read | 4361 | 12348 ms | 0 |

Dentsu 4484814584 was intermittent with recovery off: its initial read returned
missing after 8096 ms, a later read returned incomplete after 9136 ms, and the next
read verified its current 4266-character description in 5282 ms. Both failures
remained explicit errors with no accepted description. They are not counted as
successful canaries.

The bounded operator pilot passed all repeated dentsu reads. Two repeats used
the deterministic reader (4266 characters, 5218/7177 ms); the next recovered via
fresh scoped rescan (4221 normalized characters, 12329 ms elapsed, 12311 ms browser
time, zero extra recovery actions and zero model requests). Both paths identified
Senior Software Engineer, Platforms & Cloud at dentsu. The character-count
variation reflects native paragraph formatting versus normalized DOM text.

Across 14 live canary reads: 12 successes, two explicit dentsu native failures,
zero model HTTP requests and zero model cost. The adaptive-enabled repeats all
passed, including the actual rescan recovery. Browser usage followed normal
account metering; zero model cost does not imply zero browser cost.

Read-only observation of the actual worker page recorded an empty description
placeholder, then populated clipped text and its physical aria-hidden More
control. Focus was present in that observation. This supports a hydration/state
race; it does not identify every cause of the intermittent native result. The
previous production failure capture and subsequent Midnite fix are documented
in production-canary.md.

## Controlled learning in the deployed runtime

The reader test harness used the deployed image
sha256:524f901f99fe3bf8d86a3e4a1f9c698e8b2fedc2c84b1ac7f76a2bfbd58a41f1,
Linux x86_64 and Debian 12 Chromium 156.0.8078.108. Reader implementation sources
matched the deployed merge; this qualification adds test coverage only. The
statically linked harness was built with the local Go toolchain and ran inside
the existing container using separate headless browser profiles, temporary test
databases and a loopback fixture provider. It did not populate production recipes
or use production LLM credentials.

All 89 top-level shared-reader tests passed in that environment, including the
full DOM → configured HTTP client → independent verifier → durable recipe → fresh
replay chain for both LinkedIn and SEEK. SEEK's fixture uses the primary job apply
reference as identity evidence and asserts that it cannot grant application input.

The provider's usage values below are fixture-reported values for accounting
verification, not measured production token usage or actual provider charges:

| Board | Read | Source | HTTP requests | Input/output tokens | Fixture cost (micro-USD) | Browser ms | Elapsed ms |
|---|---|---|---:|---:|---:|---:|---:|
| LinkedIn | cold | verified model | 2 | 20/10 | 70 | 158 | 178 |
| LinkedIn | different-job warm | verified recipe | 0 | 0/0 | 0 | 79 | 82 |
| LinkedIn | changed layout | verified model | 2 | 20/10 | 70 | 152 | 161 |
| SEEK | cold | verified model | 2 | 20/10 | 70 | 207 | 219 |
| SEEK | different-job warm | verified recipe | 0 | 0/0 | 0 | 92 | 94 |
| SEEK | changed layout | verified model | 2 | 20/10 | 70 | 193 | 205 |

Golden assertions require the current different listing's text, no cold-text
leakage, a durable verified recipe, zero warm interpretation, and changed-layout
re-entry with a new recipe. The suite also covers wrong-job/origin/auth/absent and
partial content, hostile proposals, dynamic hydration, expansion replacement,
concurrent misses, restart, persistence failure, retirement, rollback, quota,
request/token/time/cost bounds and cancellation. Caller/integration guards and
full Go/frontend/browser CI passed for the deployed PR 167 source at c8915ca.

## Caller qualification

TestJobPageManagerLearningAndCancellationBothBoards exercises the assembled
Manager reader factory and the registered worker browser on both boards. It
uses ReadJobPage for cold/changed reads and FetchJobPage, the Generate bridge,
for a different-job warm read and cancellation during an actual HTTP request.
Cold and changed reads each use proposal plus independent verification; warm
events retain the same recipe and record zero model requests with current text.
Canceled requests remain typed reading_canceled events, cause no retry or recipe
promotion, and preserve the read-only Apply guard.

The fixture also plants a stale personal proxy URL and unrelated personal direct
key. Actual HTTP requests must use the deployment proxy credential and the
approved job_page_reading policy (Haiku 4.5), overriding the Sonnet global model.
Existing preparation/scoring/handler tests cover the other caller boundaries.
The focused local race run passed (bot 45.565 s, jobpage 10.776 s), and lint on
both packages reported zero issues. The provider remains a loopback fixture,
so its reported token usage verifies accounting without actual model charges.
The same caller fixture passed inside the deployed container (41.82 s), using
separate browser profiles and temporary databases. Measured caller results:

| Board | Read | HTTP requests | Browser ms | Elapsed ms |
|---|---|---:|---:|---:|
| LinkedIn | cold | 2 | 8149 | 8162 |
| LinkedIn | different-job warm | 0 | 8075 | 8078 |
| LinkedIn | changed layout | 2 | 8144 | 8156 |
| SEEK | cold | 2 | 576 | 585 |
| SEEK | different-job warm | 0 | 574 | 577 |
| SEEK | changed layout | 2 | 591 | 599 |

Cold/changed accounting again records the fixture's 20/10 tokens and 70
micro-USD; warm records zero. Cancellation stops after one HTTP attempt on each
board with unknown provider usage/final cost rather than claiming a free failed
request. LinkedIn's native adapter still waits about eight seconds on this
unfamiliar fixture before replay, so zero interpretation does not imply a large
caller latency reduction. These are controlled fixture timings, not live-job
provider performance measurements.

## Acceptance evidence

| Story | Implementation and verification |
|---|---|
| #155 | Versioned types/validation/browser snapshots; owned refs, origin/identity, privacy and sanitized worker-layout fixtures. |
| #156 | Bounded local recovery and native expansion; hydration, zero-height/display:contents, replacement, sticky header and wrong-job browser regressions; live dentsu rescan recovery. |
| #157 | Typed model proposal and independent verifier; hostile/malformed/fabricated/ambiguous selections, policy, provider, quota and cancellation fixtures. |
| #158 | Durable structural SQL recipes; different-job warm replay, drift/relearning, restart/concurrency, failed persistence and retirement tests. |
| #159 | Shared Bot/Manager/Generate reader hooks; cold/warm/changed/cancellation caller fixtures on both boards, preparation/scoring/failure/Submit regressions and deployment credential/policy assertions. |
| #160 | Usage attribution and per-read limits; audited independent rollout/reuse controls, retirement and private-capture bounds, admin handler/browser fixtures. |
| #161 | Golden browser/model replays, deployed runtime suites, actual worker canary and failure observation, measured accounting, rollback/retirement runbook and this report. |

Implementation is deployed through PRs 163, 166 and 167. This qualification
change adds tests and evidence only. Its own exact-head CI remains the final
verification gate before the qualification PR is ready to merge.

## Rollout and remaining limits

Recovery and reuse remain enabled only for the qualified operator (__default__).
The pilot caps each read at two model requests, 24000 tokens, US$0.15 model cost,
four recovery actions and 60 seconds; other snapshot/description caps remain
unchanged. Diagnostic private captures are off. Other accounts remain outside
the pilot. The operator's bot is idle and its applications-today count remains zero.

Missing, expired, unauthenticated, ambiguous or budget-exhausted pages can still
fail explicitly and remain unscored. Native-only reading can encounter the dentsu
race; the adaptive-enabled read was verified to recover it. No live listing
required paid LLM interpretation or production recipe learning during this
canary; those paths were exercised with the clearly identified fixture provider.
No universal jobsite availability or unmeasured production savings are claimed.

Hot rollback can independently disable recovery or reuse. Recipe retirement and
capture retention/access controls are described in the adaptive-job-page-reading
runbook. Broad rollout is a separate operational step beyond this operator pilot.
