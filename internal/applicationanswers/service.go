// Package applicationanswers manages candidate facts and their later review.
// Review records cannot grant browser execution or mapping trust on their own.
package applicationanswers

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"strings"
	"time"

	"github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
)

var ErrConflict = domain.ErrApplicationAnswerConflict

type ConfigStore interface {
	GetTx(context.Context, *sql.Tx, string, string, any) error
	SetTx(context.Context, *sql.Tx, string, string, any) error
}

type Service struct {
	DB             *sql.DB
	Config         ConfigStore
	ConfirmMapping func(context.Context, *sql.Tx, string, string, string, *domain.ResumeProfile) error
}

func NewService(database *sql.DB, config ConfigStore) *Service {
	return &Service{DB: database, Config: config}
}

var Catalog = []domain.ApplicationAnswerTopic{
	{ID: "work_rights", Label: "Right to work", Core: true},
	{ID: "sponsorship", Label: "Sponsorship", Core: true},
	{ID: "notice_period", Label: "Notice period", Core: true},
	{ID: "earliest_start_date", Label: "Earliest start", Core: true},
	{ID: "salary", Label: "Expected pay", Core: true},
	{ID: "relocation", Label: "Relocation", Core: false},
	{ID: "travel", Label: "Travel", Core: false},
	{ID: "work_arrangement", Label: "Work arrangement and commute", Core: false},
	{ID: "working_hours", Label: "Working hours", Core: false},
	{ID: "credentials", Label: "Relevant credentials", Core: false},
}

func digest(v any) string { b, _ := json.Marshal(v); return fmt.Sprintf("%x", sha256.Sum256(b)) }

// ProfileRevision covers factual profile JSON only; source metadata is external.
func ProfileRevision(p *domain.ResumeProfile) string { return digest(p) }

func AnswersRevision(p *domain.ResumeProfile, prefs domain.WorkPreferences) string {
	return digest(struct {
		Defaults               domain.ApplicationDefaults
		Credentials            []domain.Certification
		Remote, Hybrid, Onsite bool
	}{p.ApplicationDefaults, p.Certifications, prefs.Remote, prefs.Hybrid, prefs.Onsite})
}

func (s *Service) transaction(ctx context.Context, userID string, fn func(*sql.Tx) error) error {
	if userID == "" {
		return domain.ErrNotFound
	}
	if s == nil || s.DB == nil || s.Config == nil {
		return errors.New("application answers unavailable")
	}
	for retry := 0; retry < 5; retry++ {
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		// Acquire the writer before reading facts, preventing WAL read/write races.
		_, err = tx.ExecContext(ctx, `INSERT INTO application_answer_notifications(user_id) VALUES(?) ON CONFLICT(user_id) DO NOTHING`, userID)
		if err == nil {
			err = fn(tx)
		}
		if err == nil {
			err = tx.Commit()
		} else {
			_ = tx.Rollback()
		}
		_ = tx.Rollback()
		if !db.IsSQLiteBusy(err) || retry == 4 {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(20*(1<<retry)) * time.Millisecond):
		}
	}
	return errors.New("application answers transaction unavailable")
}

func (s *Service) readFacts(ctx context.Context, tx *sql.Tx, userID string) (domain.ResumeProfile, domain.WorkPreferences, error) {
	var p domain.ResumeProfile
	var prefs domain.WorkPreferences
	if err := s.Config.GetTx(ctx, tx, userID, "resume_profile", &p); err != nil && !errors.Is(err, domain.ErrNotFound) {
		return p, prefs, err
	}
	if err := s.Config.GetTx(ctx, tx, userID, "work_preferences", &prefs); err != nil && !errors.Is(err, domain.ErrNotFound) {
		return p, prefs, err
	}
	return p, prefs, nil
}

