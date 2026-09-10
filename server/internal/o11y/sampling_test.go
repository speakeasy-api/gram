package o11y

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	slogmulti "github.com/samber/slog-multi"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

const (
	samplingBucketKey    = string(attr.LogsSamplingBucketKey)
	samplingFailureGroup = "failure"
	samplingHTTPGroup    = "http"
	samplingInlineGroup  = ""
	samplingBranchKey    = "branch"
	samplingInheritedKey = "inherited"
	samplingRecordKey    = "record"
	samplingWriterKey    = "writer"
)

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n, err := b.buf.Write(p)
	if err != nil {
		return n, fmt.Errorf("capture log output: %w", err)
	}
	return n, nil
}

func (b *lockedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return bytes.Clone(b.buf.Bytes())
}

func newCaptureHandler(buf *lockedBuffer) slog.Handler {
	return slog.NewJSONHandler(buf, &slog.HandlerOptions{
		AddSource:   false,
		Level:       slog.LevelDebug,
		ReplaceAttr: nil,
	})
}

func capturedRecords(t *testing.T, buf *lockedBuffer) []map[string]any {
	t.Helper()

	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	if len(lines) == 1 && len(lines[0]) == 0 {
		return nil
	}

	records := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		var record map[string]any
		require.NoError(t, json.Unmarshal(line, &record))
		records = append(records, record)
	}
	return records
}

func capturedMessages(t *testing.T, buf *lockedBuffer) []string {
	t.Helper()

	records := capturedRecords(t, buf)
	messages := make([]string, 0, len(records))
	for _, record := range records {
		message, ok := record[slog.MessageKey].(string)
		require.True(t, ok)
		messages = append(messages, message)
	}
	return messages
}

func TestSamplingHandlerExhaustsBurstAndResetsAfterMinute(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var output lockedBuffer
		logger := slog.New(newSamplingHandler(newCaptureHandler(&output), 0))

		for range 11 {
			logger.InfoContext(t.Context(), "successful response", attr.SlogLogsSamplingBucket(attr.LogsSamplingBucketHTTPResponseSuccess))
		}
		require.Len(t, capturedRecords(t, &output), 10)

		time.Sleep(time.Minute) //nolint:forbidigo // Advances the fake clock inside the synctest bubble.
		logger.InfoContext(t.Context(), "after reset", attr.SlogLogsSamplingBucket(attr.LogsSamplingBucketHTTPResponseSuccess))

		messages := capturedMessages(t, &output)
		require.Len(t, messages, 11)
		for _, message := range messages[:10] {
			require.Equal(t, "successful response", message)
		}
		require.Equal(t, "after reset", messages[10])
	})
}

func TestSamplingHandlerRetainsRecordsWithoutOneValidKnownMarker(t *testing.T) {
	t.Parallel()

	var output lockedBuffer
	logger := slog.New(newSamplingHandler(newCaptureHandler(&output), 0))
	for range 11 {
		logger.InfoContext(t.Context(), "sampled", attr.SlogLogsSamplingBucket(attr.LogsSamplingBucketHTTPResponseSuccess))
	}

	logger.InfoContext(t.Context(), "unmarked")
	logger.InfoContext(t.Context(), "empty marker", attr.SlogLogsSamplingBucket(""))
	logger.InfoContext(t.Context(), "unknown marker", attr.SlogLogsSamplingBucket("http.response.redirect"))
	logger.InfoContext(t.Context(), "invalid marker", slog.Int(samplingBucketKey, 1))
	logger.InfoContext(t.Context(), "conflicting markers",
		attr.SlogLogsSamplingBucket(attr.LogsSamplingBucketHTTPResponseSuccess),
		attr.SlogLogsSamplingBucket("http.response.redirect"),
	)

	messages := capturedMessages(t, &output)
	require.Len(t, messages, 15)
	require.Equal(t, []string{
		"unmarked", "empty marker", "unknown marker", "invalid marker", "conflicting markers",
	}, messages[len(messages)-5:])
}

