package quota

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/user/jobifai/internal/domain"
)

// StripeSubscriptionUpdate applies Stripe subscription state to user_quota.
type StripeSubscriptionUpdate struct {
	UserID, EventID, CustomerID, SubscriptionID string
	Plan, PriceID, Status                       string
	CancelAtPeriodEnd                           bool
	PeriodStartUnix, PeriodEndUnix              int64
	AllowanceCredits                            int64
	EventCreatedUnix                            int64
	MetadataOnly                                bool
	Terminate                                   bool
}

// SubscriptionAllowsUsage is true when paid plan + Stripe status permit LLM usage.
func SubscriptionAllowsUsage(row domain.UserQuotaRow) bool {
	switch row.Plan {
	case domain.QuotaPlanStarter, domain.QuotaPlanPro:
		return subscriptionActiveStatus(row.StripeSubscriptionStatus)
	default:
		return false
	}
}

// ApplyStripeSubscription updates entitlement; resets monthly usage only on a new billing period.
func (s *Service) ApplyStripeSubscription(in StripeSubscriptionUpdate) error {
	row, err := s.getOrCreateRow(in.UserID)
	if err != nil {
		return err
	}
	if in.EventCreatedUnix > 0 && row.LastStripeStateEventCreatedAt > in.EventCreatedUnix {
		return nil
	}
	if in.EventCreatedUnix > 0 && row.LastStripeStateEventCreatedAt == in.EventCreatedUnix {
		if row.Plan == domain.QuotaPlanExpired && !in.Terminate {
			return nil
		}
	}
	if in.CustomerID != "" {
		row.StripeCustomerID = in.CustomerID
	}
	row.LastStripeEventID = in.EventID
	row.SubscriptionUpdatedAt = quotaTimePtr(time.Now().UTC())
	row.StripeSubscriptionStatus = in.Status
	row.CancelAtPeriodEnd = in.CancelAtPeriodEnd
	if in.PriceID != "" {
		row.StripePriceID = in.PriceID
	}

	if in.Terminate {
		if err := s.terminateSubscription(row, in.EventCreatedUnix); err != nil {
			return err
		}
		return nil
	}

	if in.MetadataOnly {
		if in.SubscriptionID != "" {
			row.StripeSubscriptionID = in.SubscriptionID
		}
		return updateRow(s.db, row)
	}

	if in.Plan == "" {
		return nil
	}

	prevStart := int64(0)
	if row.PeriodStart != nil {
		prevStart = row.PeriodStart.Unix()
	}
	newPeriod := in.PeriodStartUnix > 0 && in.PeriodStartUnix > prevStart
	oldPlan := row.Plan
	planChanged := oldPlan != in.Plan && oldPlan != domain.QuotaPlanTrial

	if in.SubscriptionID != "" {
		row.StripeSubscriptionID = in.SubscriptionID
	}

	if !subscriptionActiveStatus(in.Status) {
		if in.CancelAtPeriodEnd && in.PeriodEndUnix > 0 {
			row.PeriodEnd = quotaTimePtr(unixUTC(in.PeriodEndUnix))
			row.Plan = in.Plan
			row.PeriodAllowanceCredits = in.AllowanceCredits
			if in.EventCreatedUnix > 0 {
				row.LastStripeStateEventCreatedAt = in.EventCreatedUnix
			}
			return updateRow(s.db, row)
		}
		if err := s.terminateSubscription(row, in.EventCreatedUnix); err != nil {
			return err
		}
		return nil
	}

	row.Plan = in.Plan
	row.TrialRemainingCredits = 0
	row.TrialEndsAt = nil

	if newPeriod {
		if row.PeriodUsedCredits > row.PeriodAllowanceCredits {
			row.OverageDebtCredits += row.PeriodUsedCredits - row.PeriodAllowanceCredits
		}
		row.PeriodUsedCredits = 0
		row.TopUpCreditsRemaining = 0
		row.PeriodAllowanceCredits = in.AllowanceCredits
	} else if planChanged {
		if !isPlanDowngrade(oldPlan, in.Plan) {
			row.PeriodAllowanceCredits = in.AllowanceCredits
		}
	}

	if in.PeriodStartUnix > 0 {
		row.PeriodStart = quotaTimePtr(unixUTC(in.PeriodStartUnix))
	}
	if in.PeriodEndUnix > 0 {
		row.PeriodEnd = quotaTimePtr(unixUTC(in.PeriodEndUnix))
	}
	if in.EventCreatedUnix > 0 {
		row.LastStripeStateEventCreatedAt = in.EventCreatedUnix
	}
	return updateRow(s.db, row)
}

func isPlanDowngrade(from, to string) bool {
	return from == domain.QuotaPlanPro && to == domain.QuotaPlanStarter
}

// RepairReconcileAllowance raises under-allocated period allowance to the configured plan
// grant without resetting usage, top-ups, or starting a new billing period. If local
// allowance is already at or above the configured grant (e.g. same-period Pro→Starter),
// it is preserved.
func (s *Service) RepairReconcileAllowance(userID, plan string) error {
	if plan != domain.QuotaPlanStarter && plan != domain.QuotaPlanPro {
		return nil
	}
	configured := AllowanceCreditsForPlan(s.LoadDefaults(), plan)
	if configured <= 0 {
		return nil
	}
	row, err := s.getOrCreateRow(userID)
	if err != nil {
		return err
	}
	if row.PeriodAllowanceCredits >= configured {
		return nil
	}
	row.PeriodAllowanceCredits = configured
	return updateRow(s.db, row)
}

func subscriptionActiveStatus(status string) bool {
	switch status {
	case "active", "trialing", "past_due":
		return true
	default:
		return false
	}
}

func (s *Service) terminateSubscription(row domain.UserQuotaRow, eventCreatedUnix int64) error {
	row.Plan = domain.QuotaPlanExpired
	row.StripeSubscriptionID = ""
	row.PeriodAllowanceCredits = 0
	row.PeriodUsedCredits = 0
	row.TopUpCreditsRemaining = 0
	row.StripeSubscriptionStatus = "canceled"
	row.CancelAtPeriodEnd = false
	if eventCreatedUnix > 0 {
		row.LastStripeStateEventCreatedAt = eventCreatedUnix
	}
	return updateRow(s.db, row)
}

// GrantTopUpOnce credits a top-up checkout exactly once per Stripe event/session.
func (s *Service) GrantTopUpOnce(userID, eventID, checkoutSessionID string, credits int64) error {
	if credits <= 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.Exec(`
		INSERT INTO stripe_credit_grants (id, grant_type, stripe_event_id, checkout_session_id, user_id, credits)
		VALUES (?, 'topup', ?, ?, ?, ?)`,
		uuid.NewString(), eventID, nullEmpty(checkoutSessionID), userID, credits)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil
		}
		return err
	}
	row, err := getRowTx(tx, userID)
	if err != nil {
		return err
	}
	row.TopUpCreditsRemaining += credits
	if err := updateRowTx(tx, row); err != nil {
		return err
	}
	return tx.Commit()
}

func quotaTimePtr(t time.Time) *time.Time { return &t }

func nullEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

