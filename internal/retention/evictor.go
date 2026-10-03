package retention

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/user/jobifai/internal/domain"
)

const (
	defaultBatchSize = 25
	maxBatchSize     = 100
)

// Metrics summarizes a cleanup pass without resume PII.
type Metrics struct {
	ApplicationsScanned int `json:"applications_scanned"`
	PathsEligible       int `json:"paths_eligible"`
	PathsEvicted        int `json:"paths_evicted"`
	PathsProtected      int `json:"paths_protected"`
	DeleteFailures      int `json:"delete_failures"`
	TempFilesRemoved    int `json:"temp_files_removed"`
}

type Service struct {
	DB      DB
	Config  ConfigStore
	Root    string
	userMu  sync.Map // userID -> *sync.Mutex
}

func (s *Service) userLock(userID string) *sync.Mutex {
	v, _ := s.userMu.LoadOrStore(userID, &sync.Mutex{})
	return v.(*sync.Mutex)
}

func (s *Service) Limit() int {
	return LoadDefaults(s.Config).LatestSubmittedApplications
}

type EvictionCandidate struct {
	JobID      string `json:"job_id"`
	ResumePath string `json:"resume_path,omitempty"`
	CoverPath  string `json:"cover_path,omitempty"`
}

// PreviewEviction lists paths that would be cleared for userID without deleting.
func (s *Service) PreviewEviction(ctx context.Context, userID string) ([]EvictionCandidate, Metrics, error) {
	limit := s.Limit()
	protected, err := protectedPaths(ctx, s.DB, userID, limit)
	if err != nil {
		return nil, Metrics{}, err
	}
	candidates, metrics, err := s.collectCandidates(ctx, userID, limit, protected, false)
	return candidates, metrics, err
}

// RunEviction deletes eligible PDFs in bounded batches with per-user locking.
func (s *Service) RunEviction(ctx context.Context, userID string, batchSize int) (Metrics, error) {
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}
	if batchSize > maxBatchSize {
		batchSize = maxBatchSize
	}
	mu := s.userLock(userID)
	mu.Lock()
	defer mu.Unlock()

	limit := s.Limit()
	protected, err := protectedPaths(ctx, s.DB, userID, limit)
	if err != nil {
		return Metrics{}, err
	}
	candidates, metrics, err := s.collectCandidates(ctx, userID, limit, protected, true)
	if err != nil {
		return metrics, err
	}
	if len(candidates) > batchSize {
		candidates = candidates[:batchSize]
	}
	for _, c := range candidates {
		evicted, err := s.evictApplicationPaths(ctx, userID, c, protected)
		if err != nil {
			metrics.DeleteFailures++
			log.Warn().Err(err).Str("user_id", userID).Str("job_id", c.JobID).Msg("retention: eviction failed")
			continue
		}
		metrics.PathsEvicted += evicted
	}
	metrics.TempFilesRemoved = cleanTempFiles(s.Root, userID)
	log.Info().
		Str("user_id", userID).
		Int("paths_evicted", metrics.PathsEvicted).
		Int("paths_protected", metrics.PathsProtected).
		Int("delete_failures", metrics.DeleteFailures).
		Int("temp_removed", metrics.TempFilesRemoved).
		Msg("retention_cleanup")
	return metrics, nil
}

func (s *Service) collectCandidates(ctx context.Context, userID string, retainN int, protected map[string]struct{}, countOnly bool) ([]EvictionCandidate, Metrics, error) {
	var metrics Metrics
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, COALESCE(resume_path,''), COALESCE(cover_letter_path,'')
		FROM jobs_applied WHERE user_id = ? ORDER BY applied_at DESC`, userID)
	if err != nil {
		return nil, metrics, err
	}
	defer func() { _ = rows.Close() }()

	var all []EvictionCandidate
	idx := 0
	for rows.Next() {
		var c EvictionCandidate
		if err := rows.Scan(&c.JobID, &c.ResumePath, &c.CoverPath); err != nil {
			return nil, metrics, err
		}
		idx++
		if idx <= retainN {
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
		if c.ResumePath != "" && !pathProtected(protected, c.ResumePath) {
			metrics.PathsEligible++
			eligible = true
		} else if c.ResumePath != "" {
			metrics.PathsProtected++
		}
		if c.CoverPath != "" && !pathProtected(protected, c.CoverPath) {
			metrics.PathsEligible++
			eligible = true
		} else if c.CoverPath != "" {
			metrics.PathsProtected++
		}
		if eligible {
			out = append(out, c)
		}
	}
	_ = countOnly
	return out, metrics, nil
}

func (s *Service) evictApplicationPaths(ctx context.Context, userID string, c EvictionCandidate, protected map[string]struct{}) (int, error) {
	newResume, newCover := c.ResumePath, c.CoverPath
	evicted := 0
	if c.ResumePath != "" && !pathProtected(protected, c.ResumePath) {
		if err := removeFile(s.Root, c.ResumePath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return evicted, err
		}
		newResume = ""
		evicted++
	}
	if c.CoverPath != "" && !pathProtected(protected, c.CoverPath) {
		if err := removeFile(s.Root, c.CoverPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return evicted, err
		}
		newCover = ""
		evicted++
	}
	_, err := s.DB.ExecContext(ctx,
		`UPDATE jobs_applied SET resume_path = ?, cover_letter_path = ? WHERE id = ? AND user_id = ?`,
		newResume, newCover, c.JobID, userID)
	return evicted, err
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

func cleanTempFiles(root, userID string) int {
	base := filepath.Join(root, "job_applications", sanitize(userID))
	removed := 0
	_ = filepath.WalkDir(base, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.HasSuffix(d.Name(), ".tmp") || strings.HasPrefix(d.Name(), ".") && strings.Contains(d.Name(), ".tmp") {
			if rmErr := os.Remove(p); rmErr == nil {
				removed++
			}
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

// AfterSuccessfulSubmit triggers bounded cleanup for a user (non-blocking caller).
func (s *Service) AfterSuccessfulSubmit(ctx context.Context, userID string) {
	if s == nil || s.DB == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		_, _ = s.RunEviction(ctx, userID, defaultBatchSize)
	}()
}

// Ensure domain import for lint in tests referencing limits.
var _ = domain.DocumentRetentionDefault
