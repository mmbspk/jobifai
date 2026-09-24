package quota

import (
	"context"
	"database/sql"
	"sync"
	"time"

	"github.com/user/jobifai/internal/auth"
	"github.com/user/jobifai/internal/domain"
)

// Service enforces credit-based AI quotas (trial + starter/pro subscriptions).
type Service struct {
	db    *sql.DB
	cfg   settingsKV
	users *auth.UserStore

	mu       sync.Mutex
	sessions map[string]*graceSession
}

type graceSession struct {
	allowedCredits int64
	usedCredits    int64
}

// NewService creates a quota service.
func NewService(db *sql.DB, cfg settingsKV, users *auth.UserStore) *Service {
	return &Service{
		db:       db,
		cfg:      cfg,
		users:    users,
		sessions: make(map[string]*graceSession),
	}
}

func (s *Service) isAdmin(userID string) bool {
	if s.users == nil {
		return false
	}
	u, err := s.users.ByID(userID)
	return err == nil && u.IsAdmin
}

func (s *Service) getOrCreateRow(userID string) (domain.UserQuotaRow, error) {
	row, err := getRow(s.db, userID)
	if err == nil {
		return s.applyOverrides(userID, row), nil
	}
	if !isNotFound(err) {
		return domain.UserQuotaRow{}, err
	}
	if err := s.InitTrial(context.Background(), userID); err != nil {
		return domain.UserQuotaRow{}, err
	}
	row, err = getRow(s.db, userID)
	if err != nil {
		return domain.UserQuotaRow{}, err
	}
	return s.applyOverrides(userID, row), nil
}

func (s *Service) applyOverrides(userID string, row domain.UserQuotaRow) domain.UserQuotaRow {
	o := loadOverrides(s.cfg, userID)
	if o.EnforcementEnabled != nil {
		row.EnforcementEnabled = *o.EnforcementEnabled
	}
	if o.TrialCredits != nil && row.Plan == domain.QuotaPlanTrial {
		row.TrialRemainingCredits = int64(*o.TrialCredits)
	}
	if o.PeriodAllowanceCredits != nil && row.Plan != domain.QuotaPlanTrial {
		row.PeriodAllowanceCredits = *o.PeriodAllowanceCredits
	}
	return row
}

// InitTrial creates a trial quota row if one does not exist.
func (s *Service) InitTrial(_ context.Context, userID string) error {
	if _, err := getRow(s.db, userID); err == nil {
		return nil
	} else if !isNotFound(err) {
		return err
	}
	def := loadDefaults(s.cfg)
	ends := time.Now().UTC().Add(time.Duration(def.TrialDays) * 24 * time.Hour)
	return insertTrialRow(s.db, userID, int64(def.TrialCredits), ends, def.EnforcementDefault)
}

func (s *Service) trialExpired(row domain.UserQuotaRow) bool {
	if row.TrialEndsAt != nil && time.Now().After(*row.TrialEndsAt) {
		return true
	}
	return row.TrialRemainingCredits <= 0
}

func (s *Service) monthlyRemaining(row domain.UserQuotaRow) int64 {
	return row.PeriodAllowanceCredits - row.PeriodUsedCredits - row.OverageDebtCredits
}

func (s *Service) totalRemaining(row domain.UserQuotaRow) int64 {
	if row.Plan == domain.QuotaPlanTrial {
		if s.trialExpired(row) {
			return 0
		}
		return row.TrialRemainingCredits
	}
	m := s.monthlyRemaining(row)
	if m < 0 {
		m = 0
	}
	return m + row.TopUpCreditsRemaining
}

