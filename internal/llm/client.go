// Package llm provides a single LLM client that dispatches to Claude, OpenAI,
// Ollama, or Gemini based on the GeneralSettings, with optional proxy support.
package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/pricing"
	"github.com/user/jobifai/internal/quota"
)

const contentTypeJSON = "application/json"

type ctxKey struct{}

// nonRetryableError wraps an HTTP 4xx error (excluding 429) that should not be retried.
type nonRetryableError struct{ err error }

func (e *nonRetryableError) Error() string { return e.err.Error() }
func (e *nonRetryableError) Unwrap() error { return e.err }

func taskLabel(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKey{}).(string); ok && v != "" {
		return " [" + v + "]"
	}
	call := CallContextFrom(ctx)
	if call.Task != "" {
		return " [" + call.Task + "]"
	}
	return ""
}

// Message is a single chat turn.
type Message struct {
	Role    string `json:"role"` // "user" | "assistant" | "system"
	Content string `json:"content"`
	// CacheEphemeral requests Claude ephemeral prompt caching on this block when supported.
	// Cache writes/reads are not guaranteed (model minimum prefix length and request shape apply).
	CacheEphemeral bool `json:"-"`
}

// Client dispatches LLM requests based on the active configuration.
type Client struct {
	cfg         domain.LLMConfig
	apiKey      string
	httpCli     *http.Client
	tracker     *UsageTracker // optional; if set, accumulates token usage per call
	billing     BillingHooks
	guard       quota.LLMGuard
	userID      string
	taskRuntime TaskRuntime
	costCeiling *costCeiling
	reuse       *ReuseCoordinator
}

// WithReuse attaches exact-generation reuse (content-scoped, visual identity separate).
func (c *Client) WithReuse(r *ReuseCoordinator) *Client {
	c.reuse = r
	return c
}

// New creates an LLM client from the current settings.
// ModelName returns the configured model id for this client copy.
func (c *Client) ModelName() string {
	if c == nil {
		return ""
	}
	return c.cfg.Model
}

