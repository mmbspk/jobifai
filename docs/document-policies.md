# Document policies and resolver (#59)

## Resolution order (per document kind)

1. Explicit application override (review / preflight version id)
2. Saved policy mode in `general_settings.document_policies`
3. Explicitly authorised fallback flags (never enabled by migration)
4. Hold for review — no silent substitution

Resume and cover resolve **independently**.

## Legacy migration (`document_policies.version`)

| Legacy `generate_new_resume_docs` | Resume mode | Cover mode |
|-----------------------------------|-------------|------------|
| `false` | `site` (job-site resume) | `when_required` |
| `true` | `tailor` (per-job tailoring) | `when_accepted` (second file field / accepted upload) |

Migration is idempotent and does **not** set `onboarding_complete` or enable fallbacks.

## Implementation map

- **Resolver:** `internal/documents/resolver.go`
- **Materialize (LLM tailor/generate vs render-only reuse):** `internal/documents/prepare.go`
- **Bot session:** `internal/bot/documents_prepare.go`, `lazyDocGen`
- **Frozen approve:** `SubmitRequest.FrozenDocuments` + content version ids on `jobs_pending_review`

## Provenance

`document_refs_json` on pending/applied rows stores `ApplicationDocumentRef` entries (outcome, version id, local path, site-hosted flag, policy mode).

Rendering and PDF reuse from saved versions call **zero** LLM APIs and consume **zero** generation credits.
