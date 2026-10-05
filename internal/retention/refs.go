package retention

import (
	"context"
	"database/sql"
	"strings"

	"github.com/user/jobifai/internal/documents"
)

// ProtectionIndex lists export paths and blob storage keys that must not be deleted.
type ProtectionIndex struct {
	ExportPaths map[string]struct{}
	BlobKeys    map[string]struct{}
}

func (p ProtectionIndex) exportProtected(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return true
	}
	_, ok := p.ExportPaths[path]
	return ok
}

func (p ProtectionIndex) blobProtected(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return true
	}
	_, ok := p.BlobKeys[key]
	return ok
}

type versionMeta struct {
	reconstructible bool
	artifactKey     string
	originalKey     string
	artifactEvicted bool
}

func buildProtectionIndex(ctx context.Context, db DB, userID string, retainLatestN int) (ProtectionIndex, error) {
	out := ProtectionIndex{
		ExportPaths: make(map[string]struct{}),
		BlobKeys:    make(map[string]struct{}),
	}
	addExport := func(p string) {
		p = strings.TrimSpace(p)
		if p != "" {
			out.ExportPaths[p] = struct{}{}
		}
	}
	addBlob := func(k string) {
		k = strings.TrimSpace(k)
		if k != "" {
			out.BlobKeys[k] = struct{}{}
		}
	}

	versionIDs := make(map[string]struct{})
	collectRow := func(resumePath, coverPath, resumeVID, coverVID, packJSON string) {
		addExport(resumePath)
		addExport(coverPath)
		if id := strings.TrimSpace(resumeVID); id != "" {
			versionIDs[id] = struct{}{}
		}
		if id := strings.TrimSpace(coverVID); id != "" {
			versionIDs[id] = struct{}{}
		}
		pack := documents.ParseApplicationPackJSON(packJSON)
		if p := pack.ResumePath(); p != "" {
			addExport(p)
		}
		if p := pack.CoverPath(); p != "" {
			addExport(p)
		}
		if id := strings.TrimSpace(pack.Resume.ContentVersionID); id != "" {
			versionIDs[id] = struct{}{}
		}
		if id := strings.TrimSpace(pack.Cover.ContentVersionID); id != "" {
			versionIDs[id] = struct{}{}
		}
		for _, r := range pack.Refs {
			if p := strings.TrimSpace(r.LocalPath); p != "" {
				addExport(p)
			}
			if id := strings.TrimSpace(r.ContentVersionID); id != "" {
				versionIDs[id] = struct{}{}
			}
		}
	}

	rows, err := db.QueryContext(ctx, `
		SELECT COALESCE(resume_path,''), COALESCE(cover_letter_path,''),
		       COALESCE(resume_content_version_id,''), COALESCE(cover_letter_content_version_id,''),
		       COALESCE(document_refs_json,'')
		FROM jobs_applied WHERE user_id = ? ORDER BY applied_at DESC, id DESC LIMIT ?`, userID, retainLatestN)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var rp, cp, rv, cv, pack string
		if err := rows.Scan(&rp, &cp, &rv, &cv, &pack); err != nil {
			_ = rows.Close()
			return out, err
		}
		collectRow(rp, cp, rv, cv, pack)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return out, err
	}
	_ = rows.Close()

	pending, err := db.QueryContext(ctx, `
		SELECT COALESCE(resume_path,''), COALESCE(cover_letter_path,''),
		       COALESCE(resume_content_version_id,''), COALESCE(cover_letter_content_version_id,''),
		       COALESCE(document_refs_json,'')
		FROM jobs_pending_review WHERE user_id = ?`, userID)
	if err != nil {
		return out, err
	}
	for pending.Next() {
		var rp, cp, rv, cv, pack string
		if err := pending.Scan(&rp, &cp, &rv, &cv, &pack); err != nil {
			_ = pending.Close()
			return out, err
		}
		collectRow(rp, cp, rv, cv, pack)
	}
	if err := pending.Err(); err != nil {
		_ = pending.Close()
		return out, err
	}
	_ = pending.Close()

	approved, err := db.QueryContext(ctx, `
		SELECT COALESCE(resume_path,''), COALESCE(cover_letter_path,'')
		FROM jobs_approved_queue WHERE user_id = ?`, userID)
	if err != nil {
		return out, err
	}
	for approved.Next() {
		var rp, cp string
		if err := approved.Scan(&rp, &cp); err != nil {
			_ = approved.Close()
			return out, err
		}
		collectRow(rp, cp, "", "", "")
	}
	if err := approved.Err(); err != nil {
		_ = approved.Close()
		return out, err
	}
	_ = approved.Close()

	defRows, err := db.QueryContext(ctx, `
		SELECT ud.content_version_id FROM user_document_defaults ud WHERE ud.user_id = ?`, userID)
	if err != nil {
		return out, err
	}
	for defRows.Next() {
		var id string
		if err := defRows.Scan(&id); err != nil {
			_ = defRows.Close()
			return out, err
		}
		if id = strings.TrimSpace(id); id != "" {
			versionIDs[id] = struct{}{}
		}
	}
	if err := defRows.Err(); err != nil {
		_ = defRows.Close()
		return out, err
	}
	_ = defRows.Close()

	metaByID, err := loadVersionMeta(ctx, db, userID)
	if err != nil {
		return out, err
	}
	for id := range versionIDs {
		if m, ok := metaByID[id]; ok {
			addBlob(m.artifactKey)
			addBlob(m.originalKey)
		}
	}
	for id, m := range metaByID {
		if !m.reconstructible {
			addBlob(m.artifactKey)
			addBlob(m.originalKey)
			_ = id
		}
	}

	return out, nil
}