func (s *Service) fillStatus(st *domain.QuotaStatus, row domain.UserQuotaRow, def domain.QuotaDefaults) {
	st.EnforcementEnabled = row.EnforcementEnabled
	st.Plan = row.Plan
	st.OverageDebtCredits = row.OverageDebtCredits
	st.TopUpCreditsRemaining = row.TopUpCreditsRemaining
	st.StripeConfigured = def.StripePriceStarter != "" || def.StripePricePro != ""
	st.TopUpPacks = def.TopUpPacks

	if row.Plan == domain.QuotaPlanTrial {
		st.TrialRemainingCredits = &row.TrialRemainingCredits
		st.TrialEndsAt = rfc3339(row.TrialEndsAt)
		st.AllowanceCredits = int64(def.TrialCredits)
		used := int64(def.TrialCredits) - row.TrialRemainingCredits
		if used < 0 {
			used = 0
		}
		st.UsedCredits = used
		st.RemainingCredits = row.TrialRemainingCredits
		if st.AllowanceCredits > 0 {
			st.UsagePercent = float64(st.UsedCredits) / float64(st.AllowanceCredits) * 100
		}
		st.Blocked = s.trialExpired(row)
		if st.Blocked {
			if row.TrialRemainingCredits <= 0 {
				st.BlockCode = "trial_exhausted"
			} else {
				st.BlockCode = "trial_expired"
			}
		}
		return
	}

	st.PeriodStart = rfc3339(row.PeriodStart)
	st.PeriodEnd = rfc3339(row.PeriodEnd)
	st.AllowanceCredits = row.PeriodAllowanceCredits
	st.UsedCredits = row.PeriodUsedCredits
	st.RemainingCredits = s.totalRemaining(row)
	if st.AllowanceCredits > 0 {
		st.UsagePercent = float64(st.UsedCredits) / float64(st.AllowanceCredits) * 100
		if st.UsagePercent > 100 {
			st.UsagePercent = 100
		}
	}
	s.mu.Lock()
	_, st.GraceSessionActive = s.sessions[row.UserID]
	s.mu.Unlock()
	if s.totalRemaining(row) <= 0 && !st.GraceSessionActive {
		st.Blocked = true
		st.BlockCode = "period_exhausted"
	}
}

// Status returns quota state for UI and gating.
func (s *Service) Status(_ context.Context, userID string) (domain.QuotaStatus, error) {
	if s.isAdmin(userID) {
		return domain.QuotaStatus{Unlimited: true, Plan: "admin"}, nil
	}
	row, err := s.getOrCreateRow(userID)
	if err != nil {
		return domain.QuotaStatus{}, err
	}
	def := loadDefaults(s.cfg)
	var st domain.QuotaStatus
	if !row.EnforcementEnabled {
		st.Unlimited = true
		return st, nil
	}
	s.fillStatus(&st, row, def)
	return st, nil
}

// BeforeLLM checks whether a call with the given estimated cost may proceed.
func (s *Service) BeforeLLM(_ context.Context, userID, model string, estInput, estOutput int) error {
	if s.isAdmin(userID) {
		return nil
	}
	row, err := s.getOrCreateRow(userID)
	if err != nil {
		return err
	}
	if !row.EnforcementEnabled {
		return nil
	}
	def := loadDefaults(s.cfg)
	est := EstimateBurnCredits(def, model, estInput, estOutput)
	if est <= 0 {
		est = 1
	}
	if row.Plan == domain.QuotaPlanTrial {
		if s.trialExpired(row) {
			return &ExceededError{Code: "trial_exhausted", Scope: "trial"}
		}
		if row.TrialRemainingCredits < est {
			return &ExceededError{Code: "trial_exhausted", Scope: "trial"}
		}
		return nil
	}
	if s.totalRemaining(row) >= est {
		return nil
	}
	s.mu.Lock()
	sess, inGrace := s.sessions[userID]
	s.mu.Unlock()
	if !inGrace {
		return &ExceededError{Code: "period_exhausted", Scope: "period"}
	}
	needOver := est - s.totalRemaining(row)
	if needOver < 0 {
		needOver = 0
	}
	if sess.usedCredits+needOver > sess.allowedCredits {
		return &ExceededError{Code: "grace_exceeded", Scope: "period"}
	}
	s.mu.Lock()
	sess.usedCredits += needOver
	s.mu.Unlock()
	return nil
}

func (s *Service) deductPaid(row *domain.UserQuotaRow, burn int64) {
	monthlyLeft := s.monthlyRemaining(*row)
	if monthlyLeft >= burn {
		row.PeriodUsedCredits += burn
		return
	}
	if monthlyLeft > 0 {
		row.PeriodUsedCredits += monthlyLeft
		burn -= monthlyLeft
	} else if monthlyLeft < 0 {
		// already over allowance; consume top-up only
	}
	row.TopUpCreditsRemaining -= burn
	if row.TopUpCreditsRemaining < 0 {
		row.TopUpCreditsRemaining = 0
	}
}

// RecordLLM deducts credits after a successful LLM call.
func (s *Service) RecordLLM(_ context.Context, userID, model string, input, output int64) error {
	if s.isAdmin(userID) {
		return nil
	}
	def := loadDefaults(s.cfg)
	burn := BurnCredits(def, model, input, output)
	if burn <= 0 {
		return nil
	}
	row, err := s.getOrCreateRow(userID)
	if err != nil {
		return err
	}
	if !row.EnforcementEnabled {
		return nil
	}
	if row.Plan == domain.QuotaPlanTrial {
		row.TrialRemainingCredits -= burn
		if row.TrialRemainingCredits < 0 {
			row.TrialRemainingCredits = 0
		}
		return updateRow(s.db, row)
	}
	s.deductPaid(&row, burn)
	return updateRow(s.db, row)
}

