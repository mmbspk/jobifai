# Issue #60 acceptance matrix (PR #60 — document retention & LLM reuse)

Epic #40 stays open; this PR does not change byte caps, queue caps, or cloud provisioning.

| Requirement | Evidence |
|-------------|----------|
| Persisted admin retention default (latest N submitted applications per user; resume + cover count as one application) | `domain.DocumentRetentionDefaults` (default 20, min 5, max 30); `retention.LoadDefaults` / `SaveDefaultsAudited`; `GET\|PUT /api/admin/retention/defaults`; `TestAdmin_RetentionDefaults_AuditAndClamp` |
| Audited admin changes | `document_retention_audit` migration; `insertAudit`; `GET /api/admin/retention/audit`; `TestAdmin_RetentionDefaults_AuditAndClamp` |
| Safe PDF eviction beyond N with preview + bounded batches | `PreviewEviction`, `RunEviction` (`defaultBatchSize` 25, max 100); `GET /api/admin/retention/preview/{user_id}`; `POST /api/admin/retention/run/{user_id}`; `TestService_RunEviction_21Applications_KeepsLatest20` |
| Protected paths: latest N submitted, pending/approved queue, defaults, non-reconstructible artifacts, shared refs with retained jobs | `protectedPaths` in `protect.go`; `TestService_RunEviction_ProtectsSharedPathWithRetainedJob` |
| Post-submit cleanup hook | `Manager.SetRetention`, `AfterSuccessfulSubmit` after successful apply in `runSubmit` |
| Deletion failures surfaced in metrics (no resume PII) | `Metrics.DeleteFailures`; `TestService_RunEviction_DeleteFailureIncrementsMetric`; structured log `retention_cleanup` |
| Temp file cleanup | `cleanTempFiles` (`.tmp` under user job_applications tree) |
| Per-user eviction lock / race reduction | `userMu` in `retention.Service` |
| Admin limit change 5 → 30 affects retention | `TestService_RunEviction_AdminLimit5To30` |
| User isolation | `TestService_PreviewEviction_UserIsolation`; scoped `user_id` on cache and jobs |
| Historical download: reconstruct from saved content, zero LLM credits | Existing `TestService_PDFBytes_ReconstructWhenBlobMissing` (`internal/documents/service_pdf_test.go`) — eviction clears paths only, content versions remain |
| Exact LLM generation reuse (user + task + content fingerprint) | `llmreuse.ContentFingerprint`, `llm_generation_cache`; `Client.WithReuse` / `reuseBegin` cache hit skips provider |
| Visual identity separate from reuse scope | `VisualIdentityHash`; `TestVisualIdentityHash_SeparateFromContent`; cache key excludes visual hash |
| Concurrent reuse coordination | `Begin` lease + wait; `TestStore_ConcurrentBegin_WaitsForInProgressLease` |
| Crash / provider uncertainty | `StateFailedUncertain`, `MarkFailedUncertain`, stale lease recovery; `TestStore_MarkFailedUncertain_AllowsFreshProviderCall` |
| Billing idempotency (logical operation) | `usage.Ledger` idempotency on `OperationID`; `TestLedger_IdempotentOperationID`; reuse stores `operation_id` on cache rows |
| Metrics without resume PII | `retention.Metrics` JSON fields are counts only |

Out of scope (per #60): byte caps, queue caps, cloud provisioning, unrelated product changes.
