package documents

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

var ErrNotFound = errors.New("document not found")
var ErrForbidden = errors.New("document forbidden")

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) CreateDocument(ctx context.Context, userID, kind, title string) (string, error) {
	id := uuid.NewString()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO user_documents (id, user_id, kind, title) VALUES (?, ?, ?, ?)`,
		id, userID, kind, title)
	return id, err
}

type InsertVersionParams struct {
	DocumentID          string
	UserID              string
	Source              string
	ContentKind         string
	ContentJSON         string
	ProfileSnapshotJSON string
	ProfileSnapshotHash string
	Market              string
	DocumentLanguage    string
	StyleName           string
	CSSFilePath         string
	CSSSnapshot         string
	RendererVersion     string
	Reconstructible     bool
}

func (s *Store) InsertVersion(ctx context.Context, p InsertVersionParams) (versionID string, versionNum int, err error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(version_number), 0) FROM document_content_versions WHERE document_id = ?`, p.DocumentID).Scan(&n); err != nil {
		return "", 0, err
	}
	versionNum = n + 1
	versionID = uuid.NewString()
	rec := 1
	if !p.Reconstructible {
		rec = 0
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO document_content_versions (
			id, document_id, user_id, version_number, source, content_kind, content_json,
			profile_snapshot_json, profile_snapshot_hash, market, document_language,
			style_name, css_file_path, css_snapshot, renderer_version, reconstructible
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		versionID, p.DocumentID, p.UserID, versionNum, p.Source, p.ContentKind, p.ContentJSON,
		nullIfEmpty(p.ProfileSnapshotJSON), nullIfEmpty(p.ProfileSnapshotHash), nullIfEmpty(p.Market),
		nullIfEmpty(p.DocumentLanguage), nullIfEmpty(p.StyleName), nullIfEmpty(p.CSSFilePath),
		p.CSSSnapshot, p.RendererVersion, rec)
	if err != nil {
		return "", 0, err
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE user_documents SET updated_at = datetime('now') WHERE id = ?`, p.DocumentID)
	return versionID, versionNum, nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (s *Store) GetVersionRow(ctx context.Context, userID, versionID string) (docID string, p InsertVersionParams, err error) {
	var rec int
	err = s.db.QueryRowContext(ctx, `
		SELECT document_id, user_id, source, content_kind, content_json,
			COALESCE(profile_snapshot_json,''), COALESCE(profile_snapshot_hash,''),
			COALESCE(market,''), COALESCE(document_language,''),
			COALESCE(style_name,''), COALESCE(css_file_path,''), css_snapshot,
			COALESCE(renderer_version,''), reconstructible
		FROM document_content_versions WHERE id = ?`, versionID).Scan(
		&docID, &p.UserID, &p.Source, &p.ContentKind, &p.ContentJSON,
		&p.ProfileSnapshotJSON, &p.ProfileSnapshotHash, &p.Market, &p.DocumentLanguage,
		&p.StyleName, &p.CSSFilePath, &p.CSSSnapshot, &p.RendererVersion, &rec,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return "", p, ErrNotFound
	}
	if err != nil {
		return "", p, err
	}
	if p.UserID != userID {
		return "", p, ErrForbidden
	}
	p.DocumentID = docID
	p.Reconstructible = rec == 1
	return docID, p, nil
}

func (s *Store) OriginalFileForVersion(ctx context.Context, userID, versionID string) (storageKey, filename, mediaType string, err error) {
	err = s.db.QueryRowContext(ctx, `
		SELECT storage_key, filename, media_type FROM document_original_files
		WHERE content_version_id = ? AND user_id = ?`, versionID, userID).Scan(&storageKey, &filename, &mediaType)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", "", ErrNotFound
	}
	return storageKey, filename, mediaType, err
}

func (s *Store) InsertOriginalFile(ctx context.Context, userID, documentID, versionID, filename, mediaType, storageKey, sha string, size int64) (string, error) {
	id := uuid.NewString()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO document_original_files (id, user_id, document_id, content_version_id, filename, media_type, storage_key, sha256, byte_size)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, userID, documentID, versionID, filename, mediaType, storageKey, sha, size)
	return id, err
}

func (s *Store) LinkArtifact(ctx context.Context, userID, versionID, storageKey, sha string, size int64, templateIdentity string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var artifactID string
	err = tx.QueryRowContext(ctx, `SELECT id FROM document_render_artifacts WHERE user_id = ? AND sha256 = ?`, userID, sha).Scan(&artifactID)
	if errors.Is(err, sql.ErrNoRows) {
		artifactID = uuid.NewString()
		_, insErr := tx.ExecContext(ctx, `
			INSERT INTO document_render_artifacts (id, user_id, content_version_id, storage_key, sha256, byte_size, renderer_version, template_identity, state)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			artifactID, userID, versionID, storageKey, sha, size, RendererVersion, templateIdentity, ArtifactReady)
		if insErr != nil {
			if !isSQLiteUnique(insErr) {
				return insErr
			}
			if err := tx.QueryRowContext(ctx, `SELECT id FROM document_render_artifacts WHERE user_id = ? AND sha256 = ?`, userID, sha).Scan(&artifactID); err != nil {
				return err
			}
		}
	} else if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO document_version_artifact_refs (content_version_id, artifact_id) VALUES (?, ?)
		ON CONFLICT(content_version_id) DO UPDATE SET artifact_id = excluded.artifact_id`,
		versionID, artifactID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ArtifactForVersion(ctx context.Context, userID, versionID string) (storageKey string, sha string, err error) {
	err = s.db.QueryRowContext(ctx, `
		SELECT a.storage_key, a.sha256
		FROM document_version_artifact_refs r
		JOIN document_render_artifacts a ON a.id = r.artifact_id
		JOIN document_content_versions v ON v.id = r.content_version_id
		WHERE r.content_version_id = ? AND v.user_id = ? AND a.state = ?`,
		versionID, userID, ArtifactReady).Scan(&storageKey, &sha)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", ErrNotFound
	}
	return storageKey, sha, err
}

func (s *Store) SetDefault(ctx context.Context, userID, kind, versionID, profileHash, market string) error {
	docID, p, err := s.GetVersionRow(ctx, userID, versionID)
	if err != nil {
		return err
	}
	var docKind string
	if err := s.db.QueryRowContext(ctx, `SELECT kind FROM user_documents WHERE id = ? AND user_id = ?`, docID, userID).Scan(&docKind); err != nil {
		return err
	}
	if kind == KindResume && docKind != KindResume {
		return fmt.Errorf("version is not a resume")
	}
	if kind == KindCoverLetter && docKind != KindCoverLetter {
		return fmt.Errorf("version is not a cover letter")
	}
	_ = p
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO user_document_defaults (user_id, kind, content_version_id, profile_snapshot_hash, market)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id, kind) DO UPDATE SET
			content_version_id = excluded.content_version_id,
			profile_snapshot_hash = excluded.profile_snapshot_hash,
			market = excluded.market,
			selected_at = datetime('now')`,
		userID, kind, versionID, nullIfEmpty(profileHash), nullIfEmpty(market))
	return err
}