func New(cfg domain.LLMConfig, apiKey string) *Client {
	return &Client{
		cfg:    cfg,
		apiKey: apiKey,
		httpCli: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// WithTracker attaches a usage tracker that accumulates tokens across calls.
func (c *Client) WithTracker(t *UsageTracker) *Client {
	c.tracker = t
	return c
}

// WithUserID sets billing attribution for this client (independent of quota enforcement).
func (c *Client) WithUserID(userID string) *Client {
	c.userID = userID
	return c
}

// WithQuota attaches quota enforcement for the given user.
func (c *Client) WithQuota(g quota.LLMGuard, userID string) *Client {
	c.guard = g
	return c
}

// WithModel returns a shallow copy of the client with an overridden model and
// optional max-token limit. All other config (provider, proxy, api key,
// tracker) is inherited unchanged. Passing maxTokens=0 keeps the current value.
func (c *Client) WithModel(model string, maxTokens int) *Client {
	cfg := c.cfg
	cfg.Model = model
	if maxTokens > 0 {
		cfg.MaxTokens = maxTokens
	}
	return &Client{
		cfg: cfg, apiKey: c.apiKey, httpCli: c.httpCli, tracker: c.tracker,
		billing: c.billing, guard: c.guard, userID: c.userID,
		taskRuntime: c.taskRuntime, costCeiling: c.costCeiling,
	}
}

// WithTaskRuntime attaches resolved per-task options (effort, timeout, mode, etc.).
func (c *Client) WithTaskRuntime(r TaskRuntime) *Client {
	nc := *c
	nc.taskRuntime = r
	return &nc
}

func (c *Client) prepareCallContext(ctx context.Context) context.Context {
	call := CallContextFrom(ctx)
	if call.UserID == "" {
		call.UserID = c.userID
	}
	if call.OperationID == "" {
		call.OperationID = uuid.NewString()
	}
	return WithCallContext(ctx, call)
}

func (c *Client) checkQuota(ctx context.Context, estInputChars int, estOutputTokens int) error {
	if c.guard == nil || c.userID == "" || c.cfg.Provider == "ollama" {
		return nil
	}
	estIn := estInputChars / 4
	if estIn < 1 {
		estIn = 1
	}
	out := estOutputTokens
	if out <= 0 {
		out = 4096
	}
	return c.guard.BeforeLLM(ctx, c.userID, c.cfg.Model, estIn, out)
}

// Chat sends messages and returns the assistant reply.
// Retries up to 3 times total (2 retries) with a 2 s pause between attempts.
// Non-retryable HTTP 4xx errors (except 429 Too Many Requests) are returned immediately.
func (c *Client) Chat(ctx context.Context, msgs []Message) (string, error) {
	if c.taskRuntime.TimeoutSec > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(c.taskRuntime.TimeoutSec)*time.Second)
		defer cancel()
	}
	inChars := 0
	for _, m := range msgs {
		inChars += len(m.Content)
	}
	maxOut := c.cfg.MaxTokens
	if maxOut <= 0 {
		maxOut = 8192
	}
	if err := c.checkCostCeiling(inChars, maxOut); err != nil {
		return "", err
	}
	if err := c.checkQuota(ctx, inChars, c.cfg.MaxTokens); err != nil {
		return "", err
	}
	var reuseFP, reuseTask string
	var reuseEnabled bool
	if br, fp, task, ok := c.reuseBegin(ctx, msgs); ok {
		reuseEnabled = true
		reuseFP, reuseTask = fp, task
		if br.CacheHit {
			return br.Response, nil
		}
	}
	const maxAttempts = 3
	var err error
	ctx = c.prepareCallContext(ctx)
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		call := CallContextFrom(ctx)
		call.Attempt = attempt
		ctx = WithCallContext(ctx, call)
		if attempt > 1 {
			log.Warn().Msgf("llm: retry %d/%d after error: %v", attempt, maxAttempts, err)
			time.Sleep(2 * time.Second)
		}
		var result string
		switch c.cfg.Provider {
		case "claude":
			result, err = c.claudeChat(ctx, msgs)
		case "openai", "gemini": // gemini uses an OpenAI-compatible endpoint via proxy
			result, err = c.openaiChat(ctx, msgs)
		case "ollama":
			result, err = c.ollamaChat(ctx, msgs)
		default:
			return "", fmt.Errorf("unknown LLM provider: %q", c.cfg.Provider)
		}
		if err == nil {
			if reuseEnabled {
				c.reuseComplete(ctx, reuseTask, reuseFP, result)
			}
			return result, nil
		}
		if errors.Is(err, ErrBillingPersistFailed) {
			return "", err
		}
		var nre *nonRetryableError
		if errors.As(err, &nre) {
			return "", nre.Unwrap()
		}
	}
	if err != nil && !errors.Is(err, ErrBillingPersistFailed) {
		c.recordTerminalFailure(ctx, classifyChatError(err))
		if reuseEnabled {
			c.reuseFailedUncertain(ctx, reuseTask, reuseFP)
		}
	}
	return "", err
}

func classifyChatError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, ErrBillingPersistFailed) {
		return "billing_persist_failed"
	}
	return "provider_error"
}

func (c *Client) recordTerminalFailure(ctx context.Context, errCode string) {
	if errCode == "" || c.billing.Ledger == nil {
		return
	}
	call := CallContextFrom(ctx)
	if call.OperationID == "" {
		return
	}
	bu := billingUsage{
		TokenUsage:  pricing.TokenUsage{},
		ActualModel: c.cfg.Model,
	}
	_ = c.recordUsage(ctx, bu, 0, false, errCode, true)
}

// ── Claude (Anthropic Messages API) ───────────────────────────────────────

type claudeRequest struct {
	Model        string              `json:"model"`
	MaxTokens    int                 `json:"max_tokens"`
	System       any                 `json:"system,omitempty"`
	Messages     []claudeMessage     `json:"messages"`
	OutputConfig *claudeOutputConfig `json:"output_config,omitempty"`
}

type claudeOutputConfig struct {
	Effort string `json:"effort,omitempty"`
}

type claudeMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type claudeTextBlock struct {
	Type         string                 `json:"type"`
	Text         string                 `json:"text"`
	CacheControl *claudeCacheControl    `json:"cache_control,omitempty"`
}

type claudeCacheControl struct {
	Type string `json:"type"`
}

func claudeContentPayload(text string, cacheEphemeral bool) any {
	if text == "" {
		return ""
	}
	if !cacheEphemeral {
		return text
	}
	return []claudeTextBlock{{
		Type:         "text",
		Text:         text,
		CacheControl: &claudeCacheControl{Type: "ephemeral"},
	}}
}

type claudeUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreation            *struct {
		Ephemeral5mInputTokens int `json:"ephemeral_5m_input_tokens"`
		Ephemeral1hInputTokens int `json:"ephemeral_1h_input_tokens"`
	} `json:"cache_creation,omitempty"`
}

