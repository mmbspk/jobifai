# Application answers foundation

Candidate values remain in the existing profile and preferences. Ten stable catalog IDs define the compact UI; learned variants remain in the PR #138 mapping cache. Migration 061 adds separate source, validated import, grouped review, encounter and notification records. Migration 060 is the existing registered Go schema reconciliation.

Common-answer saves patch only supplied topic IDs. A factual answer revision prevents concurrent answer changes from being overwritten while unrelated CV changes are preserved by rereading inside the same transaction. Full-profile saves support the flat legacy JSON plus an optional `profile_revision`/If-Match precondition. No evidence, badge, notification or review timestamp enters the stored profile or its grounding digest.

Clear CV evidence is captured as unsaved import metadata. An explicit profile/topic save attaches that origin only when the saved topic exactly matches the validated draft value. Ambiguous sidecars remain suggestions. Candidate changes override the prior origin, and mismatched source fingerprints are not displayed as current evidence.

Review groups preserve field semantics, country, employer/job scope, option and temporal/pay/credential context. The recorder accepts caller-stable IDs; replay with altered immutable content is rejected. A successful durable proposal record precedes provisional filling. A later fill record stores exact actual text once. Final submission and unknown outcomes are derived by joining the same user's attempt; corrections cannot change old encounters.

The inbox loads bounded pages, count and unread state in one read snapshot. Repeated active groups do not create another unread notification. Reading the grouped notification never confirms an answer. Corrections use an owned ID and item/fact revisions, update candidate facts and validate every associated mapping in the same transaction. Mappingless facts preserve employer/job qualifiers in candidate-authored answers and remain subject to runtime evidence scope checks.

Related fact changes mark affected reviews outdated while unrelated missing facts stay actionable. Confirmation never starts a bot, retries a submission, or changes submission authority.

Qualification includes race-enabled storage/handler tests for legacy facts, false/null, scoped grouping, concurrent saves, CV-origin matching, metadata-only digest stability, unread/unresolved separation, old-page confirmation, atomic rollback and immutable submitted/unknown evidence.
