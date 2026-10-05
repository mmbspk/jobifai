package retention

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/user/jobifai/internal/documents"
)

// BlobRemover deletes durable blob keys (document PDF storage).
type BlobRemover interface {
	Remove(key string) error
}

// collectOrphanBlobKeys returns the storage keys for artifact blobs that are
// eligible for eviction: reconstructible versions not referenced by any active
// application or user default.  No deletions are performed.
func (s *Service) collectOrphanBlobKeys(ctx context.Context, userID string, retainN int, idx ProtectionIndex, meta map[string]versionMeta) ([]string, error) {
	if s == nil || s.Blobs == nil {
		return nil, nil
	}
	protectedVersions, err := s.versionIDsReferencedByActiveApplications(ctx, userID, retainN)
	if err != nil {
		return nil, err
	}
	defRows, err := s.DB.QueryContext(ctx,
		`SELECT content_version_id FROM user_document_defaults WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	for defRows.Next() {
		var id string
		if err := defRows.Scan(&id); err != nil {
			_ = defRows.Close()
			return nil, err
		}
		if id = strings.TrimSpace(id); id != "" {
			protectedVersions[id] = struct{}{}
		}
	}
	if err := defRows.Err(); err != nil {
		_ = defRows.Close()
		return nil, err
	}
	_ = defRows.Close()

	unique := make(map[string]struct{})
	var candidates []string
	for id, m := range meta {
		if !m.reconstructible || m.artifactKey == "" || m.artifactEvicted {
			continue
		}
		if _, active := protectedVersions[id]; active {
			continue
		}
		if idx.blobProtected(m.artifactKey) {
			continue
		}
		if _, seen := unique[m.artifactKey]; !seen {
			unique[m.artifactKey] = struct{}{}
			candidates = append(candidates, m.artifactKey)
		}
	}
	return candidates, nil
}

// countOrphanBlobCandidates returns the number of artifact blobs eligible for
// eviction without performing any deletions.
func (s *Service) countOrphanBlobCandidates(ctx context.Context, userID string, retainN int, idx ProtectionIndex, meta map[string]versionMeta) (int, error) {
	keys, err := s.collectOrphanBlobKeys(ctx, userID, retainN, idx, meta)
	return len(keys), err
}

func (s *Service) evictOrphanArtifactBlobs(ctx context.Context, userID string, retainN int, idx ProtectionIndex, meta map[string]versionMeta, batchLimit int) (int, error) {
	if batchLimit <= 0 {
		batchLimit = defaultBatchSize
	}
	candidates, err := s.collectOrphanBlobKeys(ctx, userID, retainN, idx, meta)
	if err != nil {
		return 0, err
	}
	sort.Strings(candidates)
	if len(candidates) > batchLimit {
		candidates = candidates[:batchLimit]
	}
	removed := 0
	var firstErr error
	for _, key := range candidates {
		if err := s.Blobs.Remove(key); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("remove artifact %q: %w", key, err)
			}
			continue
		}
		if _, err := s.DB.ExecContext(ctx, `UPDATE document_render_artifacts SET evicted_at = datetime('now') WHERE user_id = ? AND storage_key = ? AND evicted_at IS NULL`, userID, key); err != nil {
			return removed, err
		}
		removed++
	}
	if firstErr != nil {
		return removed, firstErr
	}
	return removed, nil
}

func (s *Service) versionIDsReferencedByActiveApplications(ctx context.Context, userID string, retainN int) (map[string]struct{}, error) {
	out := make(map[string]struct{})
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id != "" {
			out[id] = struct{}{}
		}
	}
	scan := func(rv, cv, pack string) {
		add(rv)
		add(cv)
		p := documents.ParseApplicationPackJSON(pack)
		add(p.Resume.ContentVersionID)
		add(p.Cover.ContentVersionID)
		for _, r := range p.Refs {
			add(r.ContentVersionID)
		}
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT COALESCE(resume_content_version_id,''), COALESCE(cover_letter_content_version_id,''),
		       COALESCE(document_refs_json,'')
		FROM jobs_applied WHERE user_id = ? ORDER BY applied_at DESC, id DESC LIMIT ?`, userID, retainN)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var rv, cv, pack string
		if err := rows.Scan(&rv, &cv, &pack); err != nil {
			_ = rows.Close()
			return nil, err
		}
		scan(rv, cv, pack)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()

	pr, err := s.DB.QueryContext(ctx, `
		SELECT COALESCE(resume_content_version_id,''), COALESCE(cover_letter_content_version_id,''), COALESCE(document_refs_json,'')
		FROM jobs_pending_review WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	for pr.Next() {
		var rv, cv, pack string
		if err := pr.Scan(&rv, &cv, &pack); err != nil {
			_ = pr.Close()
			return nil, err
		}
		scan(rv, cv, pack)
	}
	if err := pr.Err(); err != nil {
		_ = pr.Close()
		return nil, err
	}
	_ = pr.Close()
	return out, nil
}
