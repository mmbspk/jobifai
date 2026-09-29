package quota

import "github.com/user/jobifai/internal/domain"

func effectiveTrialAllowance(def domain.QuotaDefaults, o domain.QuotaUserOverrides) int64 {
	if o.TrialCredits != nil {
		return int64(*o.TrialCredits)
	}
	return int64(def.TrialCredits)
}

func reconcileTrialRemaining(currentRemaining, oldAllowance, newAllowance int64) int64 {
	used := oldAllowance - currentRemaining
	if used < 0 {
		used = 0
	}
	next := newAllowance - used
	if next < 0 {
		next = 0
	}
	return next
}
