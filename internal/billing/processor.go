package billing

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/rs/zerolog/log"
	"github.com/stripe/stripe-go/v86"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/quota"
)

// QuotaSync applies Stripe-derived entitlement changes.
type QuotaSync interface {
	LoadDefaults() domain.QuotaDefaults
	RowForUser(userID string) (domain.UserQuotaRow, error)
	RowByStripeCustomer(customerID string) (domain.UserQuotaRow, error)
	SetStripeCustomer(userID, customerID string) error
	ApplyStripeSubscription(in quota.StripeSubscriptionUpdate) error
	RepairReconcileAllowance(userID, plan string) error
	GrantTopUpOnce(userID, eventID, checkoutSessionID string, credits int64) error
}

// Processor handles Stripe webhook side effects with idempotency.
type Processor struct {
	DB    *sql.DB
	Quota QuotaSync
}

func (p *Processor) ProcessEvent(ctx context.Context, event stripe.Event) error {
	meta := ExtractWebhookEventMeta(event, p.Quota)
	_, err := ClaimWebhookEvent(ctx, p.DB, event.ID, string(event.Type), event.Created, meta)
	if errors.Is(err, ErrAlreadyProcessed) {
		return nil
	}
	if errors.Is(err, ErrEventClaimLost) {
		return err
	}
	if err != nil {
		return err
	}

	var procErr error
	switch event.Type {
	case stripe.EventTypeCheckoutSessionCompleted:
		procErr = p.handleCheckoutCompleted(event)
	case stripe.EventTypeCustomerSubscriptionCreated, stripe.EventTypeCustomerSubscriptionUpdated:
		procErr = p.handleSubscription(event)
	case stripe.EventTypeCustomerSubscriptionDeleted:
		procErr = p.handleSubscriptionDeleted(ctx, event)
	case stripe.EventTypeInvoicePaid:
		procErr = p.handleInvoicePaid(event)
	case stripe.EventTypeInvoicePaymentFailed:
		procErr = p.handleInvoicePaymentFailed(event)
	default:
		_ = UpdateWebhookEventMeta(ctx, p.DB, event.ID, meta)
		return FinishWebhookEvent(ctx, p.DB, event.ID, EventStatusIgnored, "", "")
	}

	if procErr != nil {
		if code, msg, ok := asIgnoredEvent(procErr); ok {
			_ = UpdateWebhookEventMeta(ctx, p.DB, event.ID, meta)
			return FinishWebhookEvent(ctx, p.DB, event.ID, EventStatusIgnored, code, msg)
		}
		errCode := "processing_error"
		if errors.Is(procErr, errUnknownPrice) {
			errCode = "unknown_price"
		}
		_ = FinishWebhookEvent(ctx, p.DB, event.ID, EventStatusFailed, errCode, procErr.Error())
		log.Error().Err(procErr).Str("event_id", event.ID).Str("type", string(event.Type)).Msg("stripe webhook failed")
		return procErr
	}
	_ = UpdateWebhookEventMeta(ctx, p.DB, event.ID, ExtractWebhookEventMeta(event, p.Quota))
	return FinishWebhookEvent(ctx, p.DB, event.ID, EventStatusProcessed, "", "")
}

var errUnknownPrice = errors.New("unknown stripe price id")

func (p *Processor) handleCheckoutCompleted(event stripe.Event) error {
	var sess stripe.CheckoutSession
	if err := json.Unmarshal(event.Data.Raw, &sess); err != nil {
		return err
	}
	if sess.Mode == stripe.CheckoutSessionModeSubscription {
		return nil
	}
	if sess.PaymentStatus != stripe.CheckoutSessionPaymentStatusPaid {
		return errEventIgnored("not_paid", "checkout session not paid")
	}
	userID := resolveUserID(p.Quota, sess.Metadata, customerID(&sess))
	if userID == "" {
		return errors.New("checkout: no user mapping")
	}
	if sess.Customer != nil && sess.Customer.ID != "" {
		_ = p.Quota.SetStripeCustomer(userID, sess.Customer.ID)
	}
	if sess.Metadata["jobifai_topup"] != "1" {
		return nil
	}
	credits, _ := strconv.ParseInt(sess.Metadata["jobifai_credits"], 10, 64)
	if credits <= 0 {
		return errors.New("checkout: invalid top-up credits")
	}
	return p.Quota.GrantTopUpOnce(userID, event.ID, sess.ID, credits)
}

func (p *Processor) handleSubscription(event stripe.Event) error {
	var sub stripe.Subscription
	if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
		return err
	}
	return p.applySubscriptionEvent(event, sub)
}

