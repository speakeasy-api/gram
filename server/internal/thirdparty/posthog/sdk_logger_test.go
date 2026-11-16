package posthog

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

type capturedLog struct {
	Level     string `json:"level"`
	Msg       string `json:"msg"`
	Component string `json:"gram.component,omitempty"`
}

func newCapturingLogger(t *testing.T, level slog.Level) (*slog.Logger, *bytes.Buffer) {
	t.Helper()

	var buf bytes.Buffer
	return slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: level, AddSource: false, ReplaceAttr: nil})), &buf
}

func newCapturingSDKLogger(t *testing.T, level slog.Level) (*sdkLogger, *bytes.Buffer) {
	t.Helper()

	logger, buf := newCapturingLogger(t, level)
	return &sdkLogger{logger: logger}, buf
}

func decodeCapturedLogs(t *testing.T, buf *bytes.Buffer) []capturedLog {
	t.Helper()

	var logs []capturedLog
	decoder := json.NewDecoder(buf)
	for decoder.More() {
		var entry capturedLog
		require.NoError(t, decoder.Decode(&entry))
		logs = append(logs, entry)
	}
	return logs
}

func TestSDKLoggerLevels(t *testing.T) {
	t.Parallel()

	logger, buf := newCapturingSDKLogger(t, slog.LevelDebug)

	logger.Debugf("debug %s", "one")
	logger.Logf("info %s", "two")
	logger.Warnf("warn %s", "three")
	logger.Errorf("error %s", "four")

	require.Equal(t, []capturedLog{
		{Level: "DEBUG", Msg: "debug one"},
		{Level: "DEBUG", Msg: "info two"},
		{Level: "WARN", Msg: "warn three"},
		{Level: "ERROR", Msg: "error four"},
	}, decodeCapturedLogs(t, buf))
}

func TestSDKLoggerDemotesLocalEvaluationWarnings(t *testing.T) {
	t.Parallel()

	logger, buf := newCapturingSDKLogger(t, slog.LevelDebug)

	logger.Warnf("Unable to compute flag locally (%s) - %s", "some-flag", "Can't determine if feature flag is enabled or not with given properties")
	logger.Warnf("[FEATURE FLAGS] PostHog feature flags quota limited, resetting feature flag data.")

	require.Equal(t, []capturedLog{
		{Level: "DEBUG", Msg: "Unable to compute flag locally (some-flag) - Can't determine if feature flag is enabled or not with given properties"},
		{Level: "WARN", Msg: "[FEATURE FLAGS] PostHog feature flags quota limited, resetting feature flag data."},
	}, decodeCapturedLogs(t, buf))
}

func TestSDKLoggerKeepsFetchErrorsAtError(t *testing.T) {
	t.Parallel()

	logger, buf := newCapturingSDKLogger(t, slog.LevelDebug)

	logger.Errorf("Unable to fetch feature flags: %s", "connection refused")

	require.Equal(t, []capturedLog{
		{Level: "ERROR", Msg: "Unable to fetch feature flags: connection refused"},
	}, decodeCapturedLogs(t, buf))
}

// The SDK configuration must route diagnostics through the logger it is given
// rather than the SDK's default stderr logger, so entries keep the attributes
// (such as the component) that New attaches.
func TestNewSDKConfigRoutesSDKLogsThroughLogger(t *testing.T) {
	t.Parallel()

	logger, buf := newCapturingLogger(t, slog.LevelDebug)

	config := newSDKConfig(logger.With(attr.SlogComponent("posthog")), "https://posthog.example.test", "", nil)
	require.NotNil(t, config.Logger)
	require.Empty(t, config.PersonalApiKey)
	require.Zero(t, config.DefaultFeatureFlagsPollingInterval)

	config.Logger.Warnf("Unable to compute flag locally (%s) - %s", "some-flag", "reason")
	config.Logger.Errorf("Unable to fetch feature flags: %s", "connection refused")

	require.Equal(t, []capturedLog{
		{Level: "DEBUG", Msg: "Unable to compute flag locally (some-flag) - reason", Component: "posthog"},
		{Level: "ERROR", Msg: "Unable to fetch feature flags: connection refused", Component: "posthog"},
	}, decodeCapturedLogs(t, buf))
}