func (s *Store) ListDocuments(ctx context.Context, userID string) ([]Document, DefaultsView, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, kind, title, created_at, updated_at FROM user_documents WHERE user_id = ? ORDER BY updated_at DESC`, userID)
	if err != nil {
		return nil, DefaultsView{}, err
	}
	defer func() { _ = rows.Close() }()
	var docs []Document
	for rows.Next() {
		var d Document
		if err := rows.Scan(&d.ID, &d.Kind, &d.Title, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, DefaultsView{}, err
		}
		vers, err := s.listVersions(ctx, d.ID, userID)
		if err != nil {
			return nil, DefaultsView{}, err
		}
		d.Versions = vers
		docs = append(docs, d)
	}
	def, err := s.loadDefaults(ctx, userID)
	return docs, def, err
}

func (s *Store) listVersions(ctx context.Context, docID, userID string) ([]VersionSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT v.id, v.version_number, v.source, v.content_kind, COALESCE(v.market,''), v.reconstructible, v.created_at,
			(SELECT 1 FROM document_version_artifact_refs r WHERE r.content_version_id = v.id LIMIT 1) AS has_pdf
		FROM document_content_versions v
		WHERE v.document_id = ? AND v.user_id = ?
		ORDER BY v.version_number DESC`, docID, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []VersionSummary
	for rows.Next() {
		var v VersionSummary
		var rec, has int
		if err := rows.Scan(&v.ID, &v.VersionNumber, &v.Source, &v.ContentKind, &v.Market, &rec, &v.CreatedAt, &has); err != nil {
			return nil, err
		}
		v.Reconstructible = rec == 1
		v.HasPDF = has == 1
		out = append(out, v)
	}
	return out, nil
}

func (s *Store) loadDefaults(ctx context.Context, userID string) (DefaultsView, error) {
	var d DefaultsView
	_ = s.db.QueryRowContext(ctx, `
		SELECT content_version_id FROM user_document_defaults WHERE user_id = ? AND kind = ?`, userID, KindResume).Scan(&d.ResumeVersionID)
	_ = s.db.QueryRowContext(ctx, `
		SELECT content_version_id FROM user_document_defaults WHERE user_id = ? AND kind = ?`, userID, KindCoverLetter).Scan(&d.CoverLetterVersionID)
	return d, nil
}

type DefaultsMeta struct {
	ResumeOutdated  bool   `json:"resume_outdated"`
	CoverOutdated   bool   `json:"cover_outdated"`
	OutdatedReason  string `json:"outdated_reason,omitempty"`
	ProfileHash     string `json:"profile_hash,omitempty"`
	Market          string `json:"market,omitempty"`
}

func MergeDefaultsMeta(def DefaultsView, meta DefaultsMeta) DefaultsView {
	def.ResumeOutdated = meta.ResumeOutdated
	def.CoverOutdated = meta.CoverOutdated
	def.OutdatedReason = meta.OutdatedReason
	return def
}

func ParseDefaultsMeta(raw []byte) DefaultsMeta {
	var m DefaultsMeta
	_ = json.Unmarshal(raw, &m)
	return m
}

func (s *Store) MarshalMeta(m DefaultsMeta) ([]byte, error) {
	return json.Marshal(m)
}

func isSQLiteUnique(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") || strings.Contains(msg, "constraint failed")
}
