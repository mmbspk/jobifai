package billing

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

const (
	EventStatusReceived  = "received"
	EventStatusProcessing = "processing"
	EventStatusProcessed = "processed"
	EventStatusIgnored   = "ignored"
	EventStatusFailed    = "failed"
)

var (
	ErrAlreadyProcessed = errors.New("stripe event already processed")
	ErrEventClaimLost   = errors.New("stripe event claim lost to concurrent worker")
)

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

type WebhookEventMeta struct {
	UserID, CustomerID, SubscriptionID, CheckoutSessionID string
}

// ClaimWebhookEvent atomically records or re-claims a webhook delivery before side effects.
func ClaimWebhookEvent(ctx context.Context, db *sql.DB, eventID, eventType string, stripeCreated int64, meta WebhookEventMeta) (WebhookEventRow, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return WebhookEventRow{}, err
	}
	defer func() { _ = tx.Rollback() }()

	row, err := loadWebhookEvent(ctx, tx, eventID)
	if errors.Is(err, sql.ErrNoRows) {
		var createdAt any
		if stripeCreated > 0 {
			createdAt = time.Unix(stripeCreated, 0).UTC()
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO stripe_webhook_events (
				event_id, event_type, stripe_created_at, status, attempt_count,
				stripe_customer_id, stripe_subscription_id, checkout_session_id, user_id, metadata_json
			) VALUES (?, ?, ?, ?, 1, ?, ?, ?, ?, '{}')`,
			eventID, eventType, createdAt, EventStatusProcessing,
			nullStr(meta.CustomerID), nullStr(meta.SubscriptionID), nullStr(meta.CheckoutSessionID), nullStr(meta.UserID),
		)
		if err != nil {
			if isUniqueViolation(err) {
				if commitErr := tx.Commit(); commitErr != nil {
					return WebhookEventRow{}, commitErr
				}
				return ClaimWebhookEvent(ctx, db, eventID, eventType, stripeCreated, meta)
			}
			return WebhookEventRow{}, err
		}
		if err := tx.Commit(); err != nil {
			return WebhookEventRow{}, err
		}
		return WebhookEventRow{EventID: eventID, EventType: eventType, Status: EventStatusProcessing, AttemptCount: 1,
			UserID: meta.UserID, StripeCustomerID: meta.CustomerID, StripeSubscriptionID: meta.SubscriptionID,
			CheckoutSessionID: meta.CheckoutSessionID,
		}, nil
	}
	if err != nil {
		return WebhookEventRow{}, err
	}

	switch row.Status {
	case EventStatusProcessed, EventStatusIgnored:
		return row, ErrAlreadyProcessed
	case EventStatusProcessing:
		return row, ErrEventClaimLost
	}

	res, err := tx.ExecContext(ctx, `
		UPDATE stripe_webhook_events
		SET status = ?, attempt_count = attempt_count + 1
		WHERE event_id = ? AND status IN (?, ?)`,
		EventStatusProcessing, eventID, EventStatusReceived, EventStatusFailed,
	)
	if err != nil {
		return WebhookEventRow{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		row2, loadErr := loadWebhookEvent(ctx, tx, eventID)
		if loadErr != nil {
			return WebhookEventRow{}, loadErr
		}
		if row2.Status == EventStatusProcessed || row2.Status == EventStatusIgnored {
			return row2, ErrAlreadyProcessed
		}
		return row2, ErrEventClaimLost
	}
	if err := tx.Commit(); err != nil {
		return WebhookEventRow{}, err
	}
	row.Status = EventStatusProcessing
	row.AttemptCount++
	return row, nil
}

func loadWebhookEvent(ctx context.Context, q sqlQuerier, eventID string) (WebhookEventRow, error) {
	var row WebhookEventRow
	var processedAt sql.NullTime
	err := q.QueryRowContext(ctx, `SELECT event_id, event_type, status, COALESCE(user_id,''),
		COALESCE(stripe_customer_id,''), COALESCE(stripe_subscription_id,''), COALESCE(checkout_session_id,''),
		attempt_count, COALESCE(error_code,''), COALESCE(error_message,''), received_at, processed_at
		FROM stripe_webhook_events WHERE event_id = ?`, eventID).Scan(
		&row.EventID, &row.EventType, &row.Status, &row.UserID,
		&row.StripeCustomerID, &row.StripeSubscriptionID, &row.CheckoutSessionID,
		&row.AttemptCount, &row.ErrorCode, &row.ErrorMessage, &row.ReceivedAt, &processedAt,
	)
	if processedAt.Valid {
		t := processedAt.Time
		row.ProcessedAt = &t
	}
	return row, err
}

type sqlQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func FinishWebhookEvent(ctx context.Context, db *sql.DB, eventID, status, errCode, errMsg string) error {
	_, err := db.ExecContext(ctx, `
		UPDATE stripe_webhook_events SET status = ?, processed_at = CURRENT_TIMESTAMP,
			error_code = ?, error_message = ?
		WHERE event_id = ?`, status, nullStr(errCode), nullStr(errMsg), eventID)
	return err
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE") || strings.Contains(msg, "constraint failed")
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