func (p *Processor) handleSubscriptionDeleted(_ context.Context, event stripe.Event) error {
	var sub stripe.Subscription
	if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
		return err
	}
	userID := resolveUserID(p.Quota, sub.Metadata, customerIDSub(&sub))
	if userID == "" {
		return errors.New("subscription deleted: no user")
	}
	up := quota.StripeSubscriptionUpdate{
		UserID: userID, EventID: event.ID, EventCreatedUnix: event.Created,
		CustomerID: customerIDSub(&sub), SubscriptionID: sub.ID,
		Status: string(stripe.SubscriptionStatusCanceled), CancelAtPeriodEnd: false,
		Terminate: true,
	}
	return p.Quota.ApplyStripeSubscription(up)
}

func (p *Processor) handleInvoicePaid(event stripe.Event) error {
	subID, err := p.invoiceSubscriptionID(event)
	if err != nil {
		return err
	}
	if subID == "" {
		return nil
	}
	return p.reconcileSubscriptionFromStripe(event, subID)
}

func (p *Processor) handleInvoicePaymentFailed(event stripe.Event) error {
	subID, err := p.invoiceSubscriptionID(event)
	if err != nil {
		return err
	}
	if subID == "" {
		return nil
	}
	return p.reconcileSubscriptionFromStripe(event, subID)
}

func (p *Processor) invoiceSubscriptionID(event stripe.Event) (string, error) {
	var inv stripe.Invoice
	if err := json.Unmarshal(event.Data.Raw, &inv); err != nil {
		return "", err
	}
	return invoiceSubscriptionID(&inv), nil
}

func (p *Processor) reconcileSubscriptionFromStripe(event stripe.Event, subID string) error {
	sub, err := fetchSubscription(subID)
	if err != nil {
		return err
	}
	return p.applySubscriptionEvent(event, sub)
}

func (p *Processor) applySubscriptionEvent(event stripe.Event, sub stripe.Subscription) error {
	userID := resolveUserID(p.Quota, sub.Metadata, customerIDSub(&sub))
	if userID == "" {
		return errors.New("subscription: no user mapping")
	}
	priceID, periodStart, periodEnd := subscriptionPeriod(sub)
	plan, err := planFromPrice(p.Quota.LoadDefaults(), priceID)
	if err != nil {
		return err
	}
	allowance := quota.AllowanceCreditsForPlan(p.Quota.LoadDefaults(), plan)
	up := quota.StripeSubscriptionUpdate{
		UserID: userID, EventID: event.ID, EventCreatedUnix: event.Created,
		CustomerID: customerIDSub(&sub), SubscriptionID: sub.ID,
		Plan: plan, PriceID: priceID, Status: string(sub.Status),
		CancelAtPeriodEnd: sub.CancelAtPeriodEnd,
		PeriodStartUnix: periodStart, PeriodEndUnix: periodEnd,
		AllowanceCredits: allowance,
	}
	if subscriptionGrantsAccess(sub.Status) {
		return p.Quota.ApplyStripeSubscription(up)
	}
	if sub.CancelAtPeriodEnd && periodEnd > 0 &&
		(sub.Status == stripe.SubscriptionStatusActive || sub.Status == stripe.SubscriptionStatusTrialing) {
		return p.Quota.ApplyStripeSubscription(up)
	}
	up.Terminate = true
	return p.Quota.ApplyStripeSubscription(up)
}

func subscriptionGrantsAccess(status stripe.SubscriptionStatus) bool {
	switch status {
	case stripe.SubscriptionStatusActive, stripe.SubscriptionStatusTrialing, stripe.SubscriptionStatusPastDue:
		return true
	default:
		return false
	}
}

func subscriptionPeriod(sub stripe.Subscription) (priceID string, start, end int64) {
	if len(sub.Items.Data) > 0 {
		item := sub.Items.Data[0]
		if item.Price != nil {
			priceID = item.Price.ID
		}
		start = item.CurrentPeriodStart
		end = item.CurrentPeriodEnd
	}
	return priceID, start, end
}

func planFromPrice(def domain.QuotaDefaults, priceID string) (string, error) {
	switch priceID {
	case def.StripePriceStarter:
		return domain.QuotaPlanStarter, nil
	case def.StripePricePro:
		return domain.QuotaPlanPro, nil
	case "":
		return "", errUnknownPrice
	default:
		return "", errUnknownPrice
	}
}

func resolveUserID(q QuotaSync, meta map[string]string, customerID string) string {
	if meta != nil && meta["jobifai_user_id"] != "" {
		return meta["jobifai_user_id"]
	}
	if customerID != "" {
		if row, err := q.RowByStripeCustomer(customerID); err == nil {
			return row.UserID
		}
	}
	return ""
}

func customerID(sess *stripe.CheckoutSession) string {
	if sess.Customer != nil {
		return sess.Customer.ID
	}
	return ""
}

func customerIDSub(sub *stripe.Subscription) string {
	if sub.Customer != nil {
		return sub.Customer.ID
	}
	return ""
}

func invoiceSubscriptionID(inv *stripe.Invoice) string {
	if inv.Parent == nil || inv.Parent.SubscriptionDetails == nil || inv.Parent.SubscriptionDetails.Subscription == nil {
		return ""
	}
	return inv.Parent.SubscriptionDetails.Subscription.ID
}
