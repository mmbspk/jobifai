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
	"sync"

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

// RenderBiller meters a new PDF render against the user's shared credit balance.
// Implemented by resource.Service — kept minimal to simplify testing.
type RenderBiller interface {
	// BeforeRender preflight-checks one document_render unit.
	// Returns nil when metering is disabled. Returns quota.ErrExceeded on exhaustion.
	BeforeRender(ctx context.Context, userID string) error
	// RecordRender atomically commits one document_render billing event.
	// idempotencyKey must be "document-render:<versionID_or_operationID>".
	// Duplicate keys are silently ignored.
	RecordRender(ctx context.Context, userID, idempotencyKey string) error
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
	// Biller meters new PDF renders. Nil disables billing (dev/test with no resource service).
	Biller RenderBiller
	// Metrics accumulates reuse/reconstruction counters.  Nil disables tracking.
	Metrics *ServiceMetrics
	// e2eFailNextRenderUsers is a pointer so it is shared across copies made during
	// request handling (resume_documents.go copies the Service by value). Nil in production.
	e2eFailNextRenderUsers *sync.Map
}

// E2ESetFailNextRender marks the next render for this user as a deterministic failure.
// Only called from E2E fixture handlers — never from any production path.
func (s *Service) E2ESetFailNextRender(userID string) {
	if s.e2eFailNextRenderUsers == nil {
		s.e2eFailNextRenderUsers = &sync.Map{}
	}
	s.e2eFailNextRenderUsers.Store(userID, true)
}

