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

	"github.com/rs/zerolog/log"
	"github.com/user/jobifai/internal/domain"
)

const contentTypeJSON = "application/json"

type ctxKey struct{}

// nonRetryableError wraps an HTTP 4xx error (excluding 429) that should not be retried.
type nonRetryableError struct{ err error }

func (e *nonRetryableError) Error() string { return e.err.Error() }
func (e *nonRetryableError) Unwrap() error { return e.err }

// WithTask returns a context annotated with a task label shown in LLM log lines.
func WithTask(ctx context.Context, task string) context.Context {
	return context.WithValue(ctx, ctxKey{}, task)
}

func taskLabel(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKey{}).(string); ok && v != "" {
		return " [" + v + "]"
	}
	return ""
}

// Message is a single chat turn.
type Message struct {
	Role    string `json:"role"`    // "user" | "assistant" | "system"
	Content string `json:"content"`
}

// Client dispatches LLM requests based on the active configuration.
type Client struct {
	cfg     domain.LLMConfig
	apiKey  string
	httpCli *http.Client
	tracker *UsageTracker // optional; if set, accumulates token usage per call
}

// New creates an LLM client from the current settings.
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

// WithModel returns a shallow copy of the client with an overridden model and
// optional max-token limit. All other config (provider, proxy, api key,
// tracker) is inherited unchanged. Passing maxTokens=0 keeps the current value.
func (c *Client) WithModel(model string, maxTokens int) *Client {
	cfg := c.cfg
	cfg.Model = model
	if maxTokens > 0 {
		cfg.MaxTokens = maxTokens
	}
	return &Client{cfg: cfg, apiKey: c.apiKey, httpCli: c.httpCli, tracker: c.tracker}
}

// Chat sends messages and returns the assistant reply.
// Retries up to 3 times total (2 retries) with a 2 s pause between attempts.
// Non-retryable HTTP 4xx errors (except 429 Too Many Requests) are returned immediately.
func (c *Client) Chat(ctx context.Context, msgs []Message) (string, error) {
	const maxAttempts = 3
	var err error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
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
			return result, nil
		}
		var nre *nonRetryableError
		if errors.As(err, &nre) {
			return "", nre.Unwrap()
		}
	}
	return "", err
}

// ── Claude (Anthropic Messages API) ───────────────────────────────────────

type claudeRequest struct {
	Model     string           `json:"model"`
	MaxTokens int              `json:"max_tokens"`
	System    string           `json:"system,omitempty"`
	Messages  []claudeMessage  `json:"messages"`
}

type claudeMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type claudeUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type claudeResponse struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
	Usage claudeUsage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type openaiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

func (c *Client) claudeChat(ctx context.Context, msgs []Message) (string, error) {
	var system string
	var turns []claudeMessage
	for _, m := range msgs {
		if m.Role == "system" {
			system = m.Content
			continue
		}
		turns = append(turns, claudeMessage{Role: m.Role, Content: m.Content})
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
	if c.tracker != nil {
		c.tracker.Add(&Usage{InputTokens: cr.Usage.InputTokens, OutputTokens: cr.Usage.OutputTokens}, c.cfg.Model)
	}
	log.Debug().Bool("llm_call", true).Msgf("llm: claude/%s%s ✓ in=%d out=%d", c.cfg.Model, taskLabel(ctx), cr.Usage.InputTokens, cr.Usage.OutputTokens)
	return cr.Content[0].Text, nil
}

// ChatWithImage sends a single user message containing a PNG image and a text
// prompt to Claude and returns the assistant reply. Only supported for the
// Claude provider; all others return an error.
func (c *Client) ChatWithImage(ctx context.Context, imageBytes []byte, prompt string) (string, error) {
	if c.cfg.Provider != "claude" {
		return "", fmt.Errorf("ChatWithImage: unsupported provider %q (claude only)", c.cfg.Provider)
	}

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
		Model     string          `json:"model"`
		MaxTokens int             `json:"max_tokens"`
		System    string          `json:"system,omitempty"`
		Messages  []visionMessage `json:"messages"`
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
	if c.tracker != nil {
		c.tracker.Add(&Usage{InputTokens: cr.Usage.InputTokens, OutputTokens: cr.Usage.OutputTokens}, c.cfg.Model)
	}
	log.Debug().Bool("llm_call", true).Msgf("llm: claude/%s vision%s ✓ in=%d out=%d", c.cfg.Model, taskLabel(ctx), cr.Usage.InputTokens, cr.Usage.OutputTokens)
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
	turns := make([]openaiMessage, len(msgs))
	for i, m := range msgs {
		turns[i] = openaiMessage{Role: m.Role, Content: m.Content}
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
	if c.tracker != nil {
		c.tracker.Add(&Usage{InputTokens: or.Usage.PromptTokens, OutputTokens: or.Usage.CompletionTokens}, c.cfg.Model)
	}
	log.Debug().Bool("llm_call", true).Msgf("llm: openai/%s%s ✓ in=%d out=%d", c.cfg.Model, taskLabel(ctx), or.Usage.PromptTokens, or.Usage.CompletionTokens)
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
	Error string `json:"error,omitempty"`
}

func (c *Client) ollamaChat(ctx context.Context, msgs []Message) (string, error) {
	turns := make([]openaiMessage, len(msgs))
	for i, m := range msgs {
		turns[i] = openaiMessage{Role: m.Role, Content: m.Content}
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
	defer resp.Body.Close()
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
