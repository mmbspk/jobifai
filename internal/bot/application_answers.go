package bot

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/user/jobifai/internal/domain"
)

// ApplicationAnswerRecorder stores candidate decisions independently of mapping
// trust. A review record has no authority to execute a browser action.
type ApplicationAnswerRecorder interface {
	RecordEncounter(context.Context, string, domain.ApplicationAnswerEncounter) (string, error)
	MarkFilled(context.Context, string, string, string) error
}

func (m *Manager) SetApplicationAnswers(recorder ApplicationAnswerRecorder) {
	m.applicationAnswers = recorder
}

// Eligible phrasing is independently verified, descriptive composition only.
// Declarations, dates, pay, eligibility and licences require supplied facts and
// retain the existing strict resolution guards, rather than provisional use.
func eligibleAnswerReview(snap StepSnapshot, f SnapField, r FieldResolution, profile *domain.ResumeProfile) bool {
	if (r.Source != SourceLLM && r.Source != SourceMapping) || r.IntentType != FieldIntentLLM || r.Pending || r.Action != ActionFill || r.Provenance != ProvenanceVerifiedByModel {
		return false
	}
	if f.Type == SnapTypeFile || f.Type == SnapTypeUnsupported || manualDeclaration(f) {
		return false
	}
	text := strings.ToLower(f.Label + " " + f.Group + " " + f.HelpText)
	if legacySensitiveQuestion(text) || matchesAny(text, "start date", "notice", "available", "availability", "licence", "license", "certif", "consent", "disabil", "criminal", "convict", "gender", "ethnicity") {
		return false
	}
	if r.Source == SourceMapping {
		return reusableGroundedCache(r.cached, profile, snap, f, r)
	}
	return validGroundedProof(r.grounded, profile, snap, f, r.IntentType, r.BindingKey, r.Value)
}

func answerTopic(f SnapField) string {
	text := strings.ToLower(f.Label + " " + f.HelpText)
	switch {
	case isWorkAuthorizationQuestion(text), matchesAny(text, "visa type", "visa expir"):
		return "work_rights"
	case strings.Contains(text, "sponsor"):
		return "sponsorship"
	case strings.Contains(text, "notice"):
		return "notice_period"
	case matchesAny(text, "start date", "available", "availability"):
		return "earliest_start_date"
	case isSalaryFormQuestion(text):
		return "salary"
	case strings.Contains(text, "relocat"):
		return "relocation"
	case strings.Contains(text, "travel"):
		return "travel"
	case matchesAny(text, "shift", "working hours"):
		return "working_hours"
	case matchesAny(text, "remote", "commut", "hybrid"):
		return "work_arrangement"
	case matchesAny(text, "certif", "licence", "license"):
		return "credentials"
	default:
		return ""
	}
}

func candidatePending(r FieldResolution) bool {
	return r.Pending && (r.Reason == PendingMissingProfile || r.Reason == PendingAmbiguous || r.Reason == PendingInvalidOption)
}

