package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/usersessions/assertion/idjag"
)

func idJAGExchangeLogLines(t *testing.T, logs *bytes.Buffer) []map[string]any {
	t.Helper()
	var lines []map[string]any
	scanner := bufio.NewScanner(strings.NewReader(logs.String()))
	for scanner.Scan() {
		var line map[string]any
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &line))
		lines = append(lines, line)
	}
	return lines
}

func TestIDJAGExchangeLogsEachOutcomeOnceWithIssuer(t *testing.T) {
	t.Parallel()
	issuerID := uuid.New()
	endpoint := &ResolvedMcpEndpoint{UserSessionIssuerID: issuerID}

	for _, tc := range []struct {
		name    string
		finish  func(*idJAGExchange, *slog.Logger)
		message string
		outcome mcpmetrics.IDJAGExchangeOutcome
	}{
		{name: "error", finish: func(*idJAGExchange, *slog.Logger) {}, message: "oauth ID-JAG token request failed", outcome: mcpmetrics.IDJAGExchangeError},
		{name: "rejected", finish: func(e *idJAGExchange, logger *slog.Logger) {
			logger.InfoContext(t.Context(), "oauth ID-JAG token request rejected", attrsToArgs(e.reject(mcpmetrics.IDJAGRejectionInvalidRequest, nil))...)
		}, message: "oauth ID-JAG token request rejected", outcome: mcpmetrics.IDJAGExchangeRejected},
		{name: "accepted", finish: func(e *idJAGExchange, logger *slog.Logger) {
			e.accept()
			logger.InfoContext(t.Context(), "oauth ID-JAG token request completed", attrsToArgs(e.acceptedAttrs())...)
		}, message: "oauth ID-JAG token request completed", outcome: mcpmetrics.IDJAGExchangeAccepted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var logs bytes.Buffer
			base := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug, AddSource: false, ReplaceAttr: nil}))
			svc := &Service{}
			exchange, logger := svc.startIDJAGExchange(endpoint, base)
			tc.finish(exchange, logger)
			exchange.record(t.Context())

			lines := idJAGExchangeLogLines(t, &logs)
			require.Len(t, lines, 1)
			require.Equal(t, tc.message, lines[0]["msg"])
			require.Equal(t, string(tc.outcome), lines[0][string(attr.OAuthIDJAGOutcomeKey)])
			require.Equal(t, issuerID.String(), lines[0][string(attr.UserSessionIssuerIDKey)])
			require.Equal(t, 1, strings.Count(logs.String(), string(attr.UserSessionIssuerIDKey)))
		})
	}
}

func TestIDJAGExchangeRecordsInfrastructureRefusalAsError(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		reason  idjag.Reason
		outcome mcpmetrics.IDJAGExchangeOutcome
	}{
		{name: "replay store unavailable", reason: idjag.ReasonReplayStoreUnavailable, outcome: mcpmetrics.IDJAGExchangeError},
		{name: "key unresolvable", reason: idjag.ReasonKeyUnresolvable, outcome: mcpmetrics.IDJAGExchangeError},
		{name: "trust unavailable", reason: idjag.ReasonTrustUnavailable, outcome: mcpmetrics.IDJAGExchangeError},
		{name: "subject unavailable", reason: idjag.ReasonSubjectUnavailable, outcome: mcpmetrics.IDJAGExchangeError},
		{name: "verifier misconfigured", reason: idjag.ReasonVerifierMisconfigured, outcome: mcpmetrics.IDJAGExchangeError},
		{name: "expired", reason: idjag.ReasonExpired, outcome: mcpmetrics.IDJAGExchangeRejected},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var logs bytes.Buffer
			base := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug, AddSource: false, ReplaceAttr: nil}))
			exchange, logger := (&Service{}).startIDJAGExchange(&ResolvedMcpEndpoint{UserSessionIssuerID: uuid.New()}, base)
			err := &idjag.Error{Reason: tc.reason, Err: errors.New("validator refused")}
			logger.InfoContext(t.Context(), "oauth ID-JAG token request rejected", attrsToArgs(exchange.rejectAssertion(err))...)
			exchange.record(t.Context())

			require.Equal(t, tc.outcome, exchange.outcome)
			require.Equal(t, mcpmetrics.IDJAGRejectionReason(tc.reason), exchange.reason)
			lines := idJAGExchangeLogLines(t, &logs)
			require.Len(t, lines, 1)
			require.Equal(t, string(tc.outcome), lines[0][string(attr.OAuthIDJAGOutcomeKey)])
			require.Equal(t, string(tc.reason), lines[0][string(attr.OAuthIDJAGRejectionReasonKey)])
		})
	}
}

func attrsToArgs(attrs []slog.Attr) []any {
	args := make([]any, 0, len(attrs))
	for _, a := range attrs {
		args = append(args, a)
	}
	return args
}
