package applicationanswers

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/user/jobifai/internal/domain"
)

// RecordEncounter must complete before a provisional answer is filled. IDs are
// caller supplied so retrying a durable write does not create another encounter.
func (s *Service) RecordEncounter(ctx context.Context, userID string, e domain.ApplicationAnswerEncounter) (string, error) {
	var reviewID string
	if e.ID == "" || e.Question == "" || (e.Status != "required" && e.Status != "suggested") || e.ProfileRevision == "" {
		return "", fmt.Errorf("invalid answer encounter")
	}
	if e.AnswerUsed != "" || e.UseStatus != "" && e.UseStatus != "not_used" {
		return "", fmt.Errorf("record answer use only after verified filling")
	}
	if e.FieldSignature == "" && e.GroupKey == "" {
		return "", fmt.Errorf("answer encounter requires a scoped field signature")
	}
	if len(e.ProposedAnswer) > 8000 || len(e.Evidence) > 20 {
		return "", fmt.Errorf("answer evidence is too large")
	}
	e.Evidence = append([]string(nil), e.Evidence...)
	for i := range e.Evidence {
		e.Evidence[i] = cleanText(e.Evidence[i], 2000)
	}
	group := encounterGroup(e)
	err := s.transaction(ctx, userID, func(tx *sql.Tx) error {
		var owner string
		err := tx.QueryRowContext(ctx, `SELECT user_id,review_id FROM application_answer_encounters WHERE id=?`, e.ID).Scan(&owner, &reviewID)
		if err == nil {
			if owner != userID {
				return domain.ErrNotFound
			}
			raw, _ := json.Marshal(e.Evidence)
			if e.Evidence == nil {
				raw = []byte("[]")
			}
			var matches int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM application_answer_encounters e JOIN application_answer_reviews r ON r.id=e.review_id AND r.user_id=e.user_id WHERE e.id=? AND e.user_id=? AND r.group_key=? AND e.question=? AND e.proposed_answer=? AND e.profile_revision=? AND e.field_signature=? AND e.mapping_id=? AND e.attempt_id=? AND e.evidence_json=? AND e.source=? AND e.reason=? AND e.field_instance_key=? AND e.job_id=? AND e.job_title=?`, e.ID, userID, group, cleanText(e.Question, 2000), e.ProposedAnswer, e.ProfileRevision, e.FieldSignature, e.MappingID, e.AttemptID, string(raw), e.Source, cleanText(e.Reason, 1000), e.FieldInstanceKey, e.JobID, cleanText(e.JobTitle, 240)).Scan(&matches); err != nil {
				return err
			}
			if matches != 1 {
				return ErrConflict
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var previousLifecycle, previousFact, previousProposal string
		groupErr := tx.QueryRowContext(ctx, `SELECT r.lifecycle,COALESCE(e.profile_revision,''),COALESCE(e.proposed_answer,'') FROM application_answer_reviews r LEFT JOIN application_answer_encounters e ON e.seq=(SELECT MAX(latest.seq) FROM application_answer_encounters latest WHERE latest.user_id=r.user_id AND latest.review_id=r.id) WHERE r.user_id=? AND r.group_key=?`, userID, group).Scan(&previousLifecycle, &previousFact, &previousProposal)
		if groupErr != nil && !errors.Is(groupErr, sql.ErrNoRows) {
			return groupErr
		}
		keepDismissed := previousLifecycle == "dismissed" && previousFact == e.ProfileRevision && previousProposal == e.ProposedAnswer
		notify := !keepDismissed && (errors.Is(groupErr, sql.ErrNoRows) || previousLifecycle != "active")
		_, err = tx.ExecContext(ctx, `INSERT INTO application_answer_reviews(id,user_id,group_key,question,status,topic_id,country,employer,platform,context_key,job_scope,profile_revision) VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(user_id,group_key) DO UPDATE SET question=excluded.question,status=excluded.status,profile_revision=excluded.profile_revision,lifecycle=CASE WHEN ? THEN 'dismissed' ELSE 'active' END,revision=application_answer_reviews.revision+1,updated_at=CURRENT_TIMESTAMP,resolved_at=CASE WHEN ? THEN application_answer_reviews.resolved_at ELSE NULL END`, uuid.NewString(), userID, group, cleanText(e.Question, 2000), e.Status, e.TopicID, cleanText(e.Country, 120), cleanText(e.Employer, 240), cleanText(e.Platform, 40), e.ContextKey, e.JobScope, e.ProfileRevision, keepDismissed, keepDismissed)
		if err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `SELECT id FROM application_answer_reviews WHERE user_id=? AND group_key=?`, userID, group).Scan(&reviewID); err != nil {
			return err
		}
		raw, _ := json.Marshal(e.Evidence)
		if e.Evidence == nil {
			raw = []byte("[]")
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO application_answer_encounters(id,review_id,user_id,mapping_id,field_signature,field_instance_key,attempt_id,job_id,job_title,question,proposed_answer,evidence_json,reason,source,profile_revision) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, e.ID, reviewID, userID, e.MappingID, e.FieldSignature, e.FieldInstanceKey, e.AttemptID, e.JobID, cleanText(e.JobTitle, 240), cleanText(e.Question, 2000), e.ProposedAnswer, string(raw), cleanText(e.Reason, 1000), e.Source, e.ProfileRevision)
		if err != nil {
			return err
		}
		if notify {
			_, err = tx.ExecContext(ctx, `UPDATE application_answer_notifications SET generation=generation+1 WHERE user_id=?`, userID)
		}
		return err
	})
	return reviewID, err
}

// MarkFilled records the exact text used once. No correction can alter it.
// Submission status is derived from the owned apply_attempts record separately.
func (s *Service) MarkFilled(ctx context.Context, userID, encounterID, answer string) error {
	if strings.TrimSpace(answer) == "" || len(answer) > 8000 {
		return fmt.Errorf("provide the exact filled answer")
	}
	return s.transaction(ctx, userID, func(tx *sql.Tx) error {
		var old, state, reviewID, attemptID, instance string
		if err := tx.QueryRowContext(ctx, `SELECT answer_used,use_status,review_id,attempt_id,field_instance_key FROM application_answer_encounters WHERE id=? AND user_id=?`, encounterID, userID).Scan(&old, &state, &reviewID, &attemptID, &instance); errors.Is(err, sql.ErrNoRows) {
			return domain.ErrNotFound
		} else if err != nil {
			return err
		}
		if state == "filled" {
			if old == answer {
				return nil
			}
			return ErrConflict
		}
		if state != "not_used" {
			return ErrConflict
		}
		if attemptID != "" && instance != "" {
			if _, err := tx.ExecContext(ctx, `UPDATE application_answer_encounters SET use_status='replaced' WHERE user_id=? AND attempt_id=? AND field_instance_key=? AND id<>? AND use_status='filled'`, userID, attemptID, instance, encounterID); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE application_answer_reviews SET lifecycle='superseded',revision=revision+1,updated_at=CURRENT_TIMESTAMP WHERE user_id=? AND id<>? AND lifecycle='active' AND id IN (SELECT e.review_id FROM application_answer_encounters e WHERE e.user_id=? AND e.attempt_id=? AND e.field_instance_key=? AND e.use_status='replaced' AND e.seq=(SELECT MAX(latest.seq) FROM application_answer_encounters latest WHERE latest.user_id=e.user_id AND latest.review_id=e.review_id))`, userID, reviewID, userID, attemptID, instance); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE application_answer_encounters SET answer_used=?,use_status='filled' WHERE id=? AND user_id=? AND use_status='not_used'`, answer, encounterID, userID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE application_answer_reviews SET status='used',revision=revision+1,updated_at=CURRENT_TIMESTAMP WHERE id=? AND user_id=?`, reviewID, userID)
		return err
	})
}