func topicValue(p *domain.ResumeProfile, prefs domain.WorkPreferences, id string) any {
	a := p.ApplicationDefaults
	switch id {
	case "work_rights":
		return a.WorkRights
	case "sponsorship":
		return struct{ Current, Future *bool }{a.RequiresSponsorship, a.FutureRequiresSponsorship}
	case "notice_period":
		return struct{ Notice, Availability string }{a.NoticePeriod, a.Availability}
	case "earliest_start_date":
		return a.EarliestStartDate
	case "salary":
		return struct {
			Legacy string
			Range  *domain.ApplicationSalaryRange
		}{a.SalaryExpectation, a.SalaryRange}
	case "relocation":
		return a.Relocation
	case "travel":
		return a.TravelConditions
	case "working_hours":
		return a.WorkingHours
	case "work_arrangement":
		return struct {
			Conditions             string
			Remote, Hybrid, Onsite bool
		}{a.CommuteConditions, prefs.Remote, prefs.Hybrid, prefs.Onsite}
	case "credentials":
		return p.Certifications
	case "additional_answers":
		return struct {
			Custom  []domain.ApplicationAnswer
			Details string
		}{a.CustomAnswers, a.AdditionalDetails}
	}
	return nil
}

func (s *Service) Get(ctx context.Context, userID string) (domain.ApplicationAnswersView, error) {
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return domain.ApplicationAnswersView{}, err
	}
	defer func() { _ = tx.Rollback() }()
	p, prefs, err := s.readFacts(ctx, tx, userID)
	if err != nil {
		return domain.ApplicationAnswersView{}, err
	}
	return s.view(ctx, tx, userID, &p, prefs)
}

func (s *Service) view(ctx context.Context, tx *sql.Tx, userID string, p *domain.ResumeProfile, prefs domain.WorkPreferences) (domain.ApplicationAnswersView, error) {
	v := domain.ApplicationAnswersView{ApplicationDefaults: p.ApplicationDefaults, Revision: AnswersRevision(p, prefs), Sources: map[string]domain.ApplicationAnswerSource{}, Catalog: append([]domain.ApplicationAnswerTopic(nil), Catalog...), Certifications: p.Certifications, Preferences: prefs, Profile: p}
	if v.Certifications == nil {
		v.Certifications = []domain.Certification{}
	}
	v.ProfileRevision = ProfileRevision(p)
	for _, topic := range Catalog {
		v.Sources[topic.ID] = domain.ApplicationAnswerSource{Source: "profile", Label: "From profile", State: topicState(p, prefs, topic.ID)}
	}
	v.Sources["work_arrangement"] = domain.ApplicationAnswerSource{Source: "preferences", Label: "From preferences", State: topicState(p, prefs, "work_arrangement")}
	rows, err := tx.QueryContext(ctx, `SELECT topic_id,fact_digest,source,evidence_json FROM application_answer_sources WHERE user_id=?`, userID)
	if err != nil {
		return v, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id, hash, source, raw string
		if err := rows.Scan(&id, &hash, &source, &raw); err != nil {
			return v, err
		}
		if hash != digest(topicValue(p, prefs, id)) {
			continue
		}
		info := domain.ApplicationAnswerSource{Source: source, Label: "Confirmed by you", State: topicState(p, prefs, id)}
		if source == "cv" {
			info.Label = "From CV"
		}
		if source == "candidate" {
			info.Label = "Saved by you"
		}
		if err := json.Unmarshal([]byte(raw), &info.Evidence); err != nil {
			return v, err
		}
		v.Sources[id] = info
	}
	return v, rows.Err()
}