func loadVersionMeta(ctx context.Context, db DB, userID string) (map[string]versionMeta, error) {
	out := make(map[string]versionMeta)
	rows, err := db.QueryContext(ctx, `
		SELECT v.id, v.reconstructible,
		       COALESCE(a.storage_key,''), COALESCE(o.storage_key,''),
		       v.content_kind, v.content_json, COALESCE(v.css_snapshot,''), COALESCE(v.renderer_version,''),
		       COALESCE(v.render_snapshot_json,''), COALESCE(a.evicted_at,'')
		FROM document_content_versions v
		LEFT JOIN document_version_artifact_refs r ON r.content_version_id = v.id
		LEFT JOIN document_render_artifacts a ON a.id = r.artifact_id
		LEFT JOIN document_original_files o ON o.content_version_id = v.id
		WHERE v.user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id string
		var recon int
		var art, orig sql.NullString
		var kind, content, css, renderer, snapshot, evicted string
		if err := rows.Scan(&id, &recon, &art, &orig, &kind, &content, &css, &renderer, &snapshot, &evicted); err != nil {
			return nil, err
		}
		m := out[id]
		m.reconstructible = recon != 0 && documents.ValidateReconstructionInputs(kind, content, css, renderer, snapshot) == nil
		m.artifactEvicted = evicted != ""
		if art.Valid {
			m.artifactKey = art.String
		}
		if orig.Valid {
			m.originalKey = orig.String
		}
		out[id] = m
	}
	return out, rows.Err()
}

func versionIDForExport(packJSON, colVID, path, kind string) string {
	if id := strings.TrimSpace(colVID); id != "" {
		return id
	}
	pack := documents.ParseApplicationPackJSON(packJSON)
	switch kind {
	case documents.KindResume:
		if strings.TrimSpace(pack.Resume.LocalPath) == strings.TrimSpace(path) {
			return strings.TrimSpace(pack.Resume.ContentVersionID)
		}
	case documents.KindCoverLetter:
		if strings.TrimSpace(pack.Cover.LocalPath) == strings.TrimSpace(path) {
			return strings.TrimSpace(pack.Cover.ContentVersionID)
		}
	}
	for _, r := range pack.Refs {
		if r.Kind == kind && strings.TrimSpace(r.LocalPath) == strings.TrimSpace(path) {
			return strings.TrimSpace(r.ContentVersionID)
		}
	}
	return ""
}

func exportEvictAllowed(path, versionID string, meta map[string]versionMeta, idx ProtectionIndex) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	if idx.exportProtected(path) {
		return false
	}
	versionID = strings.TrimSpace(versionID)
	if versionID == "" {
		return false
	}
	m, ok := meta[versionID]
	if !ok || !m.reconstructible {
		return false
	}
	return true
}