// prepareAnswerReviews runs before any provisional fill. Its IDs are stable for
// this execution; the recorder deduplicates retries and preserves all proposals.
func (b *Bot) prepareAnswerReviews(ctx context.Context, lazy *lazyDocGen, snap StepSnapshot, interp StepInterpretation, profile *domain.ResumeProfile) (map[string]string, error) {
	ids := map[string]string{}
	if b.cfg.ApplicationAnswers != nil && lazy != nil {
		if lazy.answerProfileDigest == "" {
			lazy.answerProfileDigest = profileDigest(profile)
		}
		if err := b.ensureAnswerFactsCurrent(lazy); err != nil {
			return nil, err
		}
	}
	for _, r := range interp.Fields {
		f, ok := snap.FieldByRef(r.Ref)
		if !ok {
			continue
		}
		if r.Pending && !candidatePending(r) {
			return nil, fmt.Errorf("application form could not be resolved: %s", r.Reason)
		}
		provisional := eligibleAnswerReview(snap, f, r, profile)
		if !candidatePending(r) && !provisional {
			continue
		}
		if b.cfg.ApplicationAnswers == nil {
			continue
		}
		encounter := reviewEncounterIdentity(lazy, snap, f, profile, candidatePending(r))
		encounter.ID, encounter.Status, encounter.ProposedAnswer, encounter.Reason, encounter.Source = newAttemptID(), "suggested", r.Value, "Supported phrasing needs your confirmation", string(r.Source)
		if candidatePending(r) {
			encounter.Status = "required"
			encounter.ProposedAnswer = ""
			encounter.Reason = "A required fact is missing or cannot be verified; this application was deferred"
		}
		if r.grounded != nil {
			for _, entry := range r.grounded.evidence {
				encounter.Evidence = append(encounter.Evidence, entry.Quote)
			}
		} else if r.cached != nil {
			for _, entry := range r.cached.Evidence {
				encounter.Evidence = append(encounter.Evidence, entry.Quote)
			}
		}
		if b.cfg.DB != nil {
			err := b.cfg.DB.QueryRowContext(ctx, `SELECT id FROM form_field_mappings WHERE user_id=? AND field_sig=? AND employer_key=?`, b.cfg.UserID, encounter.FieldSignature, snap.EmployerKey).Scan(&encounter.MappingID)
			if err != nil {
				encounter.MappingID = ""
			}
		}
		if _, err := b.cfg.ApplicationAnswers.RecordEncounter(ctx, b.cfg.UserID, encounter); err != nil {
			return nil, fmt.Errorf("persist answer review: %w", err)
		}
		if provisional {
			ids[r.Ref] = encounter.ID
		}
	}
	return ids, nil
}

func reviewEncounterIdentity(lazy *lazyDocGen, snap StepSnapshot, f SnapField, profile *domain.ResumeProfile, pending bool) domain.ApplicationAnswerEncounter {
	encounter := domain.ApplicationAnswerEncounter{FieldSignature: f.FieldSignature(snap.Platform), Question: f.Label, TopicID: answerTopic(f), Platform: snap.Platform, Employer: snap.EmployerKey, JobScope: f.JobContext, ProfileRevision: profileDigest(profile), UseStatus: "not_used"}
	identity := f.ControlID
	if identity == "" {
		ordinal := f.Ref
		if f.BrowserField != nil {
			ordinal = fmt.Sprint(f.BrowserField.Index)
		}
		raw, _ := json.Marshal([]string{f.Group, f.Label, string(f.Type), ordinal})
		identity = string(raw)
	}
	instance, _ := json.Marshal(struct {
		Step    int
		Control string
	}{snap.StepIndex, identity})
	encounter.FieldInstanceKey = fmt.Sprintf("%x", sha256.Sum256(instance))
	// Exact shape is the conservative default. Only a verified equivalence may
	// supply GroupKey; common labels alone cannot merge distinct declarations.
	qualifiers, _ := json.Marshal(struct {
		Group, Help, Timing, OptionsHash, InputType, Locale, Min, Max, Step, Pattern string
		Options, Values                                                              []string
		Required                                                                     bool
		MaxChars                                                                     int
	}{f.Group, f.HelpText, sponsorshipTiming(f), f.OptionSetHash, f.InputType, f.Locale, f.MinValue, f.MaxValue, f.StepValue, f.Pattern, f.Options, f.OptionValues, f.Required, f.MaxChars})
	encounter.ContextKey = string(qualifiers)
	if lazy != nil {
		encounter.AttemptID = lazy.attemptID
		encounter.JobID = lazy.job.ID
		encounter.JobTitle = lazy.job.Title
	}
	if profile != nil {
		for _, right := range profile.ApplicationDefaults.WorkRights {
			if questionCountryMatches(f, right.Country) {
				if encounter.Country != "" && !strings.EqualFold(encounter.Country, right.Country) {
					encounter.Country = ""
					break
				}
				encounter.Country = right.Country
			}
		}
	}
	if encounter.Country == "" {
		for country := range applicationCountryAliases {
			if questionCountryMatches(f, country) {
				if encounter.Country != "" {
					encounter.Country = ""
					break
				}
				encounter.Country = country
			}
		}
	}
	if pending {
		// Locally recognised simple catalog questions have a stable meaning.
		// Retain issuer, jurisdiction, timing and choices while removing the
		// unrelated vacancy hash so repeats within that scope form one decision.
		jurisdictionKnown := encounter.Country != "" || encounter.TopicID != "sponsorship" && encounter.TopicID != "work_rights"
		if fact := missingCatalogFact(f); fact != "" && jurisdictionKnown && simpleCandidateQuestion(strings.ToLower(f.Label)) && f.Group == "" && f.HelpText == "" && !isJobDependentQuestion(f.Label) {
			encounter.GroupKey = "catalog:" + fact + ":" + sponsorshipTiming(f) + ":" + string(f.Type)
			encounter.JobScope = ""
		}
	}
	return encounter
}

