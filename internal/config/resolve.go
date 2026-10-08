package config

import "github.com/user/jobifai/internal/domain"

const (
	KeyGeneralSettings = "general_settings"
	KeyLLMOverrides    = "llm_overrides"
)

// ConfigGetter loads settings rows (used by bot and LLM resolution).
type ConfigGetter interface {
	Get(userID, key string, dst any) error
}

// KV is ConfigGetter plus writes (used by admin handlers).
type KV interface {
	ConfigGetter
	Set(userID, key string, src any) error
}

// SecretsKV is the minimal secrets store surface used for API key resolution.
type SecretsKV interface {
	Get(userID, key string) (string, error)
	Has(userID, key string) bool
}

var defaultOperationalSettings = domain.GeneralSettings{
	LLM: domain.LLMConfig{
		Provider: "claude",
		Model:    "claude-sonnet-4-6",
		UseProxy: false,
	},
	Browser: domain.BrowserConfig{
		ShowBrowser:      true,
		UseChromeProfile: true,
		RemoteDebugPort:  0,
	},
	HumanBehavior: domain.HumanBehaviorConfig{
		DailyApplicationLimit: 40,
		JobReadTimeMin:        10,
		JobReadTimeMax:        30,
		PauseBetweenJobsMin:   5,
		PauseBetweenJobsMax:   15,
	},
	JobSuitabilityScore:       7,
	MaxJobsPerKeyword: 25,
}

func getGeneral(store ConfigGetter, userID string) (domain.GeneralSettings, error) {
	var gs domain.GeneralSettings
	if err := store.Get(userID, KeyGeneralSettings, &gs); err != nil {
		return domain.GeneralSettings{}, err
	}
	return gs, nil
}

func normalizeSystemLLM(gs domain.GeneralSettings) domain.GeneralSettings {
	if gs.LLM.Provider == "" {
		gs.LLM.Provider = defaultOperationalSettings.LLM.Provider
	}
	if gs.LLM.Model == "" {
		gs.LLM.Model = defaultOperationalSettings.LLM.Model
	}
	return gs
}

// SystemGeneralKV loads deployment-wide automation defaults.
func SystemGeneralKV(store ConfigGetter) domain.GeneralSettings {
	gs, err := getGeneral(store, domain.SystemUserID)
	if err != nil {
		return defaultOperationalSettings
	}
	return normalizeSystemLLM(gs)
}

func applyUserApplicationFields(base, user domain.GeneralSettings, hasUser bool) domain.GeneralSettings {
	if !hasUser {
		return base
	}
	user = ApplyUnsetUserApplicationFields(user)
	out := base
	out.DefaultResumeMarket = user.DefaultResumeMarket
	out.RequireReview = user.RequireReview
	out.JobSuitabilityScore = user.JobSuitabilityScore
	out.MaxJobsPerKeyword = user.MaxJobsPerKeyword
	if out.MaxJobsPerKeyword == 0 {
		out.MaxJobsPerKeyword = base.MaxJobsPerKeyword
	}
	out.HalalJobFilter = user.HalalJobFilter
	out.GenerateNewResumeDocs = user.GenerateNewResumeDocs
	if hasUser {
		out.DocumentPolicies = MigrateDocumentPolicies(user)
	} else {
		ApplyPoliciesForNewUser(&out)
	}
	if user.HumanBehavior.DailyApplicationLimit > 0 {
		out.HumanBehavior.DailyApplicationLimit = user.HumanBehavior.DailyApplicationLimit
	}
	return out
}

// MergeLLM applies optional overrides on top of base system LLM config.
func MergeLLM(base domain.LLMConfig, o domain.LLMOverrides) domain.LLMConfig {
	out := base
	if o.Provider != "" {
		out.Provider = o.Provider
	}
	if o.Model != "" {
		out.Model = o.Model
	}
	if o.UseProxy != nil {
		out.UseProxy = *o.UseProxy
	}
	if o.ProxyURL != "" {
		out.ProxyURL = o.ProxyURL
	}
	if o.MaxTokens != nil {
		out.MaxTokens = *o.MaxTokens
	}
	if len(o.TaskModels) > 0 {
		if out.TaskModels == nil {
			out.TaskModels = map[string]domain.TaskModel{}
		}
		for k, v := range o.TaskModels {
			if v.Model != "" || v.MaxTokens != 0 {
				out.TaskModels[k] = v
			}
		}
	}
	return out
}

