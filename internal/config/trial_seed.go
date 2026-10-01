package config

import (
	"errors"

	"github.com/user/jobifai/internal/domain"
)

// SeedTrialDailyApplicationLimit sets the user's daily application cap when unset (trial default).
func SeedTrialDailyApplicationLimit(store KV, userID string) {
	if store == nil || userID == "" {
		return
	}
	var gs domain.GeneralSettings
	err := store.Get(userID, KeyGeneralSettings, &gs)
	if errors.Is(err, domain.ErrNotFound) {
		gs = domain.GeneralSettings{
			HumanBehavior: domain.HumanBehaviorConfig{
				DailyApplicationLimit: domain.TrialDailyApplicationLimit,
			},
		}
		_ = store.Set(userID, KeyGeneralSettings, gs)
		return
	}
	if err != nil || gs.HumanBehavior.DailyApplicationLimit > 0 {
		return
	}
	gs.HumanBehavior.DailyApplicationLimit = domain.TrialDailyApplicationLimit
	_ = store.Set(userID, KeyGeneralSettings, gs)
}