const reviewSelect = `SELECT r.id,r.question,CASE WHEN e.use_status IN ('filled','replaced') THEN 'used' WHEN e.proposed_answer='' THEN 'required' ELSE 'suggested' END,r.revision,r.profile_revision,r.topic_id,r.country,r.employer,r.platform,r.lifecycle,r.created_at,r.updated_at,
 (SELECT COUNT(*) FROM application_answer_encounters WHERE review_id=r.id AND user_id=r.user_id),
 COALESCE(e.proposed_answer,''),COALESCE(e.answer_used,''),COALESCE(e.evidence_json,'[]'),COALESCE(e.reason,''),COALESCE(e.source,''),COALESCE(e.job_title,''),COALESCE(e.job_id,''),
 CASE WHEN e.use_status='filled' AND a.outcome='submitted_confirmed' THEN 'submitted' WHEN e.use_status='filled' AND a.outcome='submission_unknown' THEN 'submission_unknown' ELSE COALESCE(e.use_status,'not_used') END
 FROM application_answer_reviews r
 LEFT JOIN application_answer_encounters e ON e.id=(SELECT id FROM application_answer_encounters WHERE review_id=r.id AND user_id=r.user_id ORDER BY seq DESC LIMIT 1)
 LEFT JOIN apply_attempts a ON a.id=e.attempt_id AND a.user_id=r.user_id `

