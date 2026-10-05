package documents

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/resume"
)

type ProfileLoader func(userID string) (*domain.ResumeProfile, error)

type PDFRenderer interface {
	RenderResume(ctx context.Context, profile *domain.ResumeProfile, styleName, cssOverride string, opts *resume.RenderOptions) ([]byte, error)
	RenderCoverLetter(ctx context.Context, body string, styleName, cssOverride string) ([]byte, error)
}

// UserWorkGuard coordinates reads/exports with retention eviction (#60).
type UserWorkGuard interface {
	BeginWork(ctx context.Context, userID string) error
	EndWork(userID string)
}

type Service struct {
	Store            *Store
	Blobs            BlobStore
	Renderer         PDFRenderer
	MarketDir        string
	StylesDir        string
	LoadProfile      ProfileLoader
	DefaultsMeta     func(userID string) (DefaultsMeta, error)
	SaveDefaultsMeta func(userID string, m DefaultsMeta) error
	WorkGuard        UserWorkGuard
}

type RenderContext struct {
	Market     string
	StyleName  string
	Language   string
}

func (s *Service) List(ctx context.Context, userID string) (ListResponse, error) {
	docs, def, err := s.Store.ListDocuments(ctx, userID)
	if err != nil {
		return ListResponse{}, err
	}
	if s.DefaultsMeta != nil {
		meta, _ := s.DefaultsMeta(userID)
		meta = CoalesceDefaultsMeta(meta)
		def = MergeDefaultsMeta(def, meta)
	}
	if docs == nil {
		docs = []Document{}
	}
	return ListResponse{Documents: docs, Defaults: def}, nil
}

func (s *Service) GetVersion(ctx context.Context, userID, versionID string) (VersionDetail, error) {
	docID, num, p, err := s.Store.GetVersionRow(ctx, userID, versionID)
	if err != nil {
		return VersionDetail{}, err
	}
	return VersionDetail{
		ID: versionID, DocumentID: docID, ContentKind: p.ContentKind, ContentJSON: p.ContentJSON, VersionNumber: num,
	}, nil
}

func (s *Service) CreateResumeFromProfile(ctx context.Context, userID, title string, rc RenderContext) (versionID string, err error) {
	profile, err := s.requireProfile(userID)
	if err != nil {
		return "", err
	}
	return s.saveResumeVersion(ctx, userID, "", title, SourceProfileRender, profile, rc)
}

func (s *Service) SaveResumeVersion(ctx context.Context, userID, documentID, title, source string, profile *domain.ResumeProfile, rc RenderContext) (versionID string, err error) {
	return s.saveResumeVersion(ctx, userID, documentID, title, source, profile, rc)
}

func (s *Service) saveResumeVersion(ctx context.Context, userID, documentID, title, source string, profile *domain.ResumeProfile, rc RenderContext) (versionID string, err error) {
	docID, err := s.ensureDocument(ctx, userID, documentID, KindResume, defaultTitle(title, "Default resume"))
	if err != nil {
		return "", err
	}
	cssPath, cssSnap, renderSnap, err := s.prepareRenderSnapshot(rc.Market, rc.StyleName)
	if err != nil {
		return "", err
	}
	content, err := json.Marshal(ResumeContent{Profile: *profile})
	if err != nil {
		return "", err
	}
	snapJSON, _ := json.Marshal(profile)
	renderJSON, err := renderSnap.JSON()
	if err != nil {
		return "", err
	}
	versionID, _, err = s.Store.InsertVersion(ctx, InsertVersionParams{
		DocumentID: docID, UserID: userID, Source: source,
		ContentKind: ContentResumeJSON, ContentJSON: string(content),
		ProfileSnapshotJSON: string(snapJSON), ProfileSnapshotHash: ProfileSnapshotHash(profile),
		Market: rc.Market, DocumentLanguage: rc.Language, StyleName: rc.StyleName,
		CSSFilePath: cssPath, CSSSnapshot: cssSnap, RendererVersion: RendererVersion,
		RenderSnapshotJSON: renderJSON, Reconstructible: cssSnap != "" && renderJSON != "",
	})
	if err != nil {
		return "", err
	}
	if _, err := s.renderAndPersistPDF(ctx, userID, versionID, profile, rc.StyleName, cssSnap, RenderOptsFromSnapshot(renderSnap)); err != nil {
		return versionID, err
	}
	return versionID, nil
}

