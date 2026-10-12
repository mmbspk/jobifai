package quota

import (
	"database/sql"
	"fmt"

	"github.com/user/jobifai/internal/domain"
)

// BurnPrepare holds quota state resolved before the billing SQL transaction starts.
// It must not trigger queries on the parent *sql.DB during CommitTransactionalBurn.
type BurnPrepare struct {
	SkipBurn      bool
	StrictBalance bool // if true, CommitTransactionalBurn returns ErrExceeded instead of clamping
	Overrides     domain.QuotaUserOverrides
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
func (s *Service) PrepareTransactionalBurnStrict(userID string, credits int64) (BurnPrepare, error) {
	prep, err := s.PrepareTransactionalBurn(userID, credits)
	if err != nil {
		return prep, err
	}
	prep.StrictBalance = true
	return prep, nil
}

// CommitTransactionalBurn applies a credit burn using only the supplied transaction.
// Returns (actualBurned, error): actualBurned is 0 for admin/skip/enforcement-disabled paths.
// When prep.StrictBalance is true and the balance is insufficient, returns (0, ErrExceeded)
// instead of silently clamping to zero.
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
	if prep.StrictBalance && s.totalRemaining(row) < credits {
		return 0, fmt.Errorf("resource billing: %w", ErrExceeded)
	}
	if err := s.applyBurn(&row, credits); err != nil {
		return 0, err
	}
	return credits, updateRowTx(tx, row)
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