func TestNewSDKConfigEnablesLocalEvaluationWithPersonalAPIKey(t *testing.T) {
	t.Parallel()

	logger, _ := newCapturingLogger(t, slog.LevelDebug)

	config := newSDKConfig(logger, "https://posthog.example.test", "personal-api-key", &atomic.Int64{})
	require.Equal(t, "personal-api-key", config.PersonalApiKey)
	require.Equal(t, time.Minute, config.DefaultFeatureFlagsPollingInterval)
	require.NotNil(t, config.Logger)
}

// New tags its logger with the posthog component before handing it to the
// SDK, so every entry, including its own, is attributable in log aggregation.
func TestNewTagsLoggerWithComponent(t *testing.T) {
	t.Parallel()

	logger, buf := newCapturingLogger(t, slog.LevelDebug)

	provider := New(t.Context(), logger, "", "", "")
	require.True(t, provider.disabled)

	logs := decodeCapturedLogs(t, buf)
	require.Len(t, logs, 1)
	require.Equal(t, "posthog", logs[0].Component)
}

// Only a successful definitions fetch confirms the cached definitions; a
// failing poll leaves the last confirmation in place so its age grows.
func TestDefinitionsTransportRecordsSuccessfulDefinitionFetches(t *testing.T) {
	t.Parallel()

	status := atomic.Int32{}
	status.Store(http.StatusOK)
	body := atomic.Pointer[string]{}
	definitions, maintenance, incompatible := `{"flags":[]}`, `<html>maintenance</html>`, `{"flags":[{"key":"pause","active":"invalid"}]}`
	body.Store(&definitions)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(int(status.Load()))
		_, _ = w.Write([]byte(*body.Load()))
	}))
	t.Cleanup(server.Close)

	confirmedAt := &atomic.Int64{}
	provider := &Posthog{client: &evaluationClient{}, disabled: false, localEvaluation: true, logger: nil, definitionsConfirmedAt: confirmedAt}
	transport := &definitionsTransport{base: http.DefaultTransport, confirmedAt: confirmedAt}
	fetch := func(path string) {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+path, nil)
		require.NoError(t, err)
		res, err := transport.RoundTrip(req)
		require.NoError(t, err)
		require.NoError(t, res.Body.Close())
	}

	_, ok := provider.FlagDefinitionsConfirmedAt()
	require.False(t, ok, "definitions never fetched are not current")
	fetch("/flags/")
	_, ok = provider.FlagDefinitionsConfirmedAt()
	require.False(t, ok, "other endpoints confirm nothing")

	fetch("/api/feature_flag/local_evaluation")
	first, ok := provider.FlagDefinitionsConfirmedAt()
	require.True(t, ok)

	status.Store(http.StatusServiceUnavailable)
	fetch("/api/feature_flag/local_evaluation")
	after, ok := provider.FlagDefinitionsConfirmedAt()
	require.True(t, ok)
	require.Equal(t, first, after, "a failed poll never refreshes the confirmation")

	status.Store(http.StatusOK)
	body.Store(&maintenance)
	fetch("/api/feature_flag/local_evaluation")
	after, ok = provider.FlagDefinitionsConfirmedAt()
	require.True(t, ok)
	require.Equal(t, first, after, "a body the SDK can't load never refreshes the confirmation")

	body.Store(&incompatible)
	for range 3 {
		fetch("/api/feature_flag/local_evaluation")
	}
	after, ok = provider.FlagDefinitionsConfirmedAt()
	require.True(t, ok)
	require.Equal(t, first, after, "flags JSON the SDK's typed decode rejects never refreshes the confirmation, so it ages past the pause window")
	body.Store(&definitions)

	status.Store(http.StatusNotModified)
	fetch("/api/feature_flag/local_evaluation")
	refreshed, ok := provider.FlagDefinitionsConfirmedAt()
	require.True(t, ok)
	require.True(t, refreshed.After(first), "an unchanged response confirms the cached definitions")
}

func TestDefinitionsTransportRefusesOversizedDefinitions(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"flags":[],"pad":"`))
		_, _ = w.Write(bytes.Repeat([]byte("a"), maxDefinitionsBytes))
		_, _ = w.Write([]byte(`"}`))
	}))
	t.Cleanup(server.Close)
	confirmedAt := &atomic.Int64{}
	transport := &definitionsTransport{base: http.DefaultTransport, confirmedAt: confirmedAt}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/api/feature_flag/local_evaluation", nil)
	require.NoError(t, err)
	res, err := transport.RoundTrip(req) //nolint:bodyclose // Refused before a body is returned.
	require.Error(t, err)
	require.Nil(t, res)
	require.Zero(t, confirmedAt.Load())
}
