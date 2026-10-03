package retention

import (
	"context"
	"database/sql"
	"strings"
)

// protectedPaths returns filesystem paths that must not be evicted for userID.
func protectedPaths(ctx context.Context, db DB, userID string, retainLatestN int) (map[string]struct{}, error) {
	out := make(map[string]struct{})
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p != "" {
			out[p] = struct{}{}
		}
	}

	rows, err := db.QueryContext(ctx, `
		SELECT resume_path, cover_letter_path FROM jobs_applied
		WHERE user_id = ? ORDER BY applied_at DESC LIMIT ?`, userID, retainLatestN)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var r, c sql.NullString
		if err := rows.Scan(&r, &c); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if r.Valid {
			add(r.String)
		}
		if c.Valid {
			add(c.String)
		}
	}
	_ = rows.Close()

	for _, q := range []string{
		`SELECT resume_path, cover_letter_path FROM jobs_pending_review WHERE user_id = ?`,
		`SELECT resume_path, cover_letter_path FROM jobs_approved_queue WHERE user_id = ?`,
	} {
		rws, qerr := db.QueryContext(ctx, q, userID)
		if qerr != nil {
			return nil, qerr
		}
		for rws.Next() {
			var r, c sql.NullString
			if err := rws.Scan(&r, &c); err != nil {
				_ = rws.Close()
				return nil, err
			}
			if r.Valid {
				add(r.String)
			}
			if c.Valid {
				add(c.String)
			}
		}
		_ = rws.Close()
	}

	defRows, err := db.QueryContext(ctx, `
		SELECT COALESCE(d.storage_key,''), COALESCE(a.storage_key,'')
		FROM user_document_defaults ud
		LEFT JOIN document_content_versions v ON v.id = ud.content_version_id
		LEFT JOIN document_version_artifact_refs r ON r.content_version_id = v.id
		LEFT JOIN document_render_artifacts a ON a.id = r.artifact_id
		LEFT JOIN document_original_files d ON d.content_version_id = v.id
		WHERE ud.user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	for defRows.Next() {
		var orig, art sql.NullString
		if err := defRows.Scan(&orig, &art); err != nil {
			_ = defRows.Close()
			return nil, err
		}
		if orig.Valid {
			add(orig.String)
		}
		if art.Valid {
			add(art.String)
		}
	}
	_ = defRows.Close()

	nr, err := db.QueryContext(ctx, `
		SELECT a.storage_key FROM document_content_versions v
		JOIN document_version_artifact_refs r ON r.content_version_id = v.id
		JOIN document_render_artifacts a ON a.id = r.artifact_id
		WHERE v.user_id = ? AND v.reconstructible = 0`, userID)
	if err != nil {
		return nil, err
	}
	for nr.Next() {
		var key string
		if err := nr.Scan(&key); err != nil {
			_ = nr.Close()
			return nil, err
		}
		add(key)
	}
	_ = nr.Close()

	return out, nil
}

func pathProtected(protected map[string]struct{}, path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return true
	}
	if _, ok := protected[path]; ok {
		return true
	}
	return false
}
