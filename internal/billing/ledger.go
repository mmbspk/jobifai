package billing

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const (
	EventStatusReceived  = "received"
	EventStatusProcessed = "processed"
	EventStatusIgnored   = "ignored"
	EventStatusFailed    = "failed"
)

var ErrAlreadyProcessed = errors.New("stripe event already processed")

type WebhookEventRow struct {
	EventID              string
	EventType            string
	Status               string
	UserID               string
	StripeCustomerID     string
	StripeSubscriptionID string
	CheckoutSessionID    string
	AttemptCount         int
	ErrorCode            string
	ErrorMessage         string
	ReceivedAt           time.Time
	ProcessedAt          *time.Time
}

// BeginWebhookEvent records delivery; returns ErrAlreadyProcessed when safe to ack without work.
func BeginWebhookEvent(ctx context.Context, db *sql.DB, eventID, eventType string, stripeCreated int64, meta WebhookEventMeta) (WebhookEventRow, error) {
	var row WebhookEventRow
	var processedAt sql.NullTime
	err := db.QueryRowContext(ctx, `SELECT event_id, event_type, status, COALESCE(user_id,''),
		COALESCE(stripe_customer_id,''), COALESCE(stripe_subscription_id,''), COALESCE(checkout_session_id,''),
		attempt_count, COALESCE(error_code,''), COALESCE(error_message,''), received_at, processed_at
		FROM stripe_webhook_events WHERE event_id = ?`, eventID).Scan(
		&row.EventID, &row.EventType, &row.Status, &row.UserID,
		&row.StripeCustomerID, &row.StripeSubscriptionID, &row.CheckoutSessionID,
		&row.AttemptCount, &row.ErrorCode, &row.ErrorMessage, &row.ReceivedAt, &processedAt,
	)
	if err == nil {
		_, _ = db.ExecContext(ctx, `UPDATE stripe_webhook_events SET attempt_count = attempt_count + 1 WHERE event_id = ?`, eventID)
		row.AttemptCount++
		if processedAt.Valid {
			t := processedAt.Time
			row.ProcessedAt = &t
		}
		if row.Status == EventStatusProcessed || row.Status == EventStatusIgnored {
			return row, ErrAlreadyProcessed
		}
		return row, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return row, err
	}
	var createdAt any
	if stripeCreated > 0 {
		createdAt = time.Unix(stripeCreated, 0).UTC()
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO stripe_webhook_events (
			event_id, event_type, stripe_created_at, status, attempt_count,
			stripe_customer_id, stripe_subscription_id, checkout_session_id, user_id, metadata_json
		) VALUES (?, ?, ?, ?, 1, ?, ?, ?, ?, '{}')`,
		eventID, eventType, createdAt, EventStatusReceived,
		nullStr(meta.CustomerID), nullStr(meta.SubscriptionID), nullStr(meta.CheckoutSessionID), nullStr(meta.UserID),
	)
	if err != nil {
		return row, err
	}
	row = WebhookEventRow{EventID: eventID, EventType: eventType, Status: EventStatusReceived, AttemptCount: 1,
		UserID: meta.UserID, StripeCustomerID: meta.CustomerID, StripeSubscriptionID: meta.SubscriptionID,
		CheckoutSessionID: meta.CheckoutSessionID,
	}
	return row, nil
}

type WebhookEventMeta struct {
	UserID, CustomerID, SubscriptionID, CheckoutSessionID string
}

func FinishWebhookEvent(ctx context.Context, db *sql.DB, eventID, status, errCode, errMsg string) error {
	_, err := db.ExecContext(ctx, `
		UPDATE stripe_webhook_events SET status = ?, processed_at = CURRENT_TIMESTAMP,
			error_code = ?, error_message = ?
		WHERE event_id = ?`, status, nullStr(errCode), nullStr(errMsg), eventID)
	return err
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