type claudeResponse struct {
	Model   string `json:"model"`
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
	Usage claudeUsage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func claudeBillingUsage(reqModel string, cr claudeResponse) billingUsage {
	actual := cr.Model
	verified := actual != ""
	if actual == "" {
		actual = reqModel
	}
	cw5, cw1 := 0, 0
	if cr.Usage.CacheCreation != nil {
		cw5 = cr.Usage.CacheCreation.Ephemeral5mInputTokens
		cw1 = cr.Usage.CacheCreation.Ephemeral1hInputTokens
	}
	if cw5 == 0 && cw1 == 0 && cr.Usage.CacheCreationInputTokens > 0 {
		cw5 = cr.Usage.CacheCreationInputTokens
	}
	return billingUsage{
		TokenUsage: pricing.TokenUsage{
			InputTokens:        int64(cr.Usage.InputTokens),
			OutputTokens:       int64(cr.Usage.OutputTokens),
			CacheWrite5mTokens: int64(cw5),
			CacheWrite1hTokens: int64(cw1),
			CacheReadTokens:    int64(cr.Usage.CacheReadInputTokens),
		},
		ActualModel:         actual,
		ActualModelVerified: verified,
	}
}

type openaiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

func (c *Client) claudeChat(ctx context.Context, msgs []Message) (string, error) {
	start := time.Now()
	var system any
	var turns []claudeMessage
	for _, m := range msgs {
		if m.Role == "system" {
			system = claudeContentPayload(m.Content, m.CacheEphemeral)
			continue
		}
		turns = append(turns, claudeMessage{
			Role:    m.Role,
			Content: claudeContentPayload(m.Content, m.CacheEphemeral),
		})
	}

	maxTokens := c.cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 8192 // safe default for all current Claude models
	}
	body := claudeRequest{
		Model:     c.cfg.Model,
		MaxTokens: maxTokens,
		System:    system,
		Messages:  turns,
	}
	if c.taskRuntime.Effort != "" {
		body.OutputConfig = &claudeOutputConfig{Effort: c.taskRuntime.Effort}
	}

	baseURL := "https://api.anthropic.com"
	if c.cfg.UseProxy && c.cfg.ProxyURL != "" {
		baseURL = strings.TrimRight(c.cfg.ProxyURL, "/")
	}

	resp, err := c.post(ctx, baseURL+"/v1/messages", map[string]string{
		"x-api-key":         c.apiKey,
		"anthropic-version": "2023-06-01",
		"content-type":      contentTypeJSON,
	}, body)
	if err != nil {
		log.Error().Bool("llm_call", true).Msgf("llm: claude/%s%s call failed: %v", c.cfg.Model, taskLabel(ctx), err)
		return "", err
	}
	var cr claudeResponse
	if err := json.Unmarshal(resp, &cr); err != nil {
		return "", fmt.Errorf("claude decode: %w", err)
	}
	if cr.Error != nil {
		log.Error().Bool("llm_call", true).Msgf("llm: claude/%s%s error: %s", c.cfg.Model, taskLabel(ctx), cr.Error.Message)
		return "", fmt.Errorf("claude error: %s", cr.Error.Message)
	}
	if len(cr.Content) == 0 {
		return "", fmt.Errorf("claude: empty response")
	}
	bu := claudeBillingUsage(c.cfg.Model, cr)
	if err := c.recordUsage(ctx, bu, time.Since(start).Milliseconds(), true, "", true); err != nil {
		return "", billingPersistErr(err)
	}
	log.Info().
		Str("event", "llm_call").
		Str("task", CallContextFrom(ctx).Task).
		Str("provider", c.cfg.Provider).
		Str("requested_model", c.cfg.Model).
		Str("actual_model", bu.ActualModel).
		Bool("actual_model_verified", bu.ActualModelVerified).
		Int64("input_tokens", bu.InputTokens).
		Int64("output_tokens", bu.OutputTokens).
		Int64("duration_ms", time.Since(start).Milliseconds()).
		Bool("success", true).
		Str("job_id", CallContextFrom(ctx).JobID).
		Str("run_id", CallContextFrom(ctx).AutomationRunID).
		Msg("llm call completed")
	return cr.Content[0].Text, nil
}