func (s *Service) SaveCoverLetter(ctx context.Context, userID, title, body string, rc RenderContext) (versionID string, err error) {
	return s.SaveCoverLetterWithSource(ctx, userID, title, body, SourceUserEdit, rc)
}

func (s *Service) SaveCoverLetterWithSource(ctx context.Context, userID, title, body, source string, rc RenderContext) (versionID string, err error) {
	return s.SaveCoverLetterWithSourceOnDocument(ctx, userID, "", title, body, source, rc)
}

func (s *Service) AppendCoverVersion(ctx context.Context, userID, documentID, title, body, source string, rc RenderContext) (versionID string, err error) {
	if strings.TrimSpace(documentID) == "" {
		return "", fmt.Errorf("document_id is required")
	}
	return s.SaveCoverLetterWithSourceOnDocument(ctx, userID, documentID, title, body, source, rc)
}

func (s *Service) SaveCoverLetterWithSourceOnDocument(ctx context.Context, userID, documentID, title, body, source string, rc RenderContext) (versionID string, err error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return "", fmt.Errorf("cover letter body is required")
	}
	if source == "" {
		source = SourceUserEdit
	}
	docID, err := s.ensureDocument(ctx, userID, documentID, KindCoverLetter, defaultTitle(title, "General cover letter"))
	if err != nil {
		return "", err
	}
	profile, _ := s.LoadProfile(userID)
	cssPath, cssSnap, renderSnap, err := s.prepareRenderSnapshot(rc.Market, rc.StyleName)
	if err != nil {
		return "", err
	}
	content, _ := json.Marshal(CoverContent{Body: body})
	var snapJSON, hash string
	if profile != nil {
		b, _ := json.Marshal(profile)
		snapJSON = string(b)
		hash = ProfileSnapshotHash(profile)
	}
	renderJSON, err := renderSnap.JSON()
	if err != nil {
		return "", err
	}
	versionID, _, err = s.Store.InsertVersion(ctx, InsertVersionParams{
		DocumentID: docID, UserID: userID, Source: source,
		ContentKind: ContentCoverText, ContentJSON: string(content),
		ProfileSnapshotJSON: snapJSON, ProfileSnapshotHash: hash,
		Market: rc.Market, DocumentLanguage: rc.Language, StyleName: rc.StyleName,
		CSSFilePath: cssPath, CSSSnapshot: cssSnap, RendererVersion: RendererVersion,
		RenderSnapshotJSON: renderJSON, Reconstructible: cssSnap != "" && renderJSON != "",
	})
	if err != nil {
		return "", err
	}
	if _, err := s.renderCoverPDF(ctx, userID, versionID, body, rc.StyleName, cssSnap); err != nil {
		return versionID, err
	}
	return versionID, nil
}

func (s *Service) AppendResumeVersion(ctx context.Context, userID, documentID, title, source string, profile *domain.ResumeProfile, rc RenderContext) (versionID string, err error) {
	if strings.TrimSpace(documentID) == "" {
		return "", fmt.Errorf("document_id is required")
	}
	return s.saveResumeVersion(ctx, userID, documentID, title, source, profile, rc)
}