func (s *Service) Patch(ctx context.Context, userID string, patch domain.ApplicationAnswersPatch) (domain.ApplicationAnswersView, error) {
	var out domain.ApplicationAnswersView
	if patch.Revision == "" || len(patch.Topics) == 0 || len(patch.Topics) > 11 {
		return out, fmt.Errorf("provide a current revision and changed answer topics")
	}
	err := s.transaction(ctx, userID, func(tx *sql.Tx) error {
		p, prefs, err := s.readFacts(ctx, tx, userID)
		if err != nil {
			return err
		}
		if AnswersRevision(&p, prefs) != patch.Revision {
			return ErrConflict
		}
		oldRaw, _ := json.Marshal(p)
		var old domain.ResumeProfile
		_ = json.Unmarshal(oldRaw, &old)
		if err := applyTopics(&p, patch.Topics); err != nil {
			return err
		}
		if err := s.Config.SetTx(ctx, tx, userID, "resume_profile", &p); err != nil {
			return err
		}
		for id := range patch.Topics {
			if err := s.sourceTx(ctx, tx, userID, id, "confirmed", nil, &p, prefs); err != nil {
				return err
			}
		}
		if err := s.attachImportTx(ctx, tx, userID, patch.ImportID, &p, prefs, patch.Topics); err != nil {
			return err
		}
		if err := s.syncReviewsTx(ctx, tx, userID, &old, &p, prefs); err != nil {
			return err
		}
		out, err = s.view(ctx, tx, userID, &p, prefs)
		return err
	})
	return out, err
}

func applyTopics(p *domain.ResumeProfile, topics map[string]json.RawMessage) error {
	for id, raw := range topics {
		if len(raw) > 32000 {
			return fmt.Errorf("answer topic is too large")
		}
		var err error
		switch id {
		case "work_rights":
			err = json.Unmarshal(raw, &p.ApplicationDefaults.WorkRights)
		case "sponsorship":
			if strings.HasPrefix(strings.TrimSpace(string(raw)), "{") {
				var values map[string]json.RawMessage
				err = json.Unmarshal(raw, &values)
				for key, value := range values {
					if err != nil {
						break
					}
					switch key {
					case "requires_sponsorship":
						err = json.Unmarshal(value, &p.ApplicationDefaults.RequiresSponsorship)
					case "future_requires_sponsorship":
						err = json.Unmarshal(value, &p.ApplicationDefaults.FutureRequiresSponsorship)
					default:
						err = fmt.Errorf("unknown sponsorship qualifier")
					}
				}
			} else {
				err = json.Unmarshal(raw, &p.ApplicationDefaults.RequiresSponsorship)
			}
		case "notice_period":
			var v struct {
				Notice       string `json:"notice_period"`
				Availability string `json:"availability"`
			}
			err = json.Unmarshal(raw, &v)
			p.ApplicationDefaults.NoticePeriod = v.Notice
			p.ApplicationDefaults.Availability = v.Availability
		case "earliest_start_date":
			p.ApplicationDefaults.EarliestStartDate = ""
			err = json.Unmarshal(raw, &p.ApplicationDefaults.EarliestStartDate)
		case "salary":
			var v struct {
				Legacy string                         `json:"salary_expectation"`
				Range  *domain.ApplicationSalaryRange `json:"salary_range"`
			}
			err = json.Unmarshal(raw, &v)
			p.ApplicationDefaults.SalaryExpectation = v.Legacy
			p.ApplicationDefaults.SalaryRange = v.Range
		case "relocation":
			err = json.Unmarshal(raw, &p.ApplicationDefaults.Relocation)
		case "travel":
			p.ApplicationDefaults.TravelConditions = ""
			err = json.Unmarshal(raw, &p.ApplicationDefaults.TravelConditions)
		case "working_hours":
			p.ApplicationDefaults.WorkingHours = ""
			err = json.Unmarshal(raw, &p.ApplicationDefaults.WorkingHours)
		case "work_arrangement":
			var v struct {
				Conditions string `json:"commute_conditions"`
			}
			err = json.Unmarshal(raw, &v)
			p.ApplicationDefaults.CommuteConditions = v.Conditions
		case "credentials":
			err = json.Unmarshal(raw, &p.Certifications)
		case "additional_answers":
			var v struct {
				Custom  []domain.ApplicationAnswer `json:"custom_answers"`
				Details string                     `json:"additional_details"`
			}
			err = json.Unmarshal(raw, &v)
			p.ApplicationDefaults.CustomAnswers = v.Custom
			p.ApplicationDefaults.AdditionalDetails = v.Details
		default:
			return fmt.Errorf("unknown answer topic %q", id)
		}
		if err != nil {
			return fmt.Errorf("invalid %s answer", id)
		}
	}
	return p.ApplicationDefaults.Validate()
}

