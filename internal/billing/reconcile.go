package billing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/subscription"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/quota"
)

// ReconcileResult describes admin reconciliation outcome.
type ReconcileResult struct {
	UserID              string `json:"user_id"`
	StripeCustomerID    string `json:"stripe_customer_id,omitempty"`
	SubscriptionID      string `json:"stripe_subscription_id,omitempty"`
	SubscriptionStatus  string `json:"stripe_subscription_status,omitempty"`
	Action              string `json:"action"`
	Message             string `json:"message,omitempty"`
}

// ReconcileUser fetches Stripe subscription state and applies it locally (recovery path).
func (p *Processor) ReconcileUser(ctx context.Context, userID string) (ReconcileResult, error) {
	res := ReconcileResult{UserID: userID}
	if !StripeConfigured() {
		return res, errors.New("stripe not configured")
	}
	stripe.Key = StripeSecretKey()

	row, err := p.Quota.RowForUser(userID)
	if err != nil {
		return res, err
	}
	if row.StripeCustomerID == "" {
		res.Action = "noop"
		res.Message = "no Stripe customer on file"
		return res, nil
	}
	res.StripeCustomerID = row.StripeCustomerID

	params := &stripe.SubscriptionListParams{
		Customer: stripe.String(row.StripeCustomerID),
		Status:   stripe.String("all"),
	}
	iter := subscription.List(params)
	var subs []*stripe.Subscription
	for iter.Next() {
		subs = append(subs, iter.Subscription())
	}
	if err := iter.Err(); err != nil {
		return res, err
	}
	sub := pickReconcileSubscription(subs)
	if sub == nil {
		if row.StripeSubscriptionID != "" || row.Plan == domain.QuotaPlanStarter || row.Plan == domain.QuotaPlanPro {
			eventID := "admin_reconcile_" + uuid.NewString()
			procErr := p.Quota.ApplyStripeSubscription(quota.StripeSubscriptionUpdate{
				UserID: userID, EventID: eventID, EventCreatedUnix: time.Now().Unix(),
				CustomerID: row.StripeCustomerID,
				Status: string(stripe.SubscriptionStatusCanceled), Terminate: true,
			})
			if procErr != nil {
				return res, procErr
			}
			res.Action = "terminated"
			res.Message = "no active Stripe subscription; local paid entitlement cleared"
			log.Info().Str("user_id", userID).Msg("billing reconcile terminated local subscription")
			return res, nil
		}
		res.Action = "noop"
		res.Message = "no Stripe subscriptions for customer"
		return res, nil
	}

	res.SubscriptionID = sub.ID
	res.SubscriptionStatus = string(sub.Status)
	priceID, _, _ := subscriptionPeriod(*sub)
	plan, planErr := planFromPrice(p.Quota.LoadDefaults(), priceID)
	if planErr != nil {
		return res, planErr
	}
	eventID := "admin_reconcile_" + uuid.NewString()
	ev := stripe.Event{ID: eventID, Created: time.Now().Unix()}
	if err := p.applySubscriptionEvent(ev, *sub); err != nil {
		return res, fmt.Errorf("apply subscription: %w", err)
	}
	if subscriptionGrantsAccess(sub.Status) {
		if err := p.Quota.RepairReconcileAllowance(userID, plan); err != nil {
			return res, fmt.Errorf("repair allowance: %w", err)
		}
	}
	res.Action = "synced"
	res.Message = "applied Stripe subscription to local quota"
	log.Info().Str("user_id", userID).Str("subscription_id", sub.ID).Str("status", string(sub.Status)).Msg("billing reconcile synced")
	return res, nil
}

func pickReconcileSubscription(subs []*stripe.Subscription) *stripe.Subscription {
	var best *stripe.Subscription
	bestRank := -1
	for _, s := range subs {
		r := subscriptionRank(s.Status)
		if r > bestRank {
			bestRank = r
			best = s
		}
	}
	return best
}

func subscriptionRank(st stripe.SubscriptionStatus) int {
	switch st {
	case stripe.SubscriptionStatusActive:
		return 5
	case stripe.SubscriptionStatusTrialing:
		return 4
	case stripe.SubscriptionStatusPastDue:
		return 3
	case stripe.SubscriptionStatusIncomplete:
		return 2
	case stripe.SubscriptionStatusUnpaid, stripe.SubscriptionStatusCanceled:
		return 1
	default:
		return 0
	}
}