func missingCatalogFact(f SnapField) string {
	label := strings.ToLower(strings.Trim(normalizeLabel(f.Label), " *?:."))
	switch {
	case label == "availability":
		return "application_defaults.availability"
	case label == "earliest start date":
		return "application_defaults.earliest_start_date"
	case answerTopic(f) == "notice_period":
		return "application_defaults.notice_period"
	case answerTopic(f) == "salary":
		return "application_defaults.salary_expectation"
	case answerTopic(f) == "sponsorship":
		return "application_defaults.requires_sponsorship"
	}
	return ""
}

func (b *Bot) knownMissingChecker(lazy *lazyDocGen) func(context.Context, StepSnapshot, SnapField, *domain.ResumeProfile) (bool, error) {
	lookup, ok := b.cfg.ApplicationAnswers.(interface {
		KnownMissing(context.Context, string, domain.ApplicationAnswerEncounter) (bool, error)
	})
	if !ok {
		return nil
	}
	return func(ctx context.Context, snap StepSnapshot, field SnapField, profile *domain.ResumeProfile) (bool, error) {
		return lookup.KnownMissing(ctx, b.cfg.UserID, reviewEncounterIdentity(lazy, snap, field, profile, true))
	}
}

func (b *Bot) answerFillRecorder(ids map[string]string) func(context.Context, SnapField, FieldResolution) error {
	return func(ctx context.Context, f SnapField, r FieldResolution) error {
		id := ids[f.Ref]
		if id == "" || b.cfg.ApplicationAnswers == nil {
			return nil
		}
		if err := b.cfg.ApplicationAnswers.MarkFilled(ctx, b.cfg.UserID, id, r.Value); err != nil {
			return fmt.Errorf("persist filled answer: %w", err)
		}
		return nil
	}
}

func legacyAnswerKey(question string, options []string) string {
	question = strings.Split(question, "\n\nReply with at most ")[0]
	if len(options) == 0 {
		options = nil
	} else {
		copyOptions := make([]string, len(options))
		for i, option := range options {
			copyOptions[i] = normalizeLabel(option)
		}
		options = copyOptions
	}
	raw, _ := json.Marshal(struct {
		Question string
		Options  []string
	}{normalizeLabel(question), options})
	return string(raw)
}

func legacyFieldKey(field formField) string {
	raw, _ := json.Marshal(field)
	return string(raw)
}

func preparedLegacyAnswer(lazy *lazyDocGen, question string, options []string) (string, bool) {
	if lazy == nil || lazy.answerPlan == nil {
		return "", false
	}
	var found *FieldResolution
	for _, field := range lazy.answerSnapshot.Fields {
		if field.BrowserField == nil {
			continue
		}
		key := legacyFieldKey(*field.BrowserField)
		if lazy.answerActiveKey != "" && key != lazy.answerActiveKey {
			continue
		}
		if legacyAnswerKey(field.Label, field.Options) != legacyAnswerKey(question, options) {
			continue
		}
		r, ok := lazy.answerPlan[key]
		if !ok {
			continue
		}
		if found != nil {
			return "", false
		}
		found = &r
	}
	if found == nil {
		return "", false
	}
	return found.Value, true
}

