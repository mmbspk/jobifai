package billing

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/stripe/stripe-go/v82"
)

// ExtractWebhookEventMeta pulls safe attribution fields from a Stripe event payload.
func ExtractWebhookEventMeta(event stripe.Event, q QuotaSync) WebhookEventMeta {
	switch event.Type {
	case stripe.EventTypeCheckoutSessionCompleted:
		var sess stripe.CheckoutSession
		if err := json.Unmarshal(event.Data.Raw, &sess); err != nil {
			return WebhookEventMeta{}
		}
		return WebhookEventMeta{
			UserID:            resolveUserID(q, sess.Metadata, customerID(&sess)),
			CustomerID:        customerID(&sess),
			CheckoutSessionID: sess.ID,
		}
	case stripe.EventTypeCustomerSubscriptionCreated, stripe.EventTypeCustomerSubscriptionUpdated,
		stripe.EventTypeCustomerSubscriptionDeleted:
		var sub stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
			return WebhookEventMeta{}
		}
		return WebhookEventMeta{
			UserID:         resolveUserID(q, sub.Metadata, customerIDSub(&sub)),
			CustomerID:     customerIDSub(&sub),
			SubscriptionID: sub.ID,
		}
	case stripe.EventTypeInvoicePaid, stripe.EventTypeInvoicePaymentFailed:
		var inv stripe.Invoice
		if err := json.Unmarshal(event.Data.Raw, &inv); err != nil {
			return WebhookEventMeta{}
		}
		cust := customerIDInv(&inv)
		meta := WebhookEventMeta{
			CustomerID:     cust,
			SubscriptionID: invoiceSubscriptionID(&inv),
		}
		if cust != "" {
			if row, err := q.RowByStripeCustomer(cust); err == nil {
				meta.UserID = row.UserID
			}
		}
		return meta
	default:
		return WebhookEventMeta{}
	}
}

func customerIDInv(inv *stripe.Invoice) string {
	if inv.Customer != nil {
		return inv.Customer.ID
	}
	return ""
}

// UpdateWebhookEventMeta fills attribution columns when resolved during processing.
func UpdateWebhookEventMeta(ctx context.Context, db *sql.DB, eventID string, meta WebhookEventMeta) error {
	if eventID == "" {
		return nil
	}
	_, err := db.ExecContext(ctx, `
		UPDATE stripe_webhook_events SET
			user_id = CASE WHEN ? != '' THEN ? ELSE user_id END,
			stripe_customer_id = CASE WHEN ? != '' THEN ? ELSE stripe_customer_id END,
			stripe_subscription_id = CASE WHEN ? != '' THEN ? ELSE stripe_subscription_id END,
			checkout_session_id = CASE WHEN ? != '' THEN ? ELSE checkout_session_id END
		WHERE event_id = ?`,
		meta.UserID, meta.UserID,
		meta.CustomerID, meta.CustomerID,
		meta.SubscriptionID, meta.SubscriptionID,
		meta.CheckoutSessionID, meta.CheckoutSessionID,
		eventID,
	)
	return err
}
