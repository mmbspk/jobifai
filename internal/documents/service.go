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

type Service struct {
	Store         *Store
	Blobs         BlobStore
	Renderer      PDFRenderer
	MarketDir     string
	LoadProfile   ProfileLoader
	DefaultsMeta  func(userID string) (DefaultsMeta, error)
	SaveDefaultsMeta func(userID string, m DefaultsMeta) error
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
		def = MergeDefaultsMeta(def, meta)
	}
	return ListResponse{Documents: docs, Defaults: def}, nil
}

func (s *Service) CreateResumeFromProfile(ctx context.Context, userID, title string, rc RenderContext) (versionID string, err error) {
	profile, err := s.requireProfile(userID)
	if err != nil {
		return "", err
	}
	return s.saveResumeVersion(ctx, userID, title, SourceProfileRender, profile, rc)
}

func (s *Service) SaveResumeVersion(ctx context.Context, userID, title, source string, profile *domain.ResumeProfile, rc RenderContext) (versionID string, err error) {
	return s.saveResumeVersion(ctx, userID, title, source, profile, rc)
}

func (s *Service) saveResumeVersion(ctx context.Context, userID, title, source string, profile *domain.ResumeProfile, rc RenderContext) (versionID string, err error) {
	cssPath, cssSnap, err := s.resolveCSS(userID, rc.Market, rc.StyleName)
	if err != nil {
		return "", err
	}
	content, err := json.Marshal(ResumeContent{Profile: *profile})
	if err != nil {
		return "", err
	}
	docID, err := s.Store.CreateDocument(ctx, userID, KindResume, defaultTitle(title, "Default resume"))
	if err != nil {
		return "", err
	}
	snapJSON, _ := json.Marshal(profile)
	versionID, _, err = s.Store.InsertVersion(ctx, InsertVersionParams{
		DocumentID: docID, UserID: userID, Source: source,
		ContentKind: ContentResumeJSON, ContentJSON: string(content),
		ProfileSnapshotJSON: string(snapJSON), ProfileSnapshotHash: ProfileSnapshotHash(profile),
		Market: rc.Market, DocumentLanguage: rc.Language, StyleName: rc.StyleName,
		CSSFilePath: cssPath, CSSSnapshot: cssSnap, RendererVersion: RendererVersion,
		Reconstructible: cssSnap != "",
	})
	if err != nil {
		return "", err
	}
	if _, err := s.renderAndPersistPDF(ctx, userID, versionID, profile, nil, rc, cssPath, cssSnap); err != nil {
		return versionID, err
	}
	return versionID, nil
}

func (s *Service) SaveCoverLetter(ctx context.Context, userID, title, body string, rc RenderContext) (versionID string, err error) {
	return s.SaveCoverLetterWithSource(ctx, userID, title, body, SourceUserEdit, rc)
}

func (s *Service) SaveCoverLetterWithSource(ctx context.Context, userID, title, body, source string, rc RenderContext) (versionID string, err error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return "", fmt.Errorf("cover letter body is required")
	}
	if source == "" {
		source = SourceUserEdit
	}
	profile, _ := s.LoadProfile(userID)
	cssPath, cssSnap, err := s.resolveCSS(userID, rc.Market, rc.StyleName)
	if err != nil {
		return "", err
	}
	content, _ := json.Marshal(CoverContent{Body: body})
	docID, err := s.Store.CreateDocument(ctx, userID, KindCoverLetter, defaultTitle(title, "General cover letter"))
	if err != nil {
		return "", err
	}
	var snapJSON, hash string
	if profile != nil {
		b, _ := json.Marshal(profile)
		snapJSON = string(b)
		hash = ProfileSnapshotHash(profile)
	}
	versionID, _, err = s.Store.InsertVersion(ctx, InsertVersionParams{
		DocumentID: docID, UserID: userID, Source: source,
		ContentKind: ContentCoverText, ContentJSON: string(content),
		ProfileSnapshotJSON: snapJSON, ProfileSnapshotHash: hash,
		Market: rc.Market, DocumentLanguage: rc.Language, StyleName: rc.StyleName,
		CSSFilePath: cssPath, CSSSnapshot: cssSnap, RendererVersion: RendererVersion,
		Reconstructible: cssSnap != "",
	})
	if err != nil {
		return "", err
	}
	if _, err := s.renderCoverPDF(ctx, userID, versionID, body, rc, cssPath, cssSnap); err != nil {
		return versionID, err
	}
	return versionID, nil
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
	_, err = s.Store.InsertOriginalFile(ctx, userID, docID, versionID, filename, mediaType, key, sha, size)
	return versionID, err
}

func (s *Service) SetDefault(ctx context.Context, userID, kind, versionID string) error {
	profile, _ := s.LoadProfile(userID)
	hash := ProfileSnapshotHash(profile)
	market := ""
	_, p, err := s.Store.GetVersionRow(ctx, userID, versionID)
	if err != nil {
		return err
	}
	market = p.Market
	if err := s.Store.SetDefault(ctx, userID, kind, versionID, hash, market); err != nil {
		return err
	}
	if s.SaveDefaultsMeta != nil {
		meta, _ := s.DefaultsMeta(userID)
		if kind == KindResume {
			meta.ResumeOutdated = false
		} else if kind == KindCoverLetter {
			meta.CoverOutdated = false
		}
		if !meta.ResumeOutdated && !meta.CoverOutdated {
			meta.OutdatedReason = ""
		}
		meta.ProfileHash = hash
		meta.Market = market
		_ = s.SaveDefaultsMeta(userID, meta)
	}
	return nil
}

