# Issue #60 acceptance matrix (PR #63 — document retention & LLM reuse)

Epic #40 stays open. Byte caps, queue caps, and cloud provisioning remain out of scope.

## Round 1 — retention safety & historical downloads

| Requirement | Evidence |
|-------------|----------|
| Evict only reconstructible exports (version-scoped) | `exportEvictAllowed`, `loadVersionMeta`; `TestService_RunEviction_SkipsWithoutReconstructibleVersion` |
| Protection via version/artifact refs, not path string equality across stores | `buildProtectionIndex` in `refs.go` (export paths + blob keys from versions, defaults, non-reconstructible) |
| Artifact-level retention for orphan reconstructible blobs | `evictOrphanArtifactBlobs`; `BlobRemover` on `retention.Service` |
| Historical PDF download without repersisting blobs | `ReconstructPDF` no longer calls `persistArtifact`; `TestService_PDFBytes_ReconstructWhenBlobMissing` asserts blob still absent |
| Work/eviction coordination | `ActivityCoordinator` + `documents.Service.WorkGuard` on `PDFBytes`; eviction under `WithEviction` |
| Abandoned `.tmp` cleanup only | `cleanTempFiles` with `tempMinAge` (5m) |
| Per-artifact DB/pack reconcile on eviction | `evictOneExport` updates path + `document_refs_json` + `retention_paths_cleared` per artifact |
| Admin validation, audit-before-policy, reconcile on change | `DocumentRetentionDefaults.Validate`; audit insert before `SaveDefaults`; `ScheduleReconcileAllUsers`; `TestAdmin_RetentionDefaults_AuditAndClamp` |
| Backlog drain + auto-submit hooks | `ReconcileUser`; `AfterSuccessfulSubmit` on review approve + AI apply inserts |
| Admin UI | `/admin/retention`, `AdminRetentionPage`, `adminApi.retention` |

## Round 2 — reuse coordination, identity, billing

| Requirement | Evidence |
|-------------|----------|
| Reuse includes provider/model/max_tokens in fingerprint | `ContentFingerprint(..., provider, model, maxTokens, ...)` |
| Coordination errors fail closed | `reuseBegin` returns error; `Chat` aborts on `reuseErr` |
| Quota after cache lookup | `checkQuota` after cache hit branch in `Client.Chat` |
| SQLite-comparable lease expiry | `datetime('now', '+3 minutes')` in cache lease SQL |
| Lease-owner fenced complete/failure | `Complete` / `MarkFailedUncertain` `AND lease_owner = ?` |
| Persistent operation id through begin | `BeginResult.OperationID` applied to call context before provider/quota |
| Bot clients use reuse | `Manager.SetLLMReuse`, `userLLMClient` `WithReuse` |
| Explicit fresh generation | `LLMCallContext.BypassReuse` |
| Billing idempotency (logical op) | `TestLedger_IdempotentOperationID` (ledger); operation id on cache rows |

## Known follow-ups (not claiming done)

| Gap | Notes |
|-----|--------|
| Cache only **validated** document JSON (not raw Chat) | Reuse still stores provider `Chat` output; document validation layer not wired to cache complete |
| End-to-end billing + reuse integration test | Ledger idempotency test does not exercise `Client.Chat` + cache + ledger together |
| Bot apply file writes hold `ActivityCoordinator` | `PDFBytes` guarded; platform export writes during apply may still overlap eviction (narrow window) |
