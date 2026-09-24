package quota

import (
	"database/sql"
	"errors"
	"time"

	"github.com/user/jobifai/internal/domain"
)

func getRow(db *sql.DB, userID string) (domain.UserQuotaRow, error) {
	var row domain.UserQuotaRow
	var periodStart, periodEnd, trialEnds sql.NullTime
	var enforce int
	err := db.QueryRow(`
		SELECT user_id, plan, enforcement_enabled, trial_remaining_micro,
		       period_allowance_micro, period_used_micro, period_start_at, period_end_at,
		       overage_debt_micro, COALESCE(stripe_customer_id,''), COALESCE(stripe_subscription_id,''),
		       trial_ends_at, topup_credits_remaining
		FROM user_quota WHERE user_id = ?`, userID).Scan(
		&row.UserID, &row.Plan, &enforce, &row.TrialRemainingCredits,
		&row.PeriodAllowanceCredits, &row.PeriodUsedCredits, &periodStart, &periodEnd,
		&row.OverageDebtCredits, &row.StripeCustomerID, &row.StripeSubscriptionID,
		&trialEnds, &row.TopUpCreditsRemaining,
	)
	if err != nil {
		return domain.UserQuotaRow{}, err
	}
	row.EnforcementEnabled = enforce != 0
	if periodStart.Valid {
		t := periodStart.Time
		row.PeriodStart = &t
	}
	if periodEnd.Valid {
		t := periodEnd.Time
		row.PeriodEnd = &t
	}
	if trialEnds.Valid {
		t := trialEnds.Time
		row.TrialEndsAt = &t
	}
	return row, nil
}

func insertTrialRow(db *sql.DB, userID string, trialCredits int64, trialEnds time.Time, enforce bool) error {
	en := 0
	if enforce {
		en = 1
	}
	_, err := db.Exec(`
		INSERT INTO user_quota (user_id, plan, enforcement_enabled, trial_remaining_micro, trial_ends_at, updated_at)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		userID, domain.QuotaPlanTrial, en, trialCredits, trialEnds)
	return err
}

func updateRow(db *sql.DB, row domain.UserQuotaRow) error {
	en := 0
	if row.EnforcementEnabled {
		en = 1
	}
	var ps, pe, te any
	if row.PeriodStart != nil {
		ps = *row.PeriodStart
	}
	if row.PeriodEnd != nil {
		pe = *row.PeriodEnd
	}
	if row.TrialEndsAt != nil {
		te = *row.TrialEndsAt
	}
	_, err := db.Exec(`
		UPDATE user_quota SET
			plan = ?, enforcement_enabled = ?, trial_remaining_micro = ?,
			period_allowance_micro = ?, period_used_micro = ?,
			period_start_at = ?, period_end_at = ?,
			overage_debt_micro = ?,
			stripe_customer_id = ?, stripe_subscription_id = ?,
			trial_ends_at = ?, topup_credits_remaining = ?,
			updated_at = CURRENT_TIMESTAMP
		WHERE user_id = ?`,
		row.Plan, en, row.TrialRemainingCredits,
		row.PeriodAllowanceCredits, row.PeriodUsedCredits,
		ps, pe, row.OverageDebtCredits,
		nullIfEmpty(row.StripeCustomerID), nullIfEmpty(row.StripeSubscriptionID),
		te, row.TopUpCreditsRemaining,
		row.UserID)
	return err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func rowByStripeCustomer(db *sql.DB, customerID string) (domain.UserQuotaRow, error) {
	var row domain.UserQuotaRow
	var periodStart, periodEnd, trialEnds sql.NullTime
	var enforce int
	err := db.QueryRow(`
		SELECT user_id, plan, enforcement_enabled, trial_remaining_micro,
		       period_allowance_micro, period_used_micro, period_start_at, period_end_at,
		       overage_debt_micro, COALESCE(stripe_customer_id,''), COALESCE(stripe_subscription_id,''),
		       trial_ends_at, topup_credits_remaining
		FROM user_quota WHERE stripe_customer_id = ?`, customerID).Scan(
		&row.UserID, &row.Plan, &enforce, &row.TrialRemainingCredits,
		&row.PeriodAllowanceCredits, &row.PeriodUsedCredits, &periodStart, &periodEnd,
		&row.OverageDebtCredits, &row.StripeCustomerID, &row.StripeSubscriptionID,
		&trialEnds, &row.TopUpCreditsRemaining,
	)
	if err != nil {
		return domain.UserQuotaRow{}, err
	}
	row.EnforcementEnabled = enforce != 0
	if periodStart.Valid {
		t := periodStart.Time
		row.PeriodStart = &t
	}
	if periodEnd.Valid {
		t := periodEnd.Time
		row.PeriodEnd = &t
	}
	if trialEnds.Valid {
		t := trialEnds.Time
		row.TrialEndsAt = &t
	}
	return row, nil
}

func isNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

func rfc3339(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}