func (s *Service) sourceTx(ctx context.Context, tx *sql.Tx, userID, id, source string, evidence []string, p *domain.ResumeProfile, prefs domain.WorkPreferences) error {
	raw, _ := json.Marshal(evidence)
	if evidence == nil {
		raw = []byte("[]")
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO application_answer_sources(user_id,topic_id,fact_digest,source,evidence_json) VALUES(?,?,?,?,?) ON CONFLICT(user_id,topic_id) DO UPDATE SET fact_digest=excluded.fact_digest,source=excluded.source,evidence_json=excluded.evidence_json,updated_at=CURRENT_TIMESTAMP`, userID, id, digest(topicValue(p, prefs, id)), source, string(raw))
	return err
}

// SaveProfile retains the flat legacy profile API, with optional optimistic
// concurrency. Source labels for untouched facts survive unrelated CV edits.
func (s *Service) SaveProfile(ctx context.Context, userID string, p *domain.ResumeProfile, expected string) error {
	return s.SaveProfileWithImport(ctx, userID, p, expected, "")
}

func (s *Service) SaveProfileWithImport(ctx context.Context, userID string, p *domain.ResumeProfile, expected, importID string) error {
	return s.transaction(ctx, userID, func(tx *sql.Tx) error {
		old, prefs, err := s.readFacts(ctx, tx, userID)
		if err != nil {
			return err
		}
		if expected != "" && expected != ProfileRevision(&old) {
			return ErrConflict
		}
		if err := p.ApplicationDefaults.Validate(); err != nil {
			return err
		}
		if err := s.Config.SetTx(ctx, tx, userID, "resume_profile", p); err != nil {
			return err
		}
		for _, topic := range Catalog {
			if digest(topicValue(&old, prefs, topic.ID)) != digest(topicValue(p, prefs, topic.ID)) {
				if err := s.sourceTx(ctx, tx, userID, topic.ID, "candidate", nil, p, prefs); err != nil {
					return err
				}
			}
		}
		if err := s.attachImportTx(ctx, tx, userID, importID, p, prefs, nil); err != nil {
			return err
		}
		return s.syncReviewsTx(ctx, tx, userID, &old, p, prefs)
	})
}

func (s *Service) syncReviewsTx(ctx context.Context, tx *sql.Tx, userID string, old, p *domain.ResumeProfile, prefs domain.WorkPreferences) error {
	oldRevision, newRevision := ProfileRevision(old), ProfileRevision(p)
	if oldRevision == newRevision {
		return nil
	}
	changed := map[string]bool{}
	for _, topic := range Catalog {
		changed[topic.ID] = digest(topicValue(old, prefs, topic.ID)) != digest(topicValue(p, prefs, topic.ID))
	}
	changed["additional_answers"] = digest(topicValue(old, prefs, "additional_answers")) != digest(topicValue(p, prefs, "additional_answers"))
	oldCV, newCV := *old, *p
	oldCV.ApplicationDefaults = domain.ApplicationDefaults{}
	newCV.ApplicationDefaults = domain.ApplicationDefaults{}
	cvChanged := digest(oldCV) != digest(newCV)
	rows, err := tx.QueryContext(ctx, `SELECT r.id,r.topic_id,COALESCE(e.proposed_answer,'') FROM application_answer_reviews r LEFT JOIN application_answer_encounters e ON e.id=(SELECT id FROM application_answer_encounters WHERE user_id=r.user_id AND review_id=r.id ORDER BY seq DESC LIMIT 1) WHERE r.user_id=? AND r.lifecycle='active' AND r.profile_revision=?`, userID, oldRevision)
	if err != nil {
		return err
	}
	var stale []string
	for rows.Next() {
		var id, topic, proposal string
		if err := rows.Scan(&id, &topic, &proposal); err != nil {
			_ = rows.Close()
			return err
		}
		if changed[topic] || changed["additional_answers"] || cvChanged && proposal != "" {
			stale = append(stale, id)
		}
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	for _, id := range stale {
		if err := s.releaseAnswerDeferralsTx(ctx, tx, userID, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE application_answer_reviews SET lifecycle='outdated',revision=revision+1,updated_at=CURRENT_TIMESTAMP WHERE id=? AND user_id=?`, id, userID); err != nil {
			return err
		}
	}
	// Unrelated missing facts stay actionable; corrections do not hide them.
	_, err = tx.ExecContext(ctx, `UPDATE application_answer_reviews SET profile_revision=? WHERE user_id=? AND lifecycle='active' AND profile_revision=?`, newRevision, userID, oldRevision)
	return err
}