// ChatWithImage sends a single user message containing a PNG image and a text
// prompt to Claude and returns the assistant reply. Only supported for the
// Claude provider; all others return an error.
func (c *Client) ChatWithImage(ctx context.Context, imageBytes []byte, prompt string) (string, error) {
	start := time.Now()
	if c.taskRuntime.TimeoutSec > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(c.taskRuntime.TimeoutSec)*time.Second)
		defer cancel()
	}
	if c.cfg.Provider != "claude" {
		return "", fmt.Errorf("ChatWithImage: unsupported provider %q (claude only)", c.cfg.Provider)
	}
	maxOut := c.cfg.MaxTokens
	if maxOut <= 0 {
		maxOut = 8192
	}
	if err := c.checkVisionCostCeiling(imageBytes, len(prompt), maxOut); err != nil {
		return "", err
	}
	if err := c.checkQuota(ctx, len(imageBytes)+len(prompt), c.cfg.MaxTokens); err != nil {
		return "", err
	}
	ctx = c.prepareCallContext(ctx)

	type imageSource struct {
		Type      string `json:"type"`
		MediaType string `json:"media_type"`
		Data      string `json:"data"`
	}
	type contentBlock struct {
		Type   string       `json:"type"`
		Source *imageSource `json:"source,omitempty"`
		Text   string       `json:"text,omitempty"`
	}
	type visionMessage struct {
		Role    string         `json:"role"`
		Content []contentBlock `json:"content"`
	}
	type visionRequest struct {
		Model        string              `json:"model"`
		MaxTokens    int                 `json:"max_tokens"`
		System       string              `json:"system,omitempty"`
		Messages     []visionMessage     `json:"messages"`
		OutputConfig *claudeOutputConfig `json:"output_config,omitempty"`
	}

	maxTokens := c.cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 8192
	}
	body := visionRequest{
		Model:     c.cfg.Model,
		MaxTokens: maxTokens,
		Messages: []visionMessage{{
			Role: "user",
			Content: []contentBlock{
				{Type: "image", Source: &imageSource{
					Type:      "base64",
					MediaType: "image/png",
					Data:      base64.StdEncoding.EncodeToString(imageBytes),
				}},
				{Type: "text", Text: prompt},
			},
		}},
	}
	if c.taskRuntime.Effort != "" {
		body.OutputConfig = &claudeOutputConfig{Effort: c.taskRuntime.Effort}
	}

	baseURL := "https://api.anthropic.com"
	if c.cfg.UseProxy && c.cfg.ProxyURL != "" {
		baseURL = strings.TrimRight(c.cfg.ProxyURL, "/")
	}

	resp, err := c.post(ctx, baseURL+"/v1/messages", map[string]string{
		"x-api-key":         c.apiKey,
		"anthropic-version": "2023-06-01",
		"content-type":      contentTypeJSON,
	}, body)
	if err != nil {
		return "", err
	}
	var cr claudeResponse
	if err := json.Unmarshal(resp, &cr); err != nil {
		return "", fmt.Errorf("claude vision decode: %w", err)
	}
	if cr.Error != nil {
		return "", fmt.Errorf("claude vision error: %s", cr.Error.Message)
	}
	if len(cr.Content) == 0 {
		return "", fmt.Errorf("claude vision: empty response")
	}
	bu := claudeBillingUsage(c.cfg.Model, cr)
	if err := c.recordUsage(ctx, bu, time.Since(start).Milliseconds(), true, "", true); err != nil {
		return "", billingPersistErr(err)
	}
	return cr.Content[0].Text, nil
}

// ── OpenAI-compatible (OpenAI / Gemini via proxy) ─────────────────────────

type openaiRequest struct {
	Model     string          `json:"model"`
	Messages  []openaiMessage `json:"messages"`
	MaxTokens int             `json:"max_tokens,omitempty"`
}

type openaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openaiResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage openaiUsage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (c *Client) openaiChat(ctx context.Context, msgs []Message) (string, error) {
	start := time.Now()
	turns := make([]openaiMessage, 0, len(msgs))
	for _, m := range msgs {
		if m.Content == "" {
			continue
		}
		turns = append(turns, openaiMessage{Role: m.Role, Content: m.Content})
	}

	body := openaiRequest{Model: c.cfg.Model, Messages: turns, MaxTokens: c.cfg.MaxTokens}

	baseURL := "https://api.openai.com"
	if c.cfg.UseProxy && c.cfg.ProxyURL != "" {
		baseURL = strings.TrimRight(c.cfg.ProxyURL, "/")
	}

	resp, err := c.post(ctx, baseURL+"/v1/chat/completions", map[string]string{
		"Authorization": "Bearer " + c.apiKey,
		"Content-Type":  contentTypeJSON,
	}, body)
	if err != nil {
		log.Error().Bool("llm_call", true).Msgf("llm: openai/%s%s call failed: %v", c.cfg.Model, taskLabel(ctx), err)
		return "", err
	}
	var or openaiResponse
	if err := json.Unmarshal(resp, &or); err != nil {
		return "", fmt.Errorf("openai decode: %w", err)
	}
	if or.Error != nil {
		log.Error().Bool("llm_call", true).Msgf("llm: openai/%s%s error: %s", c.cfg.Model, taskLabel(ctx), or.Error.Message)
		return "", fmt.Errorf("openai error: %s", or.Error.Message)
	}
	if len(or.Choices) == 0 {
		return "", fmt.Errorf("openai: empty response")
	}
	actual := or.Model
	verified := actual != ""
	if actual == "" {
		actual = c.cfg.Model
	}
	bu := billingUsage{
		TokenUsage: pricing.TokenUsage{
			InputTokens:  int64(or.Usage.PromptTokens),
			OutputTokens: int64(or.Usage.CompletionTokens),
		},
		ActualModel:         actual,
		ActualModelVerified: verified,
	}
	if err := c.recordUsage(ctx, bu, time.Since(start).Milliseconds(), true, "", true); err != nil {
		return "", billingPersistErr(err)
	}
	return or.Choices[0].Message.Content, nil
}

// ── Ollama (local) ────────────────────────────────────────────────────────

type ollamaRequest struct {
	Model    string          `json:"model"`
	Messages []openaiMessage `json:"messages"`
	Stream   bool            `json:"stream"`
}

type ollamaResponse struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	PromptEvalCount int `json:"prompt_eval_count"`
	EvalCount       int `json:"eval_count"`
	Error           string `json:"error,omitempty"`
}

func (c *Client) ollamaChat(ctx context.Context, msgs []Message) (string, error) {
	start := time.Now()
	ctx = c.prepareCallContext(ctx)
	turns := make([]openaiMessage, 0, len(msgs))
	for _, m := range msgs {
		if m.Content == "" {
			continue
		}
		turns = append(turns, openaiMessage{Role: m.Role, Content: m.Content})
	}

	baseURL := "http://localhost:11434"
	if c.cfg.UseProxy && c.cfg.ProxyURL != "" {
		baseURL = strings.TrimRight(c.cfg.ProxyURL, "/")
	}

	body := ollamaRequest{Model: c.cfg.Model, Messages: turns, Stream: false}
	resp, err := c.post(ctx, baseURL+"/api/chat", map[string]string{
		"Content-Type": contentTypeJSON,
	}, body)
	if err != nil {
		log.Error().Bool("llm_call", true).Msgf("llm: ollama/%s%s call failed: %v", c.cfg.Model, taskLabel(ctx), err)
		return "", err
	}
	var or ollamaResponse
	if err := json.Unmarshal(resp, &or); err != nil {
		return "", fmt.Errorf("ollama decode: %w", err)
	}
	if or.Error != "" {
		log.Error().Bool("llm_call", true).Msgf("llm: ollama/%s%s error: %s", c.cfg.Model, taskLabel(ctx), or.Error)
		return "", fmt.Errorf("ollama error: %s", or.Error)
	}
	bu := billingUsage{
		TokenUsage: pricing.TokenUsage{
			InputTokens:  int64(or.PromptEvalCount),
			OutputTokens: int64(or.EvalCount),
		},
		ActualModel:         c.cfg.Model,
		ActualModelVerified: true,
	}
	if err := c.recordUsage(ctx, bu, time.Since(start).Milliseconds(), true, "", true); err != nil {
		return "", billingPersistErr(err)
	}
	log.Debug().Bool("llm_call", true).Msgf("llm: ollama/%s%s ✓", c.cfg.Model, taskLabel(ctx))
	return or.Message.Content, nil
}

// ── shared HTTP helper ─────────────────────────────────────────────────────

func (c *Client) post(ctx context.Context, url string, headers map[string]string, body any) ([]byte, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.httpCli.Do(req)
	if err != nil {
		return nil, fmt.Errorf("llm http: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("llm read body: %w", err)
	}
	if resp.StatusCode >= 400 {
		e := fmt.Errorf("llm http %d: %s", resp.StatusCode, string(data))
		if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode < 500 {
			return nil, &nonRetryableError{e}
		}
		return nil, e
	}
	return data, nil
}