func (s *Service) StoreOriginalUpload(ctx context.Context, userID, filename, mediaType string, r io.Reader) (versionID string, err error) {
	if filename == "" {
		filename = "upload.bin"
	}
	docID, err := s.Store.CreateDocument(ctx, userID, KindOriginalUpload, filename)
	if err != nil {
		return "", err
	}
	key := filepath.ToSlash(filepath.Join(userID, "originals", uuidNew()+"/"+filepath.Base(filename)))
	sha, size, err := s.Blobs.PutAtomic(key, r)
	if err != nil {
		return "", err
	}
	origID := uuidNew()
	content, _ := json.Marshal(OriginalFileRef{OriginalFileID: origID, Filename: filename, Sha256: sha, ByteSize: size})
	versionID, _, err = s.Store.InsertVersion(ctx, InsertVersionParams{
		DocumentID: docID, UserID: userID, Source: SourceOriginalUpload,
		ContentKind: ContentOriginalFileRef, ContentJSON: string(content),
		RendererVersion: RendererVersion, Reconstructible: false,
	})
	if err != nil {
		return "", err
	}
	if err := s.Store.InsertOriginalFile(ctx, userID, origID, docID, versionID, filename, mediaType, key, sha, size); err != nil {
		return versionID, err
	}
	return versionID, nil
}

func (s *Service) SetDefault(ctx context.Context, userID, kind, versionID string) error {
	profile, _ := s.LoadProfile(userID)
	hash := ProfileSnapshotHash(profile)
	market := ""
	_, _, p, err := s.Store.GetVersionRow(ctx, userID, versionID)
	if err != nil {
		return err
	}
	market = p.Market
	if err := s.Store.SetDefault(ctx, userID, kind, versionID, hash, market); err != nil {
		return err
	}
	if s.SaveDefaultsMeta != nil {
		meta, _ := s.DefaultsMeta(userID)
		switch kind {
		case KindResume:
			meta.ResumeOutdated = false
		case KindCoverLetter:
			meta.CoverOutdated = false
		}
		if !meta.ResumeOutdated && !meta.CoverOutdated {
			meta.OutdatedReason = ""
		}
		meta = CoalesceDefaultsMeta(meta)
		meta.ProfileHash = hash
		meta.Market = market
		norm := NormalizeStyleName(p.StyleName)
		switch kind {
		case KindResume:
			meta.ResumeStyle = norm
		case KindCoverLetter:
			meta.CoverStyle = norm
		}
		_ = s.SaveDefaultsMeta(userID, meta)
	}
	return nil
}

// NotePreferredStyle records that the user changed their preferred document design (shared style picker).
// Resume and cover drift flags are updated in a single meta write. Empty style means Default.
func (s *Service) NotePreferredStyle(ctx context.Context, userID, styleName string) error {
	if s.SaveDefaultsMeta == nil {
		return nil
	}
	_, def, err := s.Store.ListDocuments(ctx, userID)
	if err != nil {
		return fmt.Errorf("list documents: %w", err)
	}
	meta, err := s.DefaultsMeta(userID)
	if err != nil {
		return fmt.Errorf("load defaults meta: %w", err)
	}
	meta = CoalesceDefaultsMeta(meta)
	preferred := NormalizeStyleName(styleName)
	const reason = "preferred document style changed — review your default documents"

	changed := false
	if def.ResumeVersionID != "" && !styleNamesEqual(meta.ResumeStyle, preferred) {
		meta.ResumeOutdated = true
		changed = true
	}
	if def.CoverLetterVersionID != "" && !styleNamesEqual(meta.CoverStyle, preferred) {
		meta.CoverOutdated = true
		changed = true
	}
	if !changed {
		return nil
	}
	meta.OutdatedReason = reason
	if err := s.SaveDefaultsMeta(userID, meta); err != nil {
		return fmt.Errorf("save defaults meta: %w", err)
	}
	return nil
}

func (s *Service) OriginalBytes(ctx context.Context, userID, versionID string) ([]byte, string, string, error) {
	if _, _, p, err := s.Store.GetVersionRow(ctx, userID, versionID); err != nil {
		return nil, "", "", err
	} else if p.ContentKind != ContentOriginalFileRef {
		return nil, "", "", fmt.Errorf("version is not an original upload")
	}
	key, filename, mediaType, err := s.Store.OriginalFileForVersion(ctx, userID, versionID)
	if err != nil {
		return nil, "", "", err
	}
	data, err := s.Blobs.Read(key)
	return data, filename, mediaType, err
}