// Make a known pre-submit fact deferral eligible for ordinary re-evaluation.
// Keep its history; no confirmation starts a run or clears submission holds.
func (s *Service) releaseAnswerDeferralsTx(ctx context.Context, tx *sql.Tx, userID, reviewID string) error {
	_, err := tx.ExecContext(ctx, `UPDATE jobs_skipped SET failure_code='answer_confirmed' WHERE user_id=? AND failure_code='answer_confirmation' AND id IN (SELECT e.job_id FROM application_answer_encounters e JOIN apply_attempts a ON a.id=e.attempt_id AND a.user_id=e.user_id WHERE e.user_id=? AND e.review_id=? AND e.proposed_answer='' AND a.outcome='blocked' AND a.reason_code='answer_confirmation' AND a.stage<>'submit_dispatch') AND NOT EXISTS (SELECT 1 FROM apply_attempts unsafe WHERE unsafe.user_id=jobs_skipped.user_id AND unsafe.job_id=jobs_skipped.id AND unsafe.outcome IN ('in_progress','submission_unknown','submitted_confirmed')) AND NOT EXISTS (SELECT 1 FROM jobs_applied applied WHERE applied.user_id=jobs_skipped.user_id AND applied.id=jobs_skipped.id)`, userID, userID, reviewID)
	return err
}

// RecordSources accepts only validated upload evidence supplied by the trusted
// extraction path. It never edits facts or changes trusted cache provenance.
func (s *Service) RecordSources(ctx context.Context, userID string, sources map[string]domain.ApplicationAnswerSource, expected string) error {
	return s.transaction(ctx, userID, func(tx *sql.Tx) error {
		p, prefs, err := s.readFacts(ctx, tx, userID)
		if err != nil {
			return err
		}
		if expected != "" && ProfileRevision(&p) != expected {
			return ErrConflict
		}
		for id, info := range sources {
			if info.Source != "cv" {
				continue
			}
			if topicValue(&p, prefs, id) == nil {
				return fmt.Errorf("unknown source topic")
			}
			if err := s.sourceTx(ctx, tx, userID, id, "cv", info.Evidence, &p, prefs); err != nil {
				return err
			}
		}
		return nil
	})
}

