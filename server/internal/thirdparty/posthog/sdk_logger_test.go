package posthog

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

type capturedLog struct {
	Level string `json:"level"`
	Msg   string `json:"msg"`
}

func newCapturingSDKLogger(t *testing.T, level slog.Level) (*sdkLogger, *bytes.Buffer) {
	t.Helper()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: level, AddSource: false, ReplaceAttr: nil}))
	return &sdkLogger{logger: logger}, &buf
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
