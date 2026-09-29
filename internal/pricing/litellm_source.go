package pricing

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	litellmModelCostURL = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"
	litellmMaxBytes     = 8 * 1024 * 1024
)

// LiteLLMSource fetches public model metadata for discovery staging only (not billing).
type LiteLLMSource struct {
	HTTPClient *http.Client
	LastGood   map[string]json.RawMessage
	LastFetch  time.Time
	LastError  string
}

func (s *LiteLLMSource) Name() string { return "litellm_public" }

func (s *LiteLLMSource) Refresh() (int, error) {
	cli := s.HTTPClient
	if cli == nil {
		cli = &http.Client{Timeout: 30 * time.Second}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, litellmModelCostURL, nil)
	if err != nil {
		return 0, err
	}
	resp, err := cli.Do(req)
	if err != nil {
		s.rememberErr(err)
		return s.fallbackCount(), s.fallbackErr()
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		s.rememberErr(fmt.Errorf("status %d", resp.StatusCode))
		return s.fallbackCount(), s.fallbackErr()
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, litellmMaxBytes))
	if err != nil {
		s.rememberErr(err)
		return s.fallbackCount(), s.fallbackErr()
	}
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(body, &parsed); err != nil || len(parsed) < 50 {
		s.rememberErr(fmt.Errorf("invalid or shrunken litellm map"))
		return s.fallbackCount(), s.fallbackErr()
	}
	s.LastGood = parsed
	s.LastFetch = time.Now().UTC()
	s.LastError = ""
	return len(parsed), nil
}

func (s *LiteLLMSource) rememberErr(err error) {
	if err != nil {
		s.LastError = err.Error()
	}
}

func (s *LiteLLMSource) fallbackCount() int {
	if s.LastGood == nil {
		return 0
	}
	return len(s.LastGood)
}

func (s *LiteLLMSource) fallbackErr() error {
	if s.LastGood != nil {
		return fmt.Errorf("litellm refresh failed, using last known good: %s", s.LastError)
	}
	return fmt.Errorf("litellm refresh failed: %s", s.LastError)
}

// ProviderGuess maps litellm model keys to Jobifai provider ids (heuristic).
func ProviderGuess(modelKey string) string {
	k := strings.ToLower(modelKey)
	switch {
	case strings.Contains(k, "claude"):
		return "claude"
	case strings.HasPrefix(k, "gpt-"), strings.HasPrefix(k, "o"):
		return "openai"
	case strings.Contains(k, "gemini"):
		return "gemini"
	default:
		return ""
	}
}
