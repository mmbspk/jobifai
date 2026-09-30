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
	MetadataOnly                                bool
	Terminate                                   bool
}

// ApplyStripeSubscription updates entitlement; resets monthly usage only on a new billing period.
func (s *Service) ApplyStripeSubscription(in StripeSubscriptionUpdate) error {
	row, err := s.getOrCreateRow(in.UserID)
	if err != nil {
		return err
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
		return s.terminateSubscription(row)
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
	planChanged := row.Plan != in.Plan && row.Plan != domain.QuotaPlanTrial

	if in.SubscriptionID != "" {
		row.StripeSubscriptionID = in.SubscriptionID
	}

	if !subscriptionActiveStatus(in.Status) {
		if in.CancelAtPeriodEnd && in.PeriodEndUnix > 0 {
			row.PeriodEnd = quotaTimePtr(unixUTC(in.PeriodEndUnix))
			row.Plan = in.Plan
			row.PeriodAllowanceCredits = in.AllowanceCredits
			return updateRow(s.db, row)
		}
		return s.terminateSubscription(row)
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
		row.PeriodAllowanceCredits = in.AllowanceCredits
	}

	if in.PeriodStartUnix > 0 {
		row.PeriodStart = quotaTimePtr(unixUTC(in.PeriodStartUnix))
	}
	if in.PeriodEndUnix > 0 {
		row.PeriodEnd = quotaTimePtr(unixUTC(in.PeriodEndUnix))
	}
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

func (s *Service) terminateSubscription(row domain.UserQuotaRow) error {
	row.Plan = domain.QuotaPlanExpired
	row.StripeSubscriptionID = ""
	row.PeriodAllowanceCredits = 0
	row.StripeSubscriptionStatus = "canceled"
	row.CancelAtPeriodEnd = false
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