func (s *Service) PDFBytes(ctx context.Context, userID, versionID string) ([]byte, string, error) {
	if s.WorkGuard != nil {
		if err := s.WorkGuard.BeginWork(ctx, userID); err != nil {
			return nil, "", err
		}
		defer s.WorkGuard.EndWork(userID)
	}
	key, _, err := s.Store.ArtifactForVersion(ctx, userID, versionID)
	if err == nil {
		data, readErr := s.Blobs.Read(key)
		if readErr == nil {
			return data, "application/pdf", nil
		}
		if errors.Is(readErr, ErrBlobNotFound) {
			return s.ReconstructPDF(ctx, userID, versionID)
		}
		return nil, "", readErr
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, "", err
	}
	return s.ReconstructPDF(ctx, userID, versionID)
}

func (s *Service) ReconstructPDF(ctx context.Context, userID, versionID string) ([]byte, string, error) {
	_, _, p, err := s.Store.GetVersionRow(ctx, userID, versionID)
	if err != nil {
		return nil, "", err
	}
	if !p.Reconstructible {
		return nil, "", fmt.Errorf("this document version cannot be reconstructed (legacy or missing rendering snapshot)")
	}
	if p.CSSSnapshot == "" {
		return nil, "", fmt.Errorf("missing CSS snapshot for reconstruction")
	}
	if err := AssertSupportedRenderer(p.RendererVersion); err != nil {
		return nil, "", err
	}
	if p.RenderSnapshotJSON == "" {
		return nil, "", fmt.Errorf("missing render snapshot for reconstruction (version is not reconstructible)")
	}
	renderSnap, err := ParseRenderSnapshot(p.RenderSnapshotJSON)
	if err != nil {
		return nil, "", err
	}
	if err := AssertSupportedRenderer(renderSnap.RendererVersion); err != nil {
		return nil, "", err
	}
	opts := RenderOptsFromSnapshot(renderSnap)
	switch p.ContentKind {
	case ContentResumeJSON:
		var rc ResumeContent
		if err := json.Unmarshal([]byte(p.ContentJSON), &rc); err != nil {
			return nil, "", err
		}
		pdf, err := s.renderResumeWithSnapshot(ctx, &rc.Profile, p.StyleName, p.CSSSnapshot, opts)
		if err != nil {
			return nil, "", err
		}
		return pdf, "application/pdf", nil
	case ContentCoverText:
		var cc CoverContent
		if err := json.Unmarshal([]byte(p.ContentJSON), &cc); err != nil {
			return nil, "", err
		}
		pdf, err := s.renderCoverWithSnapshot(ctx, cc.Body, p.StyleName, p.CSSSnapshot)
		if err != nil {
			return nil, "", err
		}
		return pdf, "application/pdf", nil
	default:
		return nil, "", fmt.Errorf("original uploads cannot be rendered as PDF")
	}
}

func (s *Service) MarkDefaultsOutdated(userID, reason, profileHash, market string) {
	if s.SaveDefaultsMeta == nil {
		return
	}
	meta, _ := s.DefaultsMeta(userID)
	meta.ResumeOutdated = true
	meta.CoverOutdated = true
	meta.OutdatedReason = reason
	if profileHash != "" {
		meta.ProfileHash = profileHash
	}
	if market != "" {
		meta.Market = market
	}
	_ = s.SaveDefaultsMeta(userID, meta)
}

func (s *Service) requireProfile(userID string) (*domain.ResumeProfile, error) {
	p, err := s.LoadProfile(userID)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, fmt.Errorf("no confirmed profile saved yet")
	}
	return p, nil
}

