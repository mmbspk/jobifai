# Issue #60 acceptance matrix (PR #63)

Epic #40 stays open. Byte caps, queue caps, and cloud provisioning are outside this PR.

## Retention and historical downloads

| Requirement | Implementation and regression evidence |
|---|---|
| Latest N submitted applications, default 20 | Deterministic `applied_at, id` ordering; `TestService_RunEviction_21Applications_KeepsLatest20` |
| Originals, defaults, shared references, queues protected | Version-scoped protection index; query and cursor errors abort deletion; default/shared-path tests |
| Reconstructibility proven before eviction | `ValidateReconstructionInputs` checks content, CSS, renderer and render snapshot; `TestService_MissingSnapshotProtectsExportAndBlob` |
| Cleanup excludes active work and other cleanup | `ActivityCoordinator`; `TestActivityCoordinator_TwoEvictionsRemainExclusive` and cancellation/active-work tests |
| Guard lasts through application publication | Guards cover review Prepare, review Submit, ApplyFromURL and automatic LinkedIn/Seek submission; nested materialization/download guards remain valid; manager/automatic guard-entry and publication-before-cleanup regressions |
| Bounded, resumable artifact cleanup | Migration 038 records `evicted_at`; keys deduplicated and sorted before batching; `TestService_ArtifactBatchesDrainAndSecondReconcileIsEmpty` |
| Accurate PDF availability after eviction | Artifact lookup and document listing exclude evicted artifacts; rendering/linking a new artifact clears its eviction marker |
| Partial export cleanup | Per-artifact path/pack reconciliation, with retry after failed persistence |
| Historical reconstruction has no permanent retention bypass | `TestService_PDFBytes_ReconstructWhenBlobMissing` verifies reconstructed download does not repersist the blob |
| Admin policy and audit are atomic | Transactional audited save; validated limits, admin UI, reconciliation on policy change |

## Generation reuse and billing

| Requirement | Implementation and regression evidence |
|---|---|
| Exact generation identity | User, task, provider, model, effort, max tokens and messages; model-specific client copies retain reuse configuration |
| Explicit AI action can generate afresh | Document AI handlers set `BypassReuse` |
| Request-scoped validated publication | `ChatValidated` keeps response, validator and lease in one invocation; no shared pending slot; reverse-order concurrent-validation test |
| Empty/invalid document output is retryable | Resume content and cover prose validators; `TestTailor_RejectsEmptyDocumentsBeforeCaching` |
| Lease fencing | Fresh lease token separate from persistent operation ID; stale completion, failure and renewal all return `ErrLeaseLost` |
| Renewal failure stops the provider | Immutable heartbeat context, bounded renewal, cancellation on database error/lost ownership; heartbeat regressions run under `-race` |
| Concurrent reuse bills once | Real Client → SQLite cache → usage ledger → quota test, `TestReuse_ConcurrentSamePromptBillsOnce` |
| Validation failure/recovery does not rebill the logical operation | `TestReuse_ValidationFailureRetryAndCrashRecoveryDeduplicateBilling`; persistent operation retained across reclaim |
| Canceled work releases its lease | Cancellation regression; bounded cleanup uses a context independent of request cancellation |

## Validation scope

The concurrency, cache and billing tests use HTTP provider fixtures and a real SQLite database. The billing fixture verifies ledger event counts and quota burns. Crash recovery simulates a persisted charge followed by an expired unfinished cache entry. Provider-side charges cannot be guaranteed exactly once after an uncertain network outcome.

Platform coverage remains fixture-based: no live employer submission is performed. Coordination is process-local, matching the current local filesystem deployment; multi-process storage coordination is outside this implementation.

Local validation for this revision: all Go packages except the longer-running handler suite passed with `-race`; the handler suite is rerunning with a 15-minute timeout after the full run's four-minute per-package timeout. Go lint passes. Frontend tests (162), production build and lint pass, with one existing React hook warning. GitHub Actions must validate the pushed revision before merge.
