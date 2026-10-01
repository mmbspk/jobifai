package billing

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// BillingConfigStatus is the admin view of Stripe env configuration.
type BillingConfigStatus struct {
	StripeConfigured  bool `json:"stripe_configured"`
	WebhookConfigured bool `json:"webhook_configured"`
	InsecureWebhook   bool `json:"insecure_webhook_allowed"`
}

func BillingConfigStatusFromEnv() BillingConfigStatus {
	return BillingConfigStatus{
		StripeConfigured:  StripeConfigured(),
		WebhookConfigured: WebhookConfigured(),
		InsecureWebhook:   AllowInsecureWebhook(),
	}
}

type PlanCount struct {
	Plan  string `json:"plan"`
	Count int    `json:"count"`
}

type BillingSummary struct {
	Config     BillingConfigStatus `json:"config"`
	PlanCounts []PlanCount         `json:"plan_counts"`
}

func LoadBillingSummary(ctx context.Context, db *sql.DB) (BillingSummary, error) {
	out := BillingSummary{Config: BillingConfigStatusFromEnv()}
	rows, err := db.QueryContext(ctx, `SELECT plan, COUNT(*) FROM user_quota GROUP BY plan`)
	if err != nil {
		return out, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var pc PlanCount
		if err := rows.Scan(&pc.Plan, &pc.Count); err != nil {
			return out, err
		}
		out.PlanCounts = append(out.PlanCounts, pc)
	}
	return out, rows.Err()
}

type AdminUserBillingRow struct {
	UserID                   string  `json:"user_id"`
	Email                    string  `json:"email"`
	Plan                     string  `json:"plan"`
	StripeCustomerID         string  `json:"stripe_customer_id,omitempty"`
	StripeSubscriptionID     string  `json:"stripe_subscription_id,omitempty"`
	StripeSubscriptionStatus string  `json:"stripe_subscription_status,omitempty"`
	StripePriceID            string  `json:"stripe_price_id,omitempty"`
	CancelAtPeriodEnd        bool    `json:"cancel_at_period_end"`
	PeriodStart              *string `json:"period_start,omitempty"`
	PeriodEnd                *string `json:"period_end,omitempty"`
	AllowanceCredits         int64   `json:"allowance_credits"`
	PeriodUsedCredits        int64   `json:"period_used_credits"`
	TopUpCreditsRemaining    int64   `json:"topup_credits_remaining"`
	TrialRemainingCredits    int64   `json:"trial_remaining_credits"`
}

func ListUserBilling(ctx context.Context, db *sql.DB) ([]AdminUserBillingRow, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT u.id, u.email,
			COALESCE(q.plan, 'trial'), COALESCE(q.stripe_customer_id, ''), COALESCE(q.stripe_subscription_id, ''),
			COALESCE(q.stripe_subscription_status, ''), COALESCE(q.stripe_price_id, ''),
			q.cancel_at_period_end,
			q.period_start_at, q.period_end_at,
			COALESCE(q.period_allowance_micro, 0), COALESCE(q.period_used_micro, 0),
			COALESCE(q.topup_credits_remaining, 0), COALESCE(q.trial_remaining_micro, 0)
		FROM users u
		LEFT JOIN user_quota q ON q.user_id = u.id
		ORDER BY u.email ASC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []AdminUserBillingRow
	for rows.Next() {
		var r AdminUserBillingRow
		var cancel int
		var ps, pe sql.NullTime
		if err := rows.Scan(
			&r.UserID, &r.Email, &r.Plan, &r.StripeCustomerID, &r.StripeSubscriptionID,
			&r.StripeSubscriptionStatus, &r.StripePriceID, &cancel,
			&ps, &pe,
			&r.AllowanceCredits, &r.PeriodUsedCredits, &r.TopUpCreditsRemaining, &r.TrialRemainingCredits,
		); err != nil {
			return nil, err
		}
		r.CancelAtPeriodEnd = cancel != 0
		if ps.Valid {
			s := ps.Time.UTC().Format(time.RFC3339)
			r.PeriodStart = &s
		}
		if pe.Valid {
			s := pe.Time.UTC().Format(time.RFC3339)
			r.PeriodEnd = &s
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type WebhookEventListItem struct {
	EventID              string  `json:"event_id"`
	EventType            string  `json:"event_type"`
	Status               string  `json:"status"`
	AttemptCount         int     `json:"attempt_count"`
	UserID               string  `json:"user_id,omitempty"`
	StripeCustomerID     string  `json:"stripe_customer_id,omitempty"`
	StripeSubscriptionID string  `json:"stripe_subscription_id,omitempty"`
	CheckoutSessionID    string  `json:"checkout_session_id,omitempty"`
	ErrorCode            string  `json:"error_code,omitempty"`
	ErrorMessage         string  `json:"error_message,omitempty"`
	ReceivedAt           string  `json:"received_at"`
	ProcessedAt          *string `json:"processed_at,omitempty"`
}

type WebhookEventFilter struct {
	Status    string
	EventType string
	UserID    string
	Limit     int
}

func ListWebhookEvents(ctx context.Context, db *sql.DB, f WebhookEventFilter) ([]WebhookEventListItem, error) {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	var b strings.Builder
	args := []any{}
	b.WriteString(`SELECT event_id, event_type, status, attempt_count,
		COALESCE(user_id,''), COALESCE(stripe_customer_id,''), COALESCE(stripe_subscription_id,''),
		COALESCE(checkout_session_id,''), COALESCE(error_code,''), COALESCE(error_message,''),
		received_at, processed_at FROM stripe_webhook_events WHERE 1=1`)
	if f.Status != "" {
		b.WriteString(` AND status = ?`)
		args = append(args, f.Status)
	}
	if f.EventType != "" {
		b.WriteString(` AND event_type = ?`)
		args = append(args, f.EventType)
	}
	if f.UserID != "" {
		b.WriteString(` AND user_id = ?`)
		args = append(args, f.UserID)
	}
	b.WriteString(` ORDER BY received_at DESC LIMIT ?`)
	args = append(args, f.Limit)

	rows, err := db.QueryContext(ctx, b.String(), args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []WebhookEventListItem
	for rows.Next() {
		var it WebhookEventListItem
		var received time.Time
		var processed sql.NullTime
		if err := rows.Scan(
			&it.EventID, &it.EventType, &it.Status, &it.AttemptCount,
			&it.UserID, &it.StripeCustomerID, &it.StripeSubscriptionID, &it.CheckoutSessionID,
			&it.ErrorCode, &it.ErrorMessage, &received, &processed,
		); err != nil {
			return nil, err
		}
		it.ReceivedAt = received.UTC().Format(time.RFC3339)
		if processed.Valid {
			s := processed.Time.UTC().Format(time.RFC3339)
			it.ProcessedAt = &s
		}
		out = append(out, it)
	}
	return out, rows.Err()
}
