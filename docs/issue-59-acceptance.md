# Issue #59 acceptance matrix (PR #62)

| Requirement | Evidence |
|-------------|----------|
| Per-application document picker (resume/cover, originals, site/skip) | `GET\|PUT /api/bot/review/{job_id}/documents`; `ReviewDocumentsPanel.tsx`; `applyStoredPackMetadata` + `ParseApplicationPackJSON` metadata preservation; `TestReviewDocuments_SiteSkipPutGetRoundTrip` |
| Server validates ownership + kind | `Store.ValidateVersionKind`, `Service.ValidateUserVersionKind`; `TestReviewPutDocuments_InvalidatesPrepareAndValidatesKind` |
| Selection change invalidates preparation | `PackAfterSelectionChange`, `ReviewPutDocuments` clears paths + `prepared`; UI invalidates `review-pending` |
| Preflight + AI credit UX | `ReviewDocumentPreflight` (`action`, `uses_ai`, `credits_estimate`); honest unknown caps before Prepare |
| Independent overrides | `TestResolver_IndependentOverrides`, `TestResolver_ApplicationOverrideSiteAndSkip` |
| Originals preserve bytes | `TestService_OriginalUpload_NonReconstructible`, `TestDocuments_SetDefaultOriginalUploadResume` |
| Required-cover holds | `TestResolver_SkipOptionalCoverWhenNotRequired`, resolver hold when `CoverSkip` + required |
| Quota / partial materialize | `TestMaterializeSequence_OneTailorAcrossResumeThenCoverCaps`, `form_quota_test.go` |
| Zero LLM on reuse/render | `TestService_ReconstructPDF_ZeroRendererCallsWhenArtifactExists`, `TestResolveReviewPreflight_ReuseZeroCredits` |
| Prepare never submits | `applyPrimaryAllowsPrepareNav`, `TestLinkedInPrepareScanComplete_ReviewCTADoesNotComplete`, Seek prepare stops at submit button |
| Uploads match approved pack | `PackFromPrepared`, form kind-based upload, `form_scan_test.go` DOM fixtures |
| Frozen approve gate | `ReadyForSubmit`, `ReviewApprove` 409, `reviewDocumentsReady()` |

Out of scope: Epic #40 retention/cache (PR #60).