// Legacy keeps its existing browser fill path but shares one batched fact
// interpretation. Reusing this plan avoids duplicate model calls per control.
func (b *Bot) prepareLegacyAnswers(ctx context.Context, lazy *lazyDocGen, fields []formField) error {
	if b.cfg.ApplicationAnswers == nil || lazy == nil {
		return nil
	}
	if err := b.ensureAnswerFactsCurrent(lazy); err != nil {
		return err
	}
	var questions []formField
	for _, f := range fields {
		if f.Type == "file" || (f.DocumentKind != "" && f.DocumentKind != "unknown") {
			continue
		}
		questions = append(questions, f)
	}
	lazy.answerPlan = nil
	lazy.answerReviewIDs = nil
	if len(questions) == 0 {
		return nil
	}
	snap := captureStepSnapshot(questions, nil, string(b.cfg.Platform), lazy.answerStepIndex, "legacy-answers")
	scopeJobDependentFields(&snap, lazy.job)
	snap.EmployerKey = lazy.job.Company
	profile := b.currentProfile()
	lazy.answerProfileDigest = profileDigest(profile)
	si := StepInterpreter{LLM: boundFormLLM(b.cfg.AdaptiveLLM), Mapping: b.cfg.AdaptiveMapping, KnownMissing: b.knownMissingChecker(lazy)}
	interp, err := si.InterpretStep(ctx, snap, profile)
	if err != nil {
		return err
	}
	recordAdaptiveProposals(ctx, b.cfg.AdaptiveMapping, snap, interp)
	ids, err := b.prepareAnswerReviews(ctx, lazy, snap, interp, profile)
	if err != nil {
		return err
	}
	if interp.Paused {
		return errAdaptiveQuestionReview
	}
	lazy.answerPlan = map[string]FieldResolution{}
	lazy.answerReviewIDs = map[string]string{}
	lazy.answerSnapshot = snap
	for _, r := range interp.Fields {
		if f, ok := snap.FieldByRef(r.Ref); ok {
			if f.BrowserField == nil {
				continue
			}
			key := legacyFieldKey(*f.BrowserField)
			lazy.answerPlan[key] = r
			lazy.answerReviewIDs[key] = ids[r.Ref]
		}
	}
	return nil
}

func (b *Bot) markLegacyAnswerFilled(ctx context.Context, lazy *lazyDocGen, f formField, value string) error {
	if lazy == nil || b.cfg.ApplicationAnswers == nil || value == "" {
		return nil
	}
	key := legacyFieldKey(f)
	id := lazy.answerReviewIDs[key]
	if id != "" {
		if err := b.cfg.ApplicationAnswers.MarkFilled(ctx, b.cfg.UserID, id, value); err != nil {
			return fmt.Errorf("persist filled answer: %w", err)
		}
	}
	if r, ok := lazy.answerPlan[key]; ok && r.Value == value && r.Provenance == ProvenanceVerifiedByModel && b.cfg.AdaptiveMapping != nil {
		if field, found := lazy.answerSnapshot.FieldByRef(r.Ref); found && validGroundedProof(r.grounded, b.currentProfile(), lazy.answerSnapshot, field, r.IntentType, r.BindingKey, value) {
			if err := b.cfg.AdaptiveMapping.Write(ctx, lazy.answerSnapshot, []FieldResolution{r}); err != nil {
				return fmt.Errorf("persist verified answer mapping: %w", err)
			}
		}
	}
	return nil
}

// Operational causes take precedence if joined with a candidate deferral.
func isCandidateDeferral(err error) bool {
	return errors.Is(err, errAdaptiveQuestionReview) && onlyFormQuestionReview(err)
}

func (b *Bot) ensureAnswerFactsCurrent(lazy *lazyDocGen) error {
	if b.cfg.ApplicationAnswers == nil || lazy == nil || lazy.answerProfileDigest == "" {
		return nil
	}
	current := b.cfg.Profile
	if b.cfg.ProfileLoader != nil {
		current = b.cfg.ProfileLoader()
		if current == nil {
			return errors.New("current application profile could not be loaded; stop before using stale facts")
		}
	}
	if lazy.answerProfileDigest != profileDigest(current) {
		return errors.New("application profile changed during filling; retry this application in a future run with current facts")
	}
	return nil
}