func (s *Service) OriginalBytes(ctx context.Context, userID, versionID string) ([]byte, string, string, error) {
	if _, p, err := s.Store.GetVersionRow(ctx, userID, versionID); err != nil {
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
	key, _, err := s.Store.ArtifactForVersion(ctx, userID, versionID)
	if err == nil {
		data, err := s.Blobs.Read(key)
		return data, "application/pdf", err
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, "", err
	}
	return s.ReconstructPDF(ctx, userID, versionID)
}

func (s *Service) ReconstructPDF(ctx context.Context, userID, versionID string) ([]byte, string, error) {
	_, p, err := s.Store.GetVersionRow(ctx, userID, versionID)
	if err != nil {
		return nil, "", err
	}
	if !p.Reconstructible {
		return nil, "", fmt.Errorf("this document version cannot be reconstructed (legacy or missing rendering snapshot)")
	}
	if p.CSSSnapshot == "" {
		return nil, "", fmt.Errorf("missing CSS snapshot for reconstruction")
	}
	switch p.ContentKind {
	case ContentResumeJSON:
		var rc ResumeContent
		if err := json.Unmarshal([]byte(p.ContentJSON), &rc); err != nil {
			return nil, "", err
		}
		opts := marketRenderOpts(p.Market, s.MarketDir)
		pdf, err := s.renderResumeWithSnapshot(ctx, &rc.Profile, p.StyleName, p.CSSSnapshot, opts)
		if err != nil {
			return nil, "", err
		}
		if err := s.persistArtifact(ctx, userID, versionID, pdf, p.CSSSnapshot); err != nil {
			return pdf, "application/pdf", nil
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
		if err := s.persistArtifact(ctx, userID, versionID, pdf, p.CSSSnapshot); err != nil {
			return pdf, "application/pdf", nil
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

func (s *Service) resolveCSS(userID, market, style string) (cssPath, cssSnapshot string, err error) {
	_ = userID
	if market != "" && s.MarketDir != "" {
		m := resume.LoadMarketByName(s.MarketDir, market)
		if m != nil && m.CSSFile != "" {
			data, readErr := os.ReadFile(m.CSSFile)
			if readErr == nil {
				snap := resume.ExpandStylesheetImports(string(data), filepath.Dir(m.CSSFile))
				return m.CSSFile, snap, nil
			}
		}
	}
	return "", resume.DefaultPDFCSS(), nil
}

func (s *Service) renderAndPersistPDF(ctx context.Context, userID, versionID string, profile *domain.ResumeProfile, _ *domain.ResumeProfile, rc RenderContext, cssPath, cssSnap string) ([]byte, error) {
	opts := marketRenderOpts(rc.Market, s.MarketDir)
	cssOverride := cssPath
	if cssSnap != "" {
		cssOverride = writeTempCSS(cssSnap)
		defer os.Remove(cssOverride)
	}
	pdf, err := s.Renderer.RenderResume(ctx, profile, rc.StyleName, cssOverride, opts)
	if err != nil {
		return nil, err
	}
	return pdf, s.persistArtifact(ctx, userID, versionID, pdf, cssSnap)
}

func (s *Service) renderCoverPDF(ctx context.Context, userID, versionID, body string, rc RenderContext, cssPath, cssSnap string) ([]byte, error) {
	cssOverride := cssPath
	if cssSnap != "" {
		cssOverride = writeTempCSS(cssSnap)
		defer os.Remove(cssOverride)
	}
	pdf, err := s.Renderer.RenderCoverLetter(ctx, body, rc.StyleName, cssOverride)
	if err != nil {
		return nil, err
	}
	return pdf, s.persistArtifact(ctx, userID, versionID, pdf, cssSnap)
}

func (s *Service) renderResumeWithSnapshot(ctx context.Context, profile *domain.ResumeProfile, style, cssSnap string, opts *resume.RenderOptions) ([]byte, error) {
	tmp := writeTempCSS(cssSnap)
	defer os.Remove(tmp)
	return s.Renderer.RenderResume(ctx, profile, style, tmp, opts)
}

func (s *Service) renderCoverWithSnapshot(ctx context.Context, body, style, cssSnap string) ([]byte, error) {
	tmp := writeTempCSS(cssSnap)
	defer os.Remove(tmp)
	return s.Renderer.RenderCoverLetter(ctx, body, style, tmp)
}

func writeTempCSS(css string) string {
	f, err := os.CreateTemp("", "doc-css-*.css")
	if err != nil {
		return ""
	}
	_, _ = f.WriteString(css)
	_ = f.Close()
	return f.Name()
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

func marketRenderOpts(market, marketDir string) *resume.RenderOptions {
	if market == "" || marketDir == "" {
		return nil
	}
	m := resume.LoadMarketByName(marketDir, market)
	if m == nil {
		return nil
	}
	labels := m.LabelsOrDefault()
	return &resume.RenderOptions{SectionLabels: labels}
}

func defaultTitle(title, fallback string) string {
	if strings.TrimSpace(title) == "" {
		return fallback
	}
	return strings.TrimSpace(title)
}

func uuidNew() string { return uuid.NewString() }