func TestSamplingHandlerBypassesFailuresAndWarningsWithoutUsingBurst(t *testing.T) {
	t.Parallel()

	var output lockedBuffer
	logger := slog.New(newSamplingHandler(newCaptureHandler(&output), 0))
	marker := attr.SlogLogsSamplingBucket(attr.LogsSamplingBucketHTTPResponseSuccess)

	logger.With(attr.SlogErrorMessage("")).InfoContext(t.Context(), "empty inherited error", marker)
	logger.InfoContext(t.Context(), "record error", marker, attr.SlogError(errors.New("boom")))
	logger.WarnContext(t.Context(), "warning", marker)
	logger.ErrorContext(t.Context(), "error level", marker)

	for range 11 {
		logger.InfoContext(t.Context(), "clean success", marker)
	}

	messages := capturedMessages(t, &output)
	require.Len(t, messages, 14)
	require.Equal(t, []string{
		"empty inherited error", "record error", "warning", "error level",
	}, messages[:4])
	require.Equal(t, 10, bytes.Count(output.Bytes(), []byte(`"msg":"clean success"`)))
}

type samplingBucketValue string

func (v samplingBucketValue) LogValue() slog.Value {
	return slog.StringValue(string(v))
}

func TestSamplingHandlerFindsMarkersAcrossLoggerMetadata(t *testing.T) {
	t.Parallel()

	var output lockedBuffer
	logger := slog.New(newSamplingHandler(newCaptureHandler(&output), 0))
	marker := attr.SlogLogsSamplingBucket(attr.LogsSamplingBucketHTTPResponseSuccess)

	logger.With(slog.String(samplingBranchKey, "left")).InfoContext(t.Context(), "left", marker)
	logger.With(slog.String(samplingBranchKey, "right")).InfoContext(t.Context(), "right", marker)
	for range 8 {
		logger.InfoContext(t.Context(), "fill", marker)
	}

	logger.With(marker).InfoContext(t.Context(), "inherited marker")
	logger.WithGroup("").With(marker).InfoContext(t.Context(), "empty group")
	logger.With(slog.String(samplingBranchKey, "plain")).InfoContext(t.Context(), "unmarked sibling")
	logger.InfoContext(t.Context(), "duplicate marker", marker, marker)

	records := capturedRecords(t, &output)
	require.Len(t, records, 11)
	require.Equal(t, "left", records[0]["branch"])
	require.Equal(t, "right", records[1]["branch"])
	require.Equal(t, "plain", records[10]["branch"])
	messages := capturedMessages(t, &output)
	require.Equal(t, []string{"left", "right"}, messages[:2])
	for _, message := range messages[2:10] {
		require.Equal(t, "fill", message)
	}
	require.Equal(t, "unmarked sibling", messages[10])
}

func TestSamplingHandlerRetainsGroupsWithoutInspectingThem(t *testing.T) {
	t.Parallel()

	var output lockedBuffer
	logger := slog.New(newSamplingHandler(newCaptureHandler(&output), 0))
	marker := attr.SlogLogsSamplingBucket(attr.LogsSamplingBucketHTTPResponseSuccess)
	for range 11 {
		logger.InfoContext(t.Context(), "fill", marker)
	}

	logger.InfoContext(t.Context(), "nested marker", slog.Group(samplingHTTPGroup, marker))
	logger.InfoContext(t.Context(), "inline marker", slog.Group(samplingInlineGroup, marker))
	logger.InfoContext(t.Context(), "grouped error", marker, slog.Group(samplingFailureGroup, attr.SlogError(errors.New("boom"))))
	logger.With(slog.Group(samplingHTTPGroup, marker)).InfoContext(t.Context(), "inherited group")
	logger.WithGroup("request").With(marker).InfoContext(t.Context(), "group before marker")
	logger.With(marker).WithGroup("request").InfoContext(t.Context(), "group after marker")
	logger.WithGroup("request").InfoContext(t.Context(), "grouped record", marker)
	logger.InfoContext(t.Context(), "valuer marker", slog.Any(samplingBucketKey, samplingBucketValue(attr.LogsSamplingBucketHTTPResponseSuccess)))

	messages := capturedMessages(t, &output)
	require.Len(t, messages, 18)
	require.Equal(t, []string{
		"nested marker", "inline marker", "grouped error", "inherited group",
		"group before marker", "group after marker", "grouped record", "valuer marker",
	}, messages[10:])
}

type changingSamplingValue struct {
	calls int
}

func (v *changingSamplingValue) LogValue() slog.Value {
	v.calls++
	if v.calls > 1 {
		return slog.StringValue("changed")
	}
	return slog.GroupValue(attr.SlogLogsSamplingBucket(attr.LogsSamplingBucketHTTPResponseSuccess))
}

