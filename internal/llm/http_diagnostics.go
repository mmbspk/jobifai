package llm

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http/httptrace"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// Record transport progress without logging URLs, headers, prompts, responses,
// or raw provider errors (which can contain credentials or user content).
func (c *Client) traceHTTP(ctx context.Context) (context.Context, func(string), func(int, string, error)) {
	start := time.Now()
	deadlineMS := int64(-1)
	if deadline, ok := ctx.Deadline(); ok {
		deadlineMS = time.Until(deadline).Milliseconds()
	}
	var mu sync.Mutex
	phase := "waiting_connection"
	setPhase := func(value string) { mu.Lock(); phase = value; mu.Unlock() }
	trace := &httptrace.ClientTrace{
		DNSStart:          func(httptrace.DNSStartInfo) { setPhase("dns") },
		ConnectStart:      func(string, string) { setPhase("connect") },
		TLSHandshakeStart: func() { setPhase("tls") },
		TLSHandshakeDone:  func(tls.ConnectionState, error) {},
		GotConn:           func(httptrace.GotConnInfo) { setPhase("writing_request") },
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			if info.Err == nil {
				setPhase("waiting_response")
			}
		},
		GotFirstResponseByte: func() { setPhase("reading_response") },
	}
	finish := func(status int, requestID string, err error) {
		mu.Lock()
		lastPhase := phase
		mu.Unlock()
		cc := CallContextFrom(ctx)
		e := log.Info()
		if err != nil {
			e = log.Error()
		}
		e = e.Str("event", "llm_http_result").Str("provider", c.cfg.Provider).
			Str("requested_model", c.cfg.Model).Str("task", cc.Task).
			Str("user_id", cc.UserID).Str("automation_run_id", cc.AutomationRunID).
			Str("job_id", cc.JobID).Int("attempt", cc.Attempt).
			Int64("duration_ms", time.Since(start).Milliseconds()).Str("phase", lastPhase).
			Int("http_status", status).Int64("deadline_remaining_ms_at_start", deadlineMS).
			Int64("http_timeout_ms", c.httpCli.Timeout.Milliseconds()).Bool("success", err == nil)
		if requestID != "" && len(requestID) <= 128 && strings.IndexFunc(requestID, func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_.:/", r))
		}) < 0 {
			e = e.Str("provider_request_id", requestID)
		}
		source := "none"
		if err != nil {
			var ne net.Error
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				source = "context_deadline"
			} else if errors.As(err, &ne) && ne.Timeout() {
				source = "http_client_or_transport"
			}
			e = e.Str("failure_code", classifyLLMError(err)).Str("timeout_source", source).Str("error_type", fmt.Sprintf("%T", err))
			var op *net.OpError
			if errors.As(err, &op) {
				e = e.Str("network_operation", op.Op)
			}
		}
		if err != nil {
			e.Msgf("llm HTTP failed: attempt=%d phase=%s status=%d timeout_source=%s duration_ms=%d", cc.Attempt, lastPhase, status, source, time.Since(start).Milliseconds())
		} else {
			e.Msg("llm HTTP request completed")
		}
	}
	return httptrace.WithClientTrace(ctx, trace), setPhase, finish
}