type RenderContext struct {
	Market    string
	StyleName string
	Language  string
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
	// Drift flags describe existing defaults; deleting a default leaves no
	// document to review for that kind, even if older metadata still has a flag.
	if def.ResumeVersionID == "" {
		def.ResumeOutdated = false
	}
	if def.CoverLetterVersionID == "" {
		def.CoverOutdated = false
	}
	if !def.ResumeOutdated && !def.CoverOutdated {
		def.OutdatedReason = ""
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
	return versionID, s.withWork(ctx, userID, func() error {
		versionID, err = s.saveResumeVersionLocked(ctx, userID, documentID, title, source, profile, rc)
		return err
	})
}

func (s *Service) saveResumeVersionLocked(ctx context.Context, userID, documentID, title, source string, profile *domain.ResumeProfile, rc RenderContext) (string, error) {
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
	canReconstruct := cssSnap != "" && renderJSON != ""

	// 1. Preflight quota BEFORE touching the DB so a quota failure leaves nothing to clean up.
	if s.Biller != nil {
		if err := s.Biller.BeforeRender(ctx, userID); err != nil {
			return "", err
		}
	}

	docID, err := s.ensureDocument(ctx, userID, documentID, KindResume, defaultTitle(title, "Default resume"))
	if err != nil {
		return "", err
	}

	// 2. Insert version with Reconstructible=false. The version becomes reconstructible
	//    only after render + artifact persistence + billing all succeed.
	versionID, _, err := s.Store.InsertVersion(ctx, InsertVersionParams{
		DocumentID: docID, UserID: userID, Source: source,
		ContentKind: ContentResumeJSON, ContentJSON: string(content),
		ProfileSnapshotJSON: string(snapJSON), ProfileSnapshotHash: ProfileSnapshotHash(profile),
		Market: rc.Market, DocumentLanguage: rc.Language, StyleName: rc.StyleName,
		CSSFilePath: cssPath, CSSSnapshot: cssSnap, RendererVersion: RendererVersion,
		RenderSnapshotJSON: renderJSON, Reconstructible: false,
	})
	if err != nil {
		return "", err
	}

	// 3. Render → blob → bill → link.
	// On render/blob failure (needsCleanup=false): content is valid; preserve the version
	// so the caller can report partial success (render_status: "failed").
	// On blob-written failure (needsCleanup=true): blob orphaned or billing committed;
	// delete the version to avoid an inaccessible or billed-but-unlinked artifact.
	needsCleanup, err := s.resumeBillAndLink(ctx, userID, versionID, canReconstruct, profile, rc.StyleName, cssSnap, RenderOptsFromSnapshot(renderSnap))
	if err != nil {
		if needsCleanup {
			_ = s.Store.DeleteVersion(ctx, userID, versionID)
			return "", err
		}
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
	return versionID, s.withWork(ctx, userID, func() error {
		versionID, err = s.saveCoverLetterLocked(ctx, userID, documentID, title, body, source, rc)
		return err
	})
}

func (s *Service) saveCoverLetterLocked(ctx context.Context, userID, documentID, title, body, source string, rc RenderContext) (string, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return "", fmt.Errorf("cover letter body is required")
	}
	if source == "" {
		source = SourceUserEdit
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
	canReconstruct := cssSnap != "" && renderJSON != ""

	// 1. Preflight quota BEFORE touching the DB so a quota failure leaves nothing to clean up.
	if s.Biller != nil {
		if err := s.Biller.BeforeRender(ctx, userID); err != nil {
			return "", err
		}
	}

	docID, err := s.ensureDocument(ctx, userID, documentID, KindCoverLetter, defaultTitle(title, "General cover letter"))
	if err != nil {
		return "", err
	}

	// 2. Insert version with Reconstructible=false.
	versionID, _, err := s.Store.InsertVersion(ctx, InsertVersionParams{
		DocumentID: docID, UserID: userID, Source: source,
		ContentKind: ContentCoverText, ContentJSON: string(content),
		ProfileSnapshotJSON: snapJSON, ProfileSnapshotHash: hash,
		Market: rc.Market, DocumentLanguage: rc.Language, StyleName: rc.StyleName,
		CSSFilePath: cssPath, CSSSnapshot: cssSnap, RendererVersion: RendererVersion,
		RenderSnapshotJSON: renderJSON, Reconstructible: false,
	})
	if err != nil {
		return "", err
	}

	// 3. Render → blob → bill → link (same partial-success semantics as saveResumeVersionLocked).
	needsCleanup, err := s.coverBillAndLink(ctx, userID, versionID, canReconstruct, body, rc.StyleName, cssSnap)
	if err != nil {
		if needsCleanup {
			_ = s.Store.DeleteVersion(ctx, userID, versionID)
			return "", err
		}
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
	return s.withWork(ctx, userID, func() error {
		return s.setDefaultLocked(ctx, userID, kind, versionID)
	})
}

func (s *Service) setDefaultLocked(ctx context.Context, userID, kind, versionID string) error {
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

// withWork acquires the activity guard (if wired) so document writes block
// eviction and are blocked while eviction runs.
func (s *Service) withWork(ctx context.Context, userID string, fn func() error) error {
	if s.WorkGuard != nil {
		if err := s.WorkGuard.BeginWork(ctx, userID); err != nil {
			return err
		}
		defer s.WorkGuard.EndWork(userID)
	}
	return fn()
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

// PDFBytes returns the PDF for versionID.  The third return value is true when
// the bytes were produced by on-demand reconstruction (artifact evicted or
// never persisted) so callers can set the X-Jobifai-Reconstructed header.
func (s *Service) PDFBytes(ctx context.Context, userID, versionID string) (data []byte, contentType string, reconstructed bool, err error) {
	if s.WorkGuard != nil {
		if err := s.WorkGuard.BeginWork(ctx, userID); err != nil {
			return nil, "", false, err
		}
		defer s.WorkGuard.EndWork(userID)
	}
	key, _, keyErr := s.Store.ArtifactForVersion(ctx, userID, versionID)
	if keyErr == nil {
		d, readErr := s.Blobs.Read(key)
		if readErr == nil {
			if s.Metrics != nil {
				s.Metrics.ArtifactServeHits.Add(1)
				s.Metrics.ArtifactServeBytesTotal.Add(int64(len(d)))
			}
			return d, "application/pdf", false, nil
		}
		if errors.Is(readErr, ErrBlobNotFound) {
			d, ct, err := s.ReconstructPDF(ctx, userID, versionID)
			if err == nil && s.Metrics != nil {
				s.Metrics.Reconstructions.Add(1)
				s.Metrics.ReconstructionBytesTotal.Add(int64(len(d)))
			}
			return d, ct, true, err
		}
		return nil, "", false, readErr
	}
	if !errors.Is(keyErr, ErrNotFound) {
		return nil, "", false, keyErr
	}
	d, ct, err := s.ReconstructPDF(ctx, userID, versionID)
	if err == nil && s.Metrics != nil {
		s.Metrics.Reconstructions.Add(1)
		s.Metrics.ReconstructionBytesTotal.Add(int64(len(d)))
	}
	return d, ct, true, err
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

// resumeBillAndLink renders a resume PDF, writes the blob, commits billing, and links the
// artifact in DB. It returns (needsCleanup, err):
//
//   - needsCleanup=false, err!=nil: render or blob failure — nothing was committed externally.
//     The pending version content is valid; the caller should preserve it and surface partial
//     success (render_status: "failed") so the client gets a versionID even without a PDF.
//
//   - needsCleanup=true, err!=nil: the blob was written (billing may or may not have committed)
//     but a subsequent step failed. The caller must delete the pending version to avoid an
//     inaccessible or billed-but-unlinked artifact in the DB.
//
//   - needsCleanup=true, err=nil: success (caller does not delete).
//
// Order is critical for billing integrity:
//
//  1. render  → failure: needsCleanup=false; no external state
//  2. blob    → failure: needsCleanup=false; blob write is atomic (PutAtomic)
//  3. billing → failure: needsCleanup=true;  orphaned blob; version cleaned up by caller
//  4. link    → failure: needsCleanup=true;  billing committed; caller cleans up version
//  5. ready   → failure: artifact accessible via direct key; logged but not fatal
func (s *Service) resumeBillAndLink(ctx context.Context, userID, versionID string, canReconstruct bool, profile *domain.ResumeProfile, styleName, cssSnap string, opts *resume.RenderOptions) (needsCleanup bool, err error) {
	cssOverride, cleanup, err := writeTempCSS(cssSnap)
	if err != nil {
		return false, err
	}
	defer cleanup()

	if s.e2eFailNextRenderUsers != nil {
		if _, shouldFail := s.e2eFailNextRenderUsers.LoadAndDelete(userID); shouldFail {
			return false, errors.New("e2e injected render failure")
		}
	}

	pdf, err := s.Renderer.RenderResume(ctx, profile, styleName, cssOverride, opts)
	if err != nil {
		return false, err
	}

	key, sha, size, err := s.writeArtifactBlob(userID, pdf)
	if err != nil {
		return false, err
	}

	// Blob written — any failure from here requires the caller to clean up the version
	// (the blob is orphaned and/or billing was committed).
	if s.Biller != nil {
		if err := s.Biller.RecordRender(ctx, userID, renderIdempotencyKey(versionID)); err != nil {
			return true, fmt.Errorf("document render billing: %w", err)
		}
	}

	tid := templateIdentity(cssSnap)
	if err := s.Store.LinkArtifact(ctx, userID, versionID, key, sha, size, tid); err != nil {
		return true, err
	}

	// Mark version reconstructible so legitimate retention-eviction followed by
	// ReconstructPDF works for free. Non-fatal: artifact is already accessible via direct key.
	if canReconstruct {
		if err := s.Store.MarkVersionReady(ctx, userID, versionID); err != nil {
			// Artifact and billing are both committed; the version is accessible.
			// Log but do not fail — a MarkVersionReady failure means reconstruction after
			// eviction won't work, but the user can still download the current artifact.
			_ = err // caller receives success; background GC / admin can rectify
		}
	}
	return true, nil
}

// coverBillAndLink is the cover-letter equivalent of resumeBillAndLink.
// Returns (needsCleanup, err) with the same semantics:
// needsCleanup=false → render/blob failure, caller preserves version;
// needsCleanup=true  → blob written (billing failure or link failure), caller must delete.
func (s *Service) coverBillAndLink(ctx context.Context, userID, versionID string, canReconstruct bool, body, styleName, cssSnap string) (needsCleanup bool, err error) {
	cssOverride, cleanup, err := writeTempCSS(cssSnap)
	if err != nil {
		return false, err
	}
	defer cleanup()

	pdf, err := s.Renderer.RenderCoverLetter(ctx, body, styleName, cssOverride)
	if err != nil {
		return false, err
	}

	key, sha, size, err := s.writeArtifactBlob(userID, pdf)
	if err != nil {
		return false, err
	}

	if s.Biller != nil {
		if err := s.Biller.RecordRender(ctx, userID, renderIdempotencyKey(versionID)); err != nil {
			return true, fmt.Errorf("document render billing: %w", err)
		}
	}

	tid := templateIdentity(cssSnap)
	if err := s.Store.LinkArtifact(ctx, userID, versionID, key, sha, size, tid); err != nil {
		return true, err
	}

	if canReconstruct {
		if err := s.Store.MarkVersionReady(ctx, userID, versionID); err != nil {
			_ = err
		}
	}
	return true, nil
}

func renderIdempotencyKey(versionID string) string {
	return "document-render:" + versionID
}

// writeArtifactBlob stores the PDF bytes in content-addressed blob storage and returns
// the storage key, hex SHA-256, and byte size. The returned key is not yet linked in DB;
// callers must call Store.LinkArtifact after committing billing.
func (s *Service) writeArtifactBlob(userID string, pdf []byte) (key, sha string, size int64, err error) {
	sum := sha256.Sum256(pdf)
	shaHex := hex.EncodeToString(sum[:])
	k := filepath.ToSlash(filepath.Join(userID, "pdf", shaHex[:2], shaHex+".pdf"))
	if _, _, err := s.Blobs.PutAtomic(k, bytes.NewReader(pdf)); err != nil {
		return "", "", 0, err
	}
	return k, shaHex, int64(len(pdf)), nil
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