// BeginSubscriberSession marks an automation session that may use grace overage.
func (s *Service) BeginSubscriberSession(userID string) {
	if s.isAdmin(userID) {
		return
	}
	row, err := s.getOrCreateRow(userID)
	if err != nil || row.Plan == domain.QuotaPlanTrial || !row.EnforcementEnabled {
		return
	}
	def := loadDefaults(s.cfg)
	s.mu.Lock()
	s.sessions[userID] = &graceSession{allowedCredits: def.SubscriberGraceCredits}
	s.mu.Unlock()
}

// EndSubscriberSession settles grace overage into carried debt for the next period.
func (s *Service) EndSubscriberSession(userID string) {
	s.mu.Lock()
	sess, ok := s.sessions[userID]
	delete(s.sessions, userID)
	s.mu.Unlock()
	if !ok || sess == nil {
		return
	}
	row, err := getRow(s.db, userID)
	if err != nil {
		return
	}
	if row.PeriodUsedCredits > row.PeriodAllowanceCredits {
		over := row.PeriodUsedCredits - row.PeriodAllowanceCredits
		if over > sess.usedCredits {
			over = sess.usedCredits
		}
		row.OverageDebtCredits += over
		_ = updateRow(s.db, row)
	}
}

// ApplySubscriptionPeriod resets usage for a new Stripe billing period.
func (s *Service) ApplySubscriptionPeriod(userID, plan, customerID, subID string, start, end int64, allowanceCredits int64) error {
	row, err := s.getOrCreateRow(userID)
	if err != nil {
		return err
	}
	if row.PeriodUsedCredits > row.PeriodAllowanceCredits {
		row.OverageDebtCredits += row.PeriodUsedCredits - row.PeriodAllowanceCredits
	}
	row.Plan = plan
	row.PeriodAllowanceCredits = allowanceCredits
	row.PeriodUsedCredits = 0
	row.TopUpCreditsRemaining = 0
	row.TrialRemainingCredits = 0
	row.TrialEndsAt = nil
	if customerID != "" {
		row.StripeCustomerID = customerID
	}
	if subID != "" {
		row.StripeSubscriptionID = subID
	}
	if start > 0 {
		t := unixUTC(start)
		row.PeriodStart = &t
	}
	if end > 0 {
		t := unixUTC(end)
		row.PeriodEnd = &t
	}
	return updateRow(s.db, row)
}

// AddTopUpCredits adds one-time credits (expire at current period end — cleared on renewal).
func (s *Service) AddTopUpCredits(userID string, credits int64) error {
	if credits <= 0 {
		return nil
	}
	row, err := s.getOrCreateRow(userID)
	if err != nil {
		return err
	}
	row.TopUpCreditsRemaining += credits
	return updateRow(s.db, row)
}

func unixUTC(sec int64) time.Time {
	return time.Unix(sec, 0).UTC()
}

func (s *Service) RowByStripeCustomer(customerID string) (domain.UserQuotaRow, error) {
	return rowByStripeCustomer(s.db, customerID)
}

func (s *Service) RowForUser(userID string) (domain.UserQuotaRow, error) {
	return s.getOrCreateRow(userID)
}

func (s *Service) SetEnforcement(userID string, enabled bool) error {
	row, err := s.getOrCreateRow(userID)
	if err != nil {
		return err
	}
	row.EnforcementEnabled = enabled
	return updateRow(s.db, row)
}

func (s *Service) LoadDefaults() domain.QuotaDefaults {
	return loadDefaults(s.cfg)
}

func (s *Service) SaveDefaults(d domain.QuotaDefaults) error {
	return saveDefaults(s.cfg, d)
}

func (s *Service) SaveUserOverrides(userID string, o domain.QuotaUserOverrides) error {
	return saveOverrides(s.cfg, userID, o)
}

func (s *Service) UserOverrides(userID string) domain.QuotaUserOverrides {
	return loadOverrides(s.cfg, userID)
}

func (s *Service) SetStripeCustomer(userID, customerID string) error {
	row, err := s.getOrCreateRow(userID)
	if err != nil {
		return err
	}
	row.StripeCustomerID = customerID
	return updateRow(s.db, row)
}