type scanner interface{ Scan(...any) error }

func scanReview(row scanner) (domain.ApplicationAnswerReview, error) {
	var r domain.ApplicationAnswerReview
	var evidence string
	err := row.Scan(&r.ID, &r.Question, &r.Status, &r.Revision, &r.ProfileRevision, &r.TopicID, &r.Country, &r.Employer, &r.Platform, &r.Lifecycle, &r.CreatedAt, &r.UpdatedAt, &r.OccurrenceCount, &r.SuggestedAnswer, &r.AnswerUsed, &evidence, &r.Reason, &r.Source, &r.JobTitle, &r.JobID, &r.UseStatus)
	if err == nil {
		err = json.Unmarshal([]byte(evidence), &r.Evidence)
	}
	if errors.Is(err, sql.ErrNoRows) {
		err = domain.ErrNotFound
	}
	return r, err
}

func (s *Service) counts(ctx context.Context, tx *sql.Tx, userID string) (int, int, error) {
	var unresolved, unread int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM application_answer_reviews WHERE user_id=? AND lifecycle='active'`, userID).Scan(&unresolved)
	if err != nil {
		return 0, 0, err
	}
	err = tx.QueryRowContext(ctx, `SELECT CASE WHEN generation>read_generation THEN 1 ELSE 0 END FROM application_answer_notifications WHERE user_id=?`, userID).Scan(&unread)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return unresolved, unread, err
}

func (s *Service) ListReviews(ctx context.Context, userID, cursor, state string, limit int) (domain.ApplicationAnswerReviewPage, error) {
	p := domain.ApplicationAnswerReviewPage{Items: []domain.ApplicationAnswerReview{}}
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	condition := "r.lifecycle='active'"
	if state == "history" {
		condition = "r.lifecycle<>'active'"
	} else if state != "" && state != "active" {
		return p, fmt.Errorf("invalid review state")
	}
	args := []any{userID}
	if cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil || len(raw) > 300 {
			return p, fmt.Errorf("invalid review cursor")
		}
		parts := strings.SplitN(string(raw), "|", 2)
		if len(parts) != 2 {
			return p, fmt.Errorf("invalid review cursor")
		}
		condition += " AND (r.updated_at<? OR (r.updated_at=? AND r.id<?))"
		args = append(args, parts[0], parts[0], parts[1])
	}
	args = append(args, limit+1)
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return p, err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, reviewSelect+" WHERE r.user_id=? AND "+condition+" ORDER BY r.updated_at DESC,r.id DESC LIMIT ?", args...)
	if err != nil {
		return p, err
	}
	for rows.Next() {
		r, err := scanReview(rows)
		if err != nil {
			_ = rows.Close()
			return p, err
		}
		p.Items = append(p.Items, r)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return p, err
	}
	if len(p.Items) > limit {
		last := p.Items[limit-1]
		p.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(last.UpdatedAt + "|" + last.ID))
		p.Items = p.Items[:limit]
	}
	p.UnresolvedCount, p.UnreadCount, err = s.counts(ctx, tx, userID)
	return p, err
}

func (s *Service) GetReview(ctx context.Context, userID, id string) (domain.ApplicationAnswerReview, error) {
	r, err := scanReview(s.DB.QueryRowContext(ctx, reviewSelect+" WHERE r.id=? AND r.user_id=?", id, userID))
	if err != nil {
		return r, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT e.id,e.question,e.proposed_answer,e.answer_used,e.attempt_id,e.job_id,e.job_title,e.evidence_json,e.reason,e.source,e.profile_revision,e.created_at,CASE WHEN e.use_status='filled' AND a.outcome='submitted_confirmed' THEN 'submitted' WHEN e.use_status='filled' AND a.outcome='submission_unknown' THEN 'submission_unknown' ELSE e.use_status END FROM application_answer_encounters e LEFT JOIN apply_attempts a ON a.id=e.attempt_id AND a.user_id=e.user_id WHERE e.review_id=? AND e.user_id=? ORDER BY e.seq DESC LIMIT 50`, id, userID)
	if err != nil {
		return r, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var e domain.ApplicationAnswerEncounter
		var raw string
		err := rows.Scan(&e.ID, &e.Question, &e.ProposedAnswer, &e.AnswerUsed, &e.AttemptID, &e.JobID, &e.JobTitle, &raw, &e.Reason, &e.Source, &e.ProfileRevision, &e.CreatedAt, &e.UseStatus)
		if err != nil {
			return r, err
		}
		if err := json.Unmarshal([]byte(raw), &e.Evidence); err != nil {
			return r, err
		}
		e.ReviewID = id
		e.Country = r.Country
		e.Employer = r.Employer
		e.Platform = r.Platform
		e.TopicID = r.TopicID
		r.Encounters = append(r.Encounters, e)
	}
	return r, rows.Err()
}