func TestSamplingHandlerRetainsValuersWithoutDoubleEvaluation(t *testing.T) {
	t.Parallel()

	var output lockedBuffer
	logger := slog.New(newSamplingHandler(newCaptureHandler(&output), 0))
	for range 11 {
		logger.InfoContext(t.Context(), "fill", attr.SlogLogsSamplingBucket(attr.LogsSamplingBucketHTTPResponseSuccess))
	}
	inherited := &changingSamplingValue{}
	record := &changingSamplingValue{}
	logger.With(slog.Any(samplingInheritedKey, inherited)).
		InfoContext(t.Context(), "resolved", slog.Any(samplingRecordKey, record))
	direct := &changingSamplingValue{}
	logger.InfoContext(t.Context(), "direct valuer",
		attr.SlogLogsSamplingBucket(attr.LogsSamplingBucketHTTPResponseSuccess),
		slog.Any(samplingRecordKey, direct),
	)

	events := capturedRecords(t, &output)
	require.Len(t, events, 12)
	require.Equal(t, 1, inherited.calls)
	require.Equal(t, 1, record.calls)
	require.Equal(t, 1, direct.calls)
	require.Equal(t, map[string]any{
		samplingBucketKey: attr.LogsSamplingBucketHTTPResponseSuccess,
	}, events[10]["inherited"])
	require.Equal(t, map[string]any{
		samplingBucketKey: attr.LogsSamplingBucketHTTPResponseSuccess,
	}, events[10]["record"])
	require.Equal(t, map[string]any{
		samplingBucketKey: attr.LogsSamplingBucketHTTPResponseSuccess,
	}, events[11]["record"])
}

type failingHandler struct {
	err error
}

func (h *failingHandler) Enabled(context.Context, slog.Level) bool {
	return true
}

func (h *failingHandler) Handle(context.Context, slog.Record) error {
	return h.err
}

func (h *failingHandler) WithAttrs([]slog.Attr) slog.Handler {
	return h
}

func (h *failingHandler) WithGroup(string) slog.Handler {
	return h
}

func TestSamplingHandlerPropagatesUnderlyingHandleError(t *testing.T) {
	t.Parallel()

	sinkErr := errors.New("sink failed")
	handler := newSamplingHandler(&failingHandler{err: sinkErr}, 1)
	record := slog.NewRecord(time.Now(), slog.LevelInfo, "successful response", 0)
	record.AddAttrs(attr.SlogLogsSamplingBucket(attr.LogsSamplingBucketHTTPResponseSuccess))

	require.ErrorIs(t, handler.Handle(t.Context(), record), sinkErr)
}

func TestSamplingHandlerLeavesIndependentFanoutSinkUnsampled(t *testing.T) {
	t.Parallel()

	var sampledOutput lockedBuffer
	var independentOutput lockedBuffer
	logger := slog.New(slogmulti.Fanout(
		newSamplingHandler(newCaptureHandler(&sampledOutput), 0),
		newCaptureHandler(&independentOutput),
	))

	for range 11 {
		logger.InfoContext(t.Context(), "successful response", attr.SlogLogsSamplingBucket(attr.LogsSamplingBucketHTTPResponseSuccess))
	}

	require.Len(t, capturedRecords(t, &sampledOutput), 10)
	require.Len(t, capturedRecords(t, &independentOutput), 11)
}

func TestSamplingHandlerHandlesConcurrentRecords(t *testing.T) {
	t.Parallel()

	const (
		writers          = 16
		recordsPerWriter = 64
	)

	var output lockedBuffer
	logger := slog.New(newSamplingHandler(newCaptureHandler(&output), 1))
	marker := attr.SlogLogsSamplingBucket(attr.LogsSamplingBucketHTTPResponseSuccess)

	var wg sync.WaitGroup
	for writer := range writers {
		wg.Go(func() {
			for record := range recordsPerWriter {
				logger.InfoContext(t.Context(), "concurrent success", marker, slog.Int(samplingWriterKey, writer), slog.Int(samplingRecordKey, record))
			}
		})
	}
	wg.Wait()

	require.Len(t, capturedRecords(t, &output), writers*recordsPerWriter)
}
