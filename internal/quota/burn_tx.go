package quota

import (
	"database/sql"
	"fmt"

	"github.com/user/jobifai/internal/domain"
)

// BurnPrepare holds quota state resolved before the billing SQL transaction starts.
// It must not trigger queries on the parent *sql.DB during CommitTransactionalBurn.
type BurnPrepare struct {
	SkipBurn            bool
	StrictBalance       bool  // if true, CommitTransactionalBurn returns ErrExceeded instead of clamping
	InGrace             bool  // user had an active grace session at prepare time
	GraceAllowedCredits int64 // snapshot of def.SubscriberGraceCredits at prepare time
	Overrides           domain.QuotaUserOverrides
}

// PrepareTransactionalBurn resolves admin status and per-user overrides outside an open transaction.
func (s *Service) PrepareTransactionalBurn(userID string, credits int64) (BurnPrepare, error) {
	if credits <= 0 || s.isAdmin(userID) {
		return BurnPrepare{SkipBurn: true}, nil
	}
	return BurnPrepare{Overrides: loadOverrides(s.cfg, userID)}, nil
}

// PrepareTransactionalBurnStrict is like PrepareTransactionalBurn but sets StrictBalance=true,
// causing CommitTransactionalBurn to return ErrExceeded (not silently clamp) when the balance
// is insufficient at commit time. Use for resource billing where exact accounting is required.
//
// If the user has an active subscriber grace session, InGrace and GraceAllowedCredits are set
// so CommitTransactionalBurn can allow the burn up to the authorised grace allowance.
func (s *Service) PrepareTransactionalBurnStrict(userID string, credits int64) (BurnPrepare, error) {
	prep, err := s.PrepareTransactionalBurn(userID, credits)
	if err != nil {
		return prep, err
	}
	prep.StrictBalance = true
	// Capture grace state at prepare time so CommitTransactionalBurn can respect it.
	s.mu.Lock()
	_, prep.InGrace = s.sessions[userID]
	s.mu.Unlock()
	if prep.InGrace {
		def := loadDefaults(s.cfg)
		prep.GraceAllowedCredits = def.SubscriberGraceCredits
	}
	return prep, nil
}

// CommitTransactionalBurn applies a credit burn using only the supplied transaction.
// Returns (actualBurned, error): actualBurned is 0 for admin/skip/enforcement-disabled paths.
// When prep.StrictBalance is true and the balance is insufficient, returns (0, ErrExceeded)
// instead of silently clamping — unless prep.InGrace is true (the preflight authorized a grace
// overspend), in which case the burn is allowed up to the grace allowance captured at prepare time.
func (s *Service) CommitTransactionalBurn(tx *sql.Tx, userID string, credits int64, prep BurnPrepare) (int64, error) {
	if credits <= 0 || prep.SkipBurn {
		return 0, nil
	}
	row, err := getRowTx(tx, userID)
	if err != nil {
		return 0, err
	}
	row = applyOverridesToRow(row, prep.Overrides)
	if !row.EnforcementEnabled {
		return 0, nil
	}
	graceUse := int64(0)
	if prep.StrictBalance {
		remaining := s.totalRemaining(row)
		if remaining < credits {
			if !prep.InGrace {
				return 0, fmt.Errorf("resource billing: %w", ErrExceeded)
			}
			// Grace path: check that the requested credits don't exceed the grace allowance
			// authorised at prepare time. Uses DB-row overage so concurrent TXs are serialised.
			currentOverage := max(int64(0), row.PeriodUsedCredits-row.PeriodAllowanceCredits)
			graceUse = max(int64(0), credits-remaining)
			if currentOverage+graceUse > prep.GraceAllowedCredits {
				return 0, fmt.Errorf("resource billing: %w", ErrExceeded)
			}
		}
	}
	if err := s.applyBurn(&row, credits); err != nil {
		return 0, err
	}
	if err := updateRowTx(tx, row); err != nil {
		return 0, err
	}
	// Keep the in-memory grace tracker in sync so EndSubscriberSession can settle debt.
	// Resource billing bypasses BeforeCreditUse, so sess.usedCredits must be updated here.
	if graceUse > 0 {
		s.mu.Lock()
		if sess, ok := s.sessions[userID]; ok {
			sess.usedCredits += graceUse
		}
		s.mu.Unlock()
	}
	return credits, nil
}

func applyOverridesToRow(row domain.UserQuotaRow, o domain.QuotaUserOverrides) domain.UserQuotaRow {
	if o.EnforcementEnabled != nil {
		row.EnforcementEnabled = *o.EnforcementEnabled
	}
	// TrialCredits override is an allowance cap, not the persisted remaining balance.
	if o.PeriodAllowanceCredits != nil && row.Plan != domain.QuotaPlanTrial {
		row.PeriodAllowanceCredits = *o.PeriodAllowanceCredits
	}
	return row
}