func (s *Service) Confirm(ctx context.Context, userID, id string, request domain.ApplicationAnswerConfirmation) (domain.ApplicationAnswersView, error) {
	var out domain.ApplicationAnswersView
	answer := strings.TrimSpace(request.Answer)
	if request.Revision < 1 || answer == "" || len(answer) > 2000 {
		return out, fmt.Errorf("provide a reviewed answer of 1–2000 characters and current revision")
	}
	err := s.transaction(ctx, userID, func(tx *sql.Tx) error {
		var revision int64
		var observed, lifecycle, question, country, employer, jobScope string
		err := tx.QueryRowContext(ctx, `SELECT revision,profile_revision,lifecycle,question,country,employer,job_scope FROM application_answer_reviews WHERE id=? AND user_id=?`, id, userID).Scan(&revision, &observed, &lifecycle, &question, &country, &employer, &jobScope)
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ErrNotFound
		}
		if err != nil {
			return err
		}
		if revision != request.Revision || lifecycle != "active" {
			return ErrConflict
		}
		p, prefs, err := s.readFacts(ctx, tx, userID)
		if err != nil {
			return err
		}
		current := ProfileRevision(&p)
		if observed != current || request.ProfileRevision != "" && request.ProfileRevision != current {
			return ErrConflict
		}
		if len(request.Topics) > 0 {
			if err := applyTopics(&p, request.Topics); err != nil {
				return err
			}
			for topic := range request.Topics {
				if err := s.sourceTx(ctx, tx, userID, topic, "confirmed", nil, &p, prefs); err != nil {
					return err
				}
			}
		}
		rows, err := tx.QueryContext(ctx, `SELECT DISTINCT mapping_id FROM application_answer_encounters WHERE review_id=? AND user_id=? AND mapping_id<>''`, id, userID)
		if err != nil {
			return err
		}
		var mappingIDs []string
		for rows.Next() {
			var mappingID string
			if err := rows.Scan(&mappingID); err != nil {
				_ = rows.Close()
				return err
			}
			mappingIDs = append(mappingIDs, mappingID)
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return err
		}
		if p.ApplicationDefaults.FormAnswers == nil {
			p.ApplicationDefaults.FormAnswers = map[string]string{}
		}
		for _, mappingID := range mappingIDs {
			if s.ConfirmMapping == nil {
				return errors.New("mapping confirmation unavailable")
			}
			key := "mapping_" + mappingID
			p.ApplicationDefaults.FormAnswers[key] = answer
			if err := s.ConfirmMapping(ctx, tx, userID, mappingID, "application_defaults.form_answers."+key, &p); err != nil {
				return err
			}
		}
		if len(mappingIDs) == 0 {
			jobID := ""
			if jobScope != "" {
				if err := tx.QueryRowContext(ctx, `SELECT job_id FROM application_answer_encounters WHERE user_id=? AND review_id=? ORDER BY seq DESC LIMIT 1`, userID, id).Scan(&jobID); err != nil {
					return err
				}
				if jobID == "" {
					return fmt.Errorf("job-specific answer requires its application identity")
				}
			}
			// Candidate-authored answer remains scoped to its question and country. It
			// supplies future model evidence but grants no browser or cache authority.
			updated := false
			for i := range p.ApplicationDefaults.CustomAnswers {
				a := &p.ApplicationDefaults.CustomAnswers[i]
				if a.Question == question && a.Country == country && a.Employer == employer && a.JobID == jobID {
					a.Answer = answer
					updated = true
					break
				}
			}
			if !updated {
				p.ApplicationDefaults.CustomAnswers = append(p.ApplicationDefaults.CustomAnswers, domain.ApplicationAnswer{Question: question, Answer: answer, Country: country, Employer: employer, JobID: jobID})
			}
		}
		if err := p.ApplicationDefaults.Validate(); err != nil {
			return err
		}
		if err := s.Config.SetTx(ctx, tx, userID, "resume_profile", &p); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE application_answer_reviews SET lifecycle='resolved',revision=revision+1,resolved_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP WHERE id=? AND user_id=? AND revision=?`, id, userID, revision); err != nil {
			return err
		}
		if err := s.releaseAnswerDeferralsTx(ctx, tx, userID, id); err != nil {
			return err
		}
		if len(request.Topics) == 0 {
			// Independent confirmations append/update only this group's owned
			// mapping facts. Rebase other unchanged decisions so a candidate can
			// review several items without a refresh/conflict after every click.
			// Encounter evidence keeps its original observed revision forever.
			_, err := tx.ExecContext(ctx, `UPDATE application_answer_reviews SET profile_revision=? WHERE user_id=? AND lifecycle='active' AND profile_revision=? AND id<>? AND id NOT IN (SELECT other.review_id FROM application_answer_encounters other JOIN application_answer_encounters current ON current.mapping_id=other.mapping_id AND current.user_id=other.user_id WHERE current.review_id=? AND current.user_id=? AND current.mapping_id<>'')`, ProfileRevision(&p), userID, current, id, id, userID)
			if err != nil {
				return err
			}
		}
		out, err = s.view(ctx, tx, userID, &p, prefs)
		return err
	})
	return out, err
}

func (s *Service) Dismiss(ctx context.Context, userID, id string, revision int64) error {
	return s.transaction(ctx, userID, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE application_answer_reviews SET lifecycle='dismissed',revision=revision+1,resolved_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP WHERE id=? AND user_id=? AND revision=? AND lifecycle='active'`, id, userID, revision)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrConflict
		}
		return nil
	})
}

