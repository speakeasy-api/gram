package o11y_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"testing/slogtest"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// captureLogger returns a logger writing JSON records into buf, so a test can
// assert on the level, message, and attributes a helper emits.
func captureLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(&o11y.ContextHandler{
		Handler: slog.NewJSONHandler(buf, &slog.HandlerOptions{
			AddSource:   false,
			Level:       slog.LevelDebug,
			ReplaceAttr: nil,
		}),
		DataDogAttr: false,
	})
}

func TestContextHandlerComponentOverridePreservesParent(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	parent := captureLogger(&buf).With(attr.SlogComponent("server"), attr.SlogOrganizationID("org_example"))
	child := parent.With(attr.SlogComponent("intermediate"), attr.SlogComponent("child"))
	child.WarnContext(t.Context(), "child log")
	parent.WarnContext(t.Context(), "parent log")

	decoder := json.NewDecoder(&buf)
	for _, component := range []string{"child", "server"} {
		var raw json.RawMessage
		require.NoError(t, decoder.Decode(&raw))
		require.Equal(t, 1, bytes.Count(raw, []byte(`"`+string(attr.ComponentKey)+`":`)))

		var record map[string]any
		require.NoError(t, json.Unmarshal(raw, &record))
		require.Equal(t, component, record[string(attr.ComponentKey)])
		require.Equal(t, "org_example", record[string(attr.OrganizationIDKey)])
	}
	require.False(t, decoder.More())
}

func TestContextHandlerRecordComponentOverride(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := captureLogger(&buf).With(attr.SlogComponent("server")).With(attr.SlogComponent("child"))
	logger.WarnContext(t.Context(), "record log", attr.SlogComponent("intermediate"), attr.SlogComponent("record"))
	logger.WarnContext(t.Context(), "child log")

	decoder := json.NewDecoder(&buf)
	for _, component := range []string{"record", "child"} {
		var raw json.RawMessage
		require.NoError(t, decoder.Decode(&raw))
		require.Equal(t, 1, bytes.Count(raw, []byte(`"`+string(attr.ComponentKey)+`":`)))

		var record map[string]any
		require.NoError(t, json.Unmarshal(raw, &record))
		require.Equal(t, component, record[string(attr.ComponentKey)])
	}
	require.False(t, decoder.More())
}

func TestContextHandlerKeepsComponentGroupScope(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := captureLogger(&buf).With(attr.SlogComponent("server")).WithGroup("request").
		With(attr.SlogComponent("group"), attr.SlogOrganizationID("org_example")).
		With(attr.SlogComponent("child"))
	logger.WarnContext(t.Context(), "group log")
	require.Equal(t, 2, bytes.Count(buf.Bytes(), []byte(`"`+string(attr.ComponentKey)+`":`)))

	var record map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(buf.Bytes(), &record))
	require.JSONEq(t, `"server"`, string(record[string(attr.ComponentKey)]))
	require.JSONEq(t, `{"gram.component":"child","gram.org.id":"org_example"}`, string(record["request"]))
}

func TestContextHandlerEmptyGroupKeepsComponentOverride(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := captureLogger(&buf).With(attr.SlogComponent("server")).WithGroup("").With(attr.SlogComponent("child"))
	logger.WarnContext(t.Context(), "child log")
	require.Equal(t, 1, bytes.Count(buf.Bytes(), []byte(`"`+string(attr.ComponentKey)+`":`)))

	var record map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &record))
	require.Equal(t, "child", record[string(attr.ComponentKey)])
}

func TestContextHandlerSlogContract(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	err := slogtest.TestHandler(captureLogger(&buf).Handler(), func() []map[string]any {
		var records []map[string]any
		decoder := json.NewDecoder(&buf)
		for decoder.More() {
			var record map[string]any
			require.NoError(t, decoder.Decode(&record))
			records = append(records, record)
		}
		return records
	})
	require.NoError(t, err)
}

func TestLogDefer_Success_LogsNothing(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	err := o11y.LogDefer(t.Context(), captureLogger(&buf), "failed to close widget", func() error {
		return nil
	})

	require.NoError(t, err)
	require.Empty(t, buf.String(), "a successful deferred call must not log")
}

func TestLogDefer_Failure_LogsMessageAndError(t *testing.T) {
	t.Parallel()

	cbErr := errors.New("boom")

	var buf bytes.Buffer
	err := o11y.LogDefer(t.Context(), captureLogger(&buf), "failed to close widget", func() error {
		return cbErr
	})

	require.ErrorIs(t, err, cbErr, "LogDefer must return the callback error unchanged")

	var record map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &record))

	// The caller-supplied message is what makes the record diagnosable. Before
	// AIM-160 this was the literal string "error" for every callsite, which
	// rendered the health digest's top error type meaningless.
	require.Equal(t, "failed to close widget", record["msg"])
	require.Equal(t, slog.LevelError.String(), record["level"])
	require.Equal(t, "boom", record["error.message"])
}