// CaptureImport stores minimized validated draft evidence separately. It never
// changes saved facts or claims that automatic extraction is user confirmation.
func (s *Service) CaptureImport(ctx context.Context, userID string, p *domain.ResumeProfile, prefill []domain.CVAnswerPrefill) (string, error) {
	if len(prefill) == 0 {
		return "", nil
	}
	importID := ""
	err := s.transaction(ctx, userID, func(tx *sql.Tx) error {
		_, prefs, err := s.readFacts(ctx, tx, userID)
		if err != nil {
			return err
		}
		grouped := map[string][]string{}
		for _, item := range prefill {
			if item.Source != "cv_evidence" || item.NeedsConfirmation || len(item.Evidence) == 0 {
				continue
			}
			for _, e := range item.Evidence {
				grouped[item.TopicID] = append(grouped[item.TopicID], cleanText(e.Quote, 2000))
			}
		}
		if len(grouped) == 0 {
			return nil
		}
		importID = uuid.NewString()
		for id, evidence := range grouped {
			raw, _ := json.Marshal(evidence)
			_, err := tx.ExecContext(ctx, `INSERT INTO application_answer_imports(user_id,import_id,topic_id,fact_digest,evidence_json,created_at) VALUES(?,?,?,?,?,?)`, userID, importID, id, digest(topicValue(p, prefs, id)), string(raw), time.Now().UTC().Format(time.RFC3339Nano))
			if err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM application_answer_imports WHERE user_id=? AND import_id NOT IN (SELECT import_id FROM application_answer_imports WHERE user_id=? GROUP BY import_id ORDER BY MAX(created_at) DESC,import_id DESC LIMIT 10)`, userID, userID)
		return err
	})
	return importID, err
}

func (s *Service) attachImportTx(ctx context.Context, tx *sql.Tx, userID, importID string, p *domain.ResumeProfile, prefs domain.WorkPreferences, topics map[string]json.RawMessage) error {
	if importID == "" {
		return nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT topic_id,fact_digest,evidence_json FROM application_answer_imports WHERE user_id=? AND import_id=?`, userID, importID)
	if err != nil {
		return err
	}
	type origin struct{ id, hash, raw string }
	var origins []origin
	for rows.Next() {
		var o origin
		if err := rows.Scan(&o.id, &o.hash, &o.raw); err != nil {
			_ = rows.Close()
			return err
		}
		origins = append(origins, o)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	if len(origins) == 0 {
		return ErrConflict
	}
	for _, o := range origins {
		if topics != nil {
			if _, ok := topics[o.id]; !ok {
				continue
			}
		}
		if o.hash != digest(topicValue(p, prefs, o.id)) {
			continue
		}
		var evidence []string
		if err := json.Unmarshal([]byte(o.raw), &evidence); err != nil {
			return err
		}
		if err := s.sourceTx(ctx, tx, userID, o.id, "cv", evidence, p, prefs); err != nil {
			return err
		}
	}
	return nil
}

func cleanText(s string, limit int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > limit {
		return string(r[:limit])
	}
	return s
}

func topicState(p *domain.ResumeProfile, prefs domain.WorkPreferences, id string) string {
	a := p.ApplicationDefaults
	known := false
	text := ""
	switch id {
	case "work_rights":
		for _, r := range a.WorkRights {
			known = known || r.Status != "" || r.VisaType != "" || r.Details != ""
		}
	case "sponsorship":
		known = a.RequiresSponsorship != nil || a.FutureRequiresSponsorship != nil
	case "notice_period":
		text = strings.TrimSpace(a.NoticePeriod + " " + a.Availability)
	case "earliest_start_date":
		text = a.EarliestStartDate
	case "salary":
		text = a.SalaryExpectation
		known = a.SalaryRange != nil && (a.SalaryRange.Minimum != nil || a.SalaryRange.Maximum != nil || a.SalaryRange.Details != "")
	case "relocation":
		known = a.Relocation != nil && (a.Relocation.Willing != nil || len(a.Relocation.Locations) > 0 || a.Relocation.Details != "")
	case "travel":
		text = a.TravelConditions
	case "working_hours":
		text = a.WorkingHours
	case "work_arrangement":
		text = a.CommuteConditions
		known = prefs.Remote || prefs.Hybrid || prefs.Onsite
	case "credentials":
		known = len(p.Certifications) > 0
	case "additional_answers":
		text = a.AdditionalDetails
		known = len(a.CustomAnswers) > 0
	}
	if strings.EqualFold(strings.TrimSpace(text), "not applicable") {
		return "not_applicable"
	}
	if known || strings.TrimSpace(text) != "" {
		return "confirmed"
	}
	return "unknown"
}