func (s *Service) MarkRead(ctx context.Context, userID string) error {
	return s.transaction(ctx, userID, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE application_answer_notifications SET read_generation=generation WHERE user_id=?`, userID)
		return err
	})
}

// ParseLimit bounds work and payload size even for accounts with many variants.
func ParseLimit(raw string) int { n, _ := strconv.Atoi(raw); return n }

func encounterGroup(e domain.ApplicationAnswerEncounter) string {
	key := e.GroupKey
	if key == "" {
		key = e.FieldSignature
	}
	return digest([]string{key, e.Country, e.Employer, e.Platform, e.JobScope, e.ContextKey, e.TopicID})
}

// KnownMissing matches an unchanged, exact scoped required question. It can
// avoid another model request only when the fact digest and context match the
// original missing observation. Rebased review metadata is not sufficient.
func (s *Service) KnownMissing(ctx context.Context, userID string, e domain.ApplicationAnswerEncounter) (bool, error) {
	if e.FieldSignature == "" || e.ProfileRevision == "" {
		return false, nil
	}
	var count int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM application_answer_reviews r JOIN application_answer_encounters e ON e.id=(SELECT id FROM application_answer_encounters WHERE user_id=r.user_id AND review_id=r.id ORDER BY seq DESC LIMIT 1) WHERE r.user_id=? AND r.group_key=? AND r.lifecycle IN ('active','dismissed') AND r.status='required' AND e.profile_revision=? AND e.proposed_answer=''`, userID, encounterGroup(e), e.ProfileRevision).Scan(&count)
	return count == 1, err
}
