package domain

// SystemUserID is the reserved settings/secrets scope for deployment-wide defaults.
const SystemUserID = "__system__"

// LLMOverrides holds optional per-user LLM settings. Empty fields inherit from system defaults.
type LLMOverrides struct {
	Provider   string               `json:"provider,omitempty"`
	Model      string               `json:"model,omitempty"`
	UseProxy   *bool                `json:"use_proxy,omitempty"`
	ProxyURL   string               `json:"proxy_url,omitempty"`
	MaxTokens  *int                 `json:"max_tokens,omitempty"`
	TaskModels map[string]TaskModel `json:"task_models,omitempty"`
}

// AdminUserRow is a summary row for the admin users list.
type AdminUserRow struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	IsAdmin     bool   `json:"is_admin"`
	CreatedAt   string `json:"created_at"`
	HasAPIKey   bool   `json:"has_api_key"`
}

// AdminUserDetail extends the list row with LLM override payload for editing.
type AdminUserDetail struct {
	AdminUserRow
	LLMOverrides   LLMOverrides       `json:"llm_overrides"`
	QuotaOverrides QuotaUserOverrides `json:"quota_overrides,omitempty"`
}
