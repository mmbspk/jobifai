package retention

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/user/jobifai/internal/documents"
	"github.com/user/jobifai/internal/domain"
)

const (
	defaultBatchSize   = 25
	maxBatchSize       = 100
	maxReconcilePasses = 50
	tempMinAge         = 5 * time.Minute
)

// Metrics summarizes a cleanup pass without resume PII.
type Metrics struct {
	ApplicationsScanned  int `json:"applications_scanned"`
	PathsEligible        int `json:"paths_eligible"`
	PathsEvicted         int `json:"paths_evicted"`
	PathsProtected       int `json:"paths_protected"`
	DeleteFailures       int `json:"delete_failures"`
	TempFilesRemoved     int `json:"temp_files_removed"`
	ArtifactBlobsEvicted int `json:"artifact_blobs_evicted"`
}

type Service struct {
	DB       DB
	Config   ConfigStore
	Root     string
	Blobs    BlobRemover
	Activity *ActivityCoordinator
	userMu   sync.Map
}

func (s *Service) userLock(userID string) *sync.Mutex {
	v, _ := s.userMu.LoadOrStore(userID, &sync.Mutex{})
	return v.(*sync.Mutex)
}

func (s *Service) Limit() int {
	return LoadDefaults(s.Config).LatestSubmittedApplications
}

type EvictionCandidate struct {
	JobID           string `json:"job_id"`
	ResumePath      string `json:"resume_path,omitempty"`
	CoverPath       string `json:"cover_path,omitempty"`
	ResumeVersionID string `json:"-"`
	CoverVersionID  string `json:"-"`
	PackJSON        string `json:"-"`
	ResumeCleared   bool   `json:"-"`
	CoverCleared    bool   `json:"-"`
}

func (s *Service) PreviewEviction(ctx context.Context, userID string) ([]EvictionCandidate, Metrics, error) {
	limit := s.Limit()
	idx, err := buildProtectionIndex(ctx, s.DB, userID, limit)
	if err != nil {
		return nil, Metrics{}, err
	}
	meta, err := loadVersionMeta(ctx, s.DB, userID)
	if err != nil {
		return nil, Metrics{}, err
	}
	return s.collectCandidates(ctx, userID, limit, idx, meta)
}

func (s *Service) RunEviction(ctx context.Context, userID string, batchSize int) (Metrics, error) {
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}
	if batchSize > maxBatchSize {
		batchSize = maxBatchSize
	}
	var metrics Metrics
	run := func() error {
		mu := s.userLock(userID)
		mu.Lock()
		defer mu.Unlock()
		limit := s.Limit()
		idx, err := buildProtectionIndex(ctx, s.DB, userID, limit)
		if err != nil {
			return err
		}
		meta, err := loadVersionMeta(ctx, s.DB, userID)
		if err != nil {
			return err
		}
		candidates, m, err := s.collectCandidates(ctx, userID, limit, idx, meta)
		metrics = m
		if err != nil {
			return err
		}
		if len(candidates) > batchSize {
			candidates = candidates[:batchSize]
		}
		for _, c := range candidates {
			ev, fail, err := s.evictApplication(ctx, userID, c, meta)
			metrics.PathsEvicted += ev
			metrics.DeleteFailures += fail
			if err != nil {
				log.Warn().Err(err).Str("user_id", userID).Str("job_id", c.JobID).Msg("retention: eviction failed")
			}
		}
		idx, err = buildProtectionIndex(ctx, s.DB, userID, limit)
		if err != nil {
			return err
		}
		artRemoved, err := s.evictOrphanArtifactBlobs(ctx, userID, limit, idx, meta, batchSize)
		if err != nil {
			return err
		}
		metrics.ArtifactBlobsEvicted += artRemoved
		metrics.TempFilesRemoved = cleanTempFiles(s.Root, userID, tempMinAge)
		return nil
	}
	var err error
	if s.Activity != nil {
		err = s.Activity.WithEviction(ctx, userID, run)
	} else {
		err = run()
	}
	log.Info().
		Str("user_id", userID).
		Int("paths_evicted", metrics.PathsEvicted).
		Int("paths_protected", metrics.PathsProtected).
		Int("delete_failures", metrics.DeleteFailures).
		Int("artifact_blobs_evicted", metrics.ArtifactBlobsEvicted).
		Int("temp_removed", metrics.TempFilesRemoved).
		Msg("retention_cleanup")
	return metrics, err
}