// ResolveOperationalSettings merges system automation defaults, user application
// preferences, and — when allowLLMOverrides is true — optional per-user LLM overrides.
// Pass allowLLMOverrides=true only for admin and tester users; regular users must always
// use the system LLM configuration (provider, model, task models, proxy).
func ResolveOperationalSettings(store ConfigGetter, userID string, allowLLMOverrides bool) domain.GeneralSettings {
	sys := SystemGeneralKV(store)
	user, err := getGeneral(store, userID)
	hasUser := err == nil
	if !hasUser {
		user = domain.GeneralSettings{}
	}
	out := applyUserApplicationFields(sys, user, hasUser)

	if !allowLLMOverrides {
		EnsureDocumentPolicies(&out)
		return out
	}

	// Legacy: LLM stored on the user general_settings row before system defaults existed.
	if hasUser && (user.LLM.Provider != "" || user.LLM.Model != "" || len(user.LLM.TaskModels) > 0 || user.LLM.UseProxy) {
		legacy := domain.LLMOverrides{
			Provider:   user.LLM.Provider,
			Model:      user.LLM.Model,
			ProxyURL:   user.LLM.ProxyURL,
			TaskModels: user.LLM.TaskModels,
		}
		if user.LLM.UseProxy {
			t := true
			legacy.UseProxy = &t
		}
		if user.LLM.MaxTokens > 0 {
			mt := user.LLM.MaxTokens
			legacy.MaxTokens = &mt
		}
		out.LLM = MergeLLM(out.LLM, legacy)
	}

	var overrides domain.LLMOverrides
	if err := store.Get(userID, KeyLLMOverrides, &overrides); err == nil {
		out.LLM = MergeLLM(out.LLM, overrides)
	}
	EnsureDocumentPolicies(&out)
	return out
}

// ResolveLLMAPIKey returns the API key to use for userID, whether it is a personal
// (user-scoped) key, and any error. The second return value is true only when the
// key came from the user's own stored credential (llm_api_key or proxy_key), which
// is the authoritative source for billing attribution and consistency checks.
//
// Personal keys are only returned when allowPersonalKey is true; otherwise the
// system key is used. allowPersonalKey must be true only for tester and admin users.
func ResolveLLMAPIKey(secrets SecretsKV, userID string, allowPersonalKey bool, useProxy bool) (key string, isPersonal bool, err error) {
	if useProxy {
		if allowPersonalKey {
			if k, e := secrets.Get(userID, "proxy_key"); e == nil && k != "" {
				return k, true, nil
			}
		}
		if k, e := secrets.Get(domain.SystemUserID, "proxy_key"); e == nil && k != "" {
			return k, false, nil
		}
	}
	if allowPersonalKey && secrets.Has(userID, "llm_api_key") {
		k, e := secrets.Get(userID, "llm_api_key")
		return k, true, e
	}
	k, e := secrets.Get(domain.SystemUserID, "llm_api_key")
	return k, false, e
}

// EnforceLLMCredentialConsistency ensures that a tester's LLM overrides are only
// active when a personal credential is actually selected. It uses the same resolver
// that will be used to build the LLM client, so the decision is based on the credential
// that would actually be used — not merely on the presence of a secret.
//
// When the resolver returns a system (non-personal) credential, the entire gs.LLM is
// replaced with the system LLM config. This resets ALL override fields — provider, model,
// task models, UseProxy, proxy URL, MaxTokens — so none of them can remain active against
// an Admin-funded credential.
//
// When a personal credential is selected, gs.LLM is left unchanged. The tester owns both
// the key and the configuration; runtime auth/quota failures from that credential will
// propagate as errors and will never silently fall back to the system credential.
//
// Must be called after ResolveOperationalSettings and before building the LLM client.
func EnforceLLMCredentialConsistency(store ConfigGetter, secrets SecretsKV, userID string, allowOverrides bool, gs *domain.GeneralSettings) {
	if !allowOverrides {
		return
	}
	// Ask the same resolver with the same UseProxy flag it will receive from the caller.
	// If it selects a system credential (or errors), revert the entire LLM config.
	_, isPersonal, err := ResolveLLMAPIKey(secrets, userID, true, gs.LLM.UseProxy)
	if err != nil || !isPersonal {
		gs.LLM = SystemGeneralKV(store).LLM
	}
}

// HasUserLLMAPIKey reports whether the user has their own API key override.
func HasUserLLMAPIKey(secrets SecretsKV, userID string) bool {
	return secrets.Has(userID, "llm_api_key")
}
