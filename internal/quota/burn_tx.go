package quota

import (
	"database/sql"

	"github.com/user/jobifai/internal/domain"
)

// BurnPrepare holds quota state resolved before the billing SQL transaction starts.
// It must not trigger queries on the parent *sql.DB during CommitTransactionalBurn.
type BurnPrepare struct {
	SkipBurn  bool
	Overrides domain.QuotaUserOverrides
}

// PrepareTransactionalBurn resolves admin status and per-user overrides outside an open transaction.
func (s *Service) PrepareTransactionalBurn(userID string, credits int64) (BurnPrepare, error) {
	if credits <= 0 || s.isAdmin(userID) {
		return BurnPrepare{SkipBurn: true}, nil
	}
	return BurnPrepare{Overrides: loadOverrides(s.cfg, userID)}, nil
}

// CommitTransactionalBurn applies a credit burn using only the supplied transaction.
func (s *Service) CommitTransactionalBurn(tx *sql.Tx, userID string, credits int64, prep BurnPrepare) error {
	if credits <= 0 || prep.SkipBurn {
		return nil
	}
	row, err := getRowTx(tx, userID)
	if err != nil {
		return err
	}
	row = applyOverridesToRow(row, prep.Overrides)
	if !row.EnforcementEnabled {
		return nil
	}
	if err := s.applyBurn(&row, credits); err != nil {
		return err
	}
	return updateRowTx(tx, row)
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