// ReconcileUser drains eligible cleanup in bounded passes (backlog after limit changes).
func (s *Service) ReconcileUser(ctx context.Context, userID string) (Metrics, error) {
	var total Metrics
	for pass := 0; pass < maxReconcilePasses; pass++ {
		m, err := s.RunEviction(ctx, userID, maxBatchSize)
		total.ApplicationsScanned += m.ApplicationsScanned
		total.PathsEligible += m.PathsEligible
		total.PathsEvicted += m.PathsEvicted
		total.PathsProtected += m.PathsProtected
		total.DeleteFailures += m.DeleteFailures
		total.TempFilesRemoved += m.TempFilesRemoved
		total.ArtifactBlobsEvicted += m.ArtifactBlobsEvicted
		if err != nil {
			return total, err
		}
		if m.PathsEvicted == 0 && m.ArtifactBlobsEvicted == 0 && m.PathsEligible == 0 {
			break
		}
	}
	return total, nil
}

func (s *Service) collectCandidates(ctx context.Context, userID string, retainN int, idx ProtectionIndex, meta map[string]versionMeta) ([]EvictionCandidate, Metrics, error) {
	var metrics Metrics
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, COALESCE(resume_path,''), COALESCE(cover_letter_path,''),
		       COALESCE(resume_content_version_id,''), COALESCE(cover_letter_content_version_id,''),
		       COALESCE(document_refs_json,''), COALESCE(retention_paths_cleared,'')
		FROM jobs_applied WHERE user_id = ? ORDER BY applied_at DESC, id DESC`, userID)
	if err != nil {
		return nil, metrics, err
	}
	defer func() { _ = rows.Close() }()

	var all []EvictionCandidate
	idxRow := 0
	for rows.Next() {
		var c EvictionCandidate
		var cleared string
		if err := rows.Scan(&c.JobID, &c.ResumePath, &c.CoverPath, &c.ResumeVersionID, &c.CoverVersionID, &c.PackJSON, &cleared); err != nil {
			return nil, metrics, err
		}
		parseClearedFlags(&c, cleared)
		idxRow++
		if idxRow <= retainN {
			continue
		}
		all = append(all, c)
	}
	if err := rows.Err(); err != nil {
		return nil, metrics, err
	}

	var out []EvictionCandidate
	for _, c := range all {
		metrics.ApplicationsScanned++
		eligible := false
		rv := versionIDForExport(c.PackJSON, c.ResumeVersionID, c.ResumePath, "resume")
		cv := versionIDForExport(c.PackJSON, c.CoverVersionID, c.CoverPath, "cover_letter")
		if !c.ResumeCleared && c.ResumePath != "" {
			if exportEvictAllowed(c.ResumePath, rv, meta, idx) {
				metrics.PathsEligible++
				eligible = true
			} else {
				metrics.PathsProtected++
			}
		}
		if !c.CoverCleared && c.CoverPath != "" {
			if exportEvictAllowed(c.CoverPath, cv, meta, idx) {
				metrics.PathsEligible++
				eligible = true
			} else {
				metrics.PathsProtected++
			}
		}
		if eligible {
			out = append(out, c)
		}
	}
	return out, metrics, nil
}

func (s *Service) evictApplication(ctx context.Context, userID string, c EvictionCandidate, meta map[string]versionMeta) (evicted, failures int, err error) {
	idx, err := buildProtectionIndex(ctx, s.DB, userID, s.Limit())
	if err != nil {
		return 0, 0, err
	}
	resumeVID := versionIDForExport(c.PackJSON, c.ResumeVersionID, c.ResumePath, documents.KindResume)
	coverVID := versionIDForExport(c.PackJSON, c.CoverVersionID, c.CoverPath, documents.KindCoverLetter)

	state := c
	if !state.ResumeCleared && exportEvictAllowed(state.ResumePath, resumeVID, meta, idx) {
		e, f, st, perr := s.evictOneExport(ctx, userID, &state, "resume", state.ResumePath)
		evicted += e
		failures += f
		if perr != nil {
			return evicted, failures, perr
		}
		state = st
	}
	if !state.CoverCleared && exportEvictAllowed(state.CoverPath, coverVID, meta, idx) {
		e, f, st, perr := s.evictOneExport(ctx, userID, &state, "cover", state.CoverPath)
		evicted += e
		failures += f
		if perr != nil {
			return evicted, failures, perr
		}
		state = st
	}
	return evicted, failures, nil
}

func (s *Service) evictOneExport(ctx context.Context, userID string, c *EvictionCandidate, kind, path string) (evicted, failures int, state EvictionCandidate, err error) {
	state = *c
	if err := removeFile(s.Root, path); err != nil && !errors.Is(err, os.ErrNotExist) {
		failures++
		return evicted, failures, state, err
	}
	evicted++
	packJSON := clearExportPathInPack(state.PackJSON, kind, path)
	cleared := markCleared(clearedJSON(state), kind)
	var newResume, newCover = state.ResumePath, state.CoverPath
	switch kind {
	case "resume":
		newResume = ""
		state.ResumeCleared = true
	case "cover":
		newCover = ""
		state.CoverCleared = true
	}
	_, err = s.DB.ExecContext(ctx,
		`UPDATE jobs_applied SET resume_path = ?, cover_letter_path = ?, document_refs_json = ?, retention_paths_cleared = ?
		 WHERE id = ? AND user_id = ?`,
		newResume, newCover, packJSON, cleared, state.JobID, userID)
	if err != nil {
		failures++
		return evicted, failures, state, err
	}
	state.PackJSON = packJSON
	state.ResumePath = newResume
	state.CoverPath = newCover
	return evicted, failures, state, nil
}

func removeFile(root, path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(path, "/")))
	}
	return os.Remove(abs)
}

func cleanTempFiles(root, userID string, minAge time.Duration) int {
	base := filepath.Join(root, "job_applications", sanitize(userID))
	removed := 0
	cutoff := time.Now().Add(-minAge)
	_ = filepath.WalkDir(base, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		name := d.Name()
		isTmp := strings.HasSuffix(name, ".tmp") || (strings.HasPrefix(name, ".") && strings.Contains(name, ".tmp"))
		if !isTmp {
			return nil
		}
		info, statErr := d.Info()
		if statErr != nil || info.ModTime().After(cutoff) {
			return nil
		}
		if rmErr := os.Remove(p); rmErr == nil {
			removed++
		}
		return nil
	})
	return removed
}

func sanitize(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' {
			return '_'
		}
		return r
	}, s)
	if s == "" {
		return "user"
	}
	return s
}

func (s *Service) AfterSuccessfulSubmit(ctx context.Context, userID string) {
	if s == nil || s.DB == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		_, _ = s.ReconcileUser(ctx, userID)
	}()
}

var _ = domain.DocumentRetentionDefault

// parseClearedFlags reads retention_paths_cleared JSON {"resume":true,"cover":true}.
func parseClearedFlags(c *EvictionCandidate, raw string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return
	}
	if strings.Contains(raw, `"resume"`) {
		c.ResumeCleared = strings.Contains(raw, `"resume":true`)
	}
	if strings.Contains(raw, `"cover"`) {
		c.CoverCleared = strings.Contains(raw, `"cover":true`)
	}
}

func clearedJSON(c EvictionCandidate) string {
	return clearedJSONFromFlags(c.ResumeCleared, c.CoverCleared)
}

func clearedJSONFromFlags(resume, cover bool) string {
	if !resume && !cover {
		return ""
	}
	r, co := "false", "false"
	if resume {
		r = "true"
	}
	if cover {
		co = "true"
	}
	return `{"resume":` + r + `,"cover":` + co + `}`
}

func markCleared(raw, kind string) string {
	c := EvictionCandidate{}
	parseClearedFlags(&c, raw)
	switch kind {
	case "resume":
		c.ResumeCleared = true
	case "cover":
		c.CoverCleared = true
	}
	return clearedJSON(c)
}

// Ensure sql import for future tx helpers.
var _ = sql.ErrNoRows