func (s *Service) prepareRenderSnapshot(market, style string) (cssPath, cssSnap string, snap RenderSnapshot, err error) {
	cssPath, cssSnap, err = EffectiveStylesheet(s.StylesDir, s.MarketDir, market, style)
	if err != nil {
		return "", "", RenderSnapshot{}, err
	}
	snap, err = BuildRenderSnapshot(market, s.MarketDir)
	return cssPath, cssSnap, snap, err
}

func (s *Service) ensureDocument(ctx context.Context, userID, documentID, kind, title string) (string, error) {
	if strings.TrimSpace(documentID) == "" {
		return s.Store.CreateDocument(ctx, userID, kind, title)
	}
	gotKind, _, err := s.Store.GetDocument(ctx, userID, documentID)
	if err != nil {
		return "", err
	}
	if gotKind != kind {
		return "", fmt.Errorf("document kind mismatch")
	}
	return documentID, nil
}

func (s *Service) renderAndPersistPDF(ctx context.Context, userID, versionID string, profile *domain.ResumeProfile, styleName, cssSnap string, opts *resume.RenderOptions) ([]byte, error) {
	cssOverride, cleanup, err := writeTempCSS(cssSnap)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	pdf, err := s.Renderer.RenderResume(ctx, profile, styleName, cssOverride, opts)
	if err != nil {
		return nil, err
	}
	return pdf, s.persistArtifact(ctx, userID, versionID, pdf, cssSnap)
}

func (s *Service) renderCoverPDF(ctx context.Context, userID, versionID, body, styleName, cssSnap string) ([]byte, error) {
	cssOverride, cleanup, err := writeTempCSS(cssSnap)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	pdf, err := s.Renderer.RenderCoverLetter(ctx, body, styleName, cssOverride)
	if err != nil {
		return nil, err
	}
	return pdf, s.persistArtifact(ctx, userID, versionID, pdf, cssSnap)
}

func (s *Service) renderResumeWithSnapshot(ctx context.Context, profile *domain.ResumeProfile, style, cssSnap string, opts *resume.RenderOptions) ([]byte, error) {
	cssOverride, cleanup, err := writeTempCSS(cssSnap)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	return s.Renderer.RenderResume(ctx, profile, style, cssOverride, opts)
}

func (s *Service) renderCoverWithSnapshot(ctx context.Context, body, style, cssSnap string) ([]byte, error) {
	cssOverride, cleanup, err := writeTempCSS(cssSnap)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	return s.Renderer.RenderCoverLetter(ctx, body, style, cssOverride)
}

func writeTempCSS(css string) (path string, cleanup func(), err error) {
	f, err := os.CreateTemp("", "doc-css-*.css")
	if err != nil {
		return "", func() {}, err
	}
	if _, err := f.WriteString(css); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", func() {}, err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return "", func() {}, err
	}
	return f.Name(), func() { _ = os.Remove(f.Name()) }, nil
}

func (s *Service) persistArtifact(ctx context.Context, userID, versionID string, pdf []byte, cssSnap string) error {
	sum := sha256.Sum256(pdf)
	sha := hex.EncodeToString(sum[:])
	tid := templateIdentity(cssSnap)
	key := filepath.ToSlash(filepath.Join(userID, "pdf", sha[:2], sha+".pdf"))
	if _, _, err := s.Blobs.PutAtomic(key, bytes.NewReader(pdf)); err != nil {
		return err
	}
	return s.Store.LinkArtifact(ctx, userID, versionID, key, sha, int64(len(pdf)), tid)
}

func templateIdentity(cssSnap string) string {
	if cssSnap == "" {
		return RendererVersion + ":default"
	}
	h := sha256.Sum256([]byte(cssSnap))
	return RendererVersion + ":" + hex.EncodeToString(h[:8])
}

func defaultTitle(title, fallback string) string {
	if strings.TrimSpace(title) == "" {
		return fallback
	}
	return strings.TrimSpace(title)
}

func uuidNew() string { return uuid.NewString() }
