package retention

import (
	"context"
	"strings"

	"github.com/user/jobifai/internal/documents"
)

// BlobRemover deletes durable blob keys (document PDF storage).
type BlobRemover interface {
	Remove(key string) error
}

func (s *Service) evictOrphanArtifactBlobs(ctx context.Context, userID string, retainN int, idx ProtectionIndex, meta map[string]versionMeta) (int, error) {
	if s == nil || s.Blobs == nil {
		return 0, nil
	}
	protectedVersions, err := s.versionIDsReferencedByActiveApplications(ctx, userID, retainN)
	if err != nil {
		return 0, err
	}
	removed := 0
	for id, m := range meta {
		if !m.reconstructible || m.artifactKey == "" {
			continue
		}
		if _, active := protectedVersions[id]; active {
			continue
		}
		if idx.blobProtected(m.artifactKey) {
			continue
		}
		if err := s.Blobs.Remove(m.artifactKey); err != nil {
			continue
		}
		removed++
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
		FROM jobs_applied WHERE user_id = ? ORDER BY applied_at DESC LIMIT ?`, userID, retainN)
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
	_ = pr.Close()
	return out, nil
}
