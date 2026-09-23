package anthropicinference

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// unscannedBlockCounts collects the recorded coverage counter, keyed by the
// block type and skip reason it was recorded with.
func unscannedBlockCounts(t *testing.T, reader *sdkmetric.ManualReader) map[attribute.Set]int64 {
	t.Helper()
	var collected metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &collected))
	counts := map[attribute.Set]int64{}
	for _, scope := range collected.ScopeMetrics {
		for _, recorded := range scope.Metrics {
			if recorded.Name != meterUnscannedBlocks {
				continue
			}
			sum, ok := recorded.Data.(metricdata.Sum[int64])
			require.True(t, ok, "unscanned block instrument must be an int64 counter")
			for _, point := range sum.DataPoints {
				counts[point.Attributes] = point.Value
			}
		}
	}
	return counts
}

func TestPolicyInputsReportUndecodedBlockTypes(t *testing.T) {
	t.Parallel()
	inputs, unscanned, err := policyInputs([]Message{
		{Role: "user", Content: json.RawMessage(`[{"type":"image","source":{"data":"secret-pixels"}},{"type":"text","text":"prompt"}]`)},
		{Role: "assistant", Content: json.RawMessage(`[{"type":"thinking","thinking":"secret reasoning"},{"type":"image","source":{"data":"more-pixels"}}]`)},
	})
	require.NoError(t, err)
	require.Equal(t, []policyInput{{kind: message.User, tool: "", text: "prompt", toolCallID: ""}}, inputs)
	require.Equal(t, coverage{
		{blockType: "image", reason: reasonUnsupportedType}:    2,
		{blockType: "thinking", reason: reasonUnsupportedType}: 1,
	}, unscanned)
}

func TestPolicyInputsReportDecodedBlocksWithoutScannableText(t *testing.T) {
	t.Parallel()
	inputs, unscanned, err := policyInputs([]Message{
		{Role: "user", Content: json.RawMessage(`[{"type":"text","text":""},{"type":"tool_result","tool_use_id":"call-1","content":""}]`)},
	})
	require.Empty(t, inputs)
	require.NoError(t, err)
	require.Equal(t, coverage{
		{blockType: "text", reason: reasonNoScannableText}:        1,
		{blockType: "tool_result", reason: reasonNoScannableText}: 1,
	}, unscanned)
}

// The counter is the coverage view on the enforcement dashboard: an unscanned
// block has to be countable by type before a decision to scan it can be made.
func TestServiceCountsUnscannedContentBlocks(t *testing.T) {
	t.Parallel()
	reader := sdkmetric.NewManualReader()
	service := &Service{
		logger:  testenv.NewLogger(t),
		store:   &memoryStore{saved: nil, userID: "user-example", err: nil},
		scanner: &recordingScanner{},
		metrics: newCoverageMetrics(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)), testenv.NewLogger(t)),
	}
	frame := exampleFrame()
	frame.Messages = []Message{
		{Role: "user", Content: json.RawMessage(`[{"type":"text","text":"look at this"},{"type":"image","source":{"data":"pixels"}},{"type":"document","source":{"data":"pdf"}}]`)},
	}
	_, err := service.Process(t.Context(), Config{ID: "example", OrganizationID: "org_example", ProjectID: uuid.New(), TenantID: "tenant-example", SigningSecrets: nil}, frame)
	require.NoError(t, err)

	counts := unscannedBlockCounts(t, reader)
	require.Len(t, counts, 2)
	require.Equal(t, int64(1), counts[attribute.NewSet(
		attr.InferenceContentBlockType("image"),
		attr.InferenceContentBlockSkipReason(reasonUnsupportedType),
	)])
	require.Equal(t, int64(1), counts[attribute.NewSet(
		attr.InferenceContentBlockType("document"),
		attr.InferenceContentBlockSkipReason(reasonUnsupportedType),
	)])
}

// Block types arrive inside a customer transcript, so the counter keeps a
// fixed set of series and the exact name stays on the log line.
func TestServiceBucketsUnrecognizedBlockTypesAndNamesThemInLogs(t *testing.T) {
	t.Parallel()
	var logged bytes.Buffer
	reader := sdkmetric.NewManualReader()
	service := &Service{
		logger:  slog.New(slog.NewJSONHandler(&logged, nil)),
		store:   &memoryStore{saved: nil, userID: "user-example", err: nil},
		scanner: &recordingScanner{},
		metrics: newCoverageMetrics(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)), testenv.NewLogger(t)),
	}
	frame := exampleFrame()
	frame.Messages = []Message{
		{Role: "user", Content: json.RawMessage(`[{"type":"holographic_memo","text":"classified contents"}]`)},
	}
	_, err := service.Process(t.Context(), Config{ID: "example", OrganizationID: "org_example", ProjectID: uuid.New(), TenantID: "tenant-example", SigningSecrets: nil}, frame)
	require.NoError(t, err)

	require.Equal(t, int64(1), unscannedBlockCounts(t, reader)[attribute.NewSet(
		attr.InferenceContentBlockType(otherBlockType),
		attr.InferenceContentBlockSkipReason(reasonUnsupportedType),
	)])

	var event map[string]any
	require.NoError(t, json.Unmarshal(logged.Bytes(), &event))
	require.Equal(t, "holographic_memo", event[string(attr.InferenceContentBlockTypeKey)])
	require.Equal(t, reasonUnsupportedType, event[string(attr.InferenceContentBlockSkipReasonKey)])
	require.EqualValues(t, 1, event[string(attr.InferenceUnscannedBlockCountKey)])
	require.NotContains(t, logged.String(), "classified contents")
}

// History an accepted checkpoint covers is not rescanned, so counting it again
// on every later turn would make a single image look like recurring traffic.
func TestServiceCountsUnscannedBlocksOncePerDelivery(t *testing.T) {
	t.Parallel()
	reader := sdkmetric.NewManualReader()
	service := &Service{
		logger:  testenv.NewLogger(t),
		store:   &memoryStore{saved: nil, userID: "user-example", err: nil},
		scanner: &recordingScanner{},
		metrics: newCoverageMetrics(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)), testenv.NewLogger(t)),
	}
	config := Config{ID: "example", OrganizationID: "org_example", ProjectID: uuid.New(), TenantID: "tenant-example", SigningSecrets: nil}
	frame := exampleFrame()
	frame.Messages = []Message{
		{Role: "user", Content: json.RawMessage(`[{"type":"image","source":{"data":"pixels"}},{"type":"text","text":"look at this"}]`)},
		textMessage("assistant", "reply"),
		textMessage("user", "and this"),
	}
	_, err := service.Process(t.Context(), config, frame)
	require.NoError(t, err)

	frame.Messages = append(frame.Messages, textMessage("assistant", "second reply"), textMessage("user", "next"))
	_, err = service.Process(t.Context(), config, frame)
	require.NoError(t, err)

	require.Equal(t, int64(1), unscannedBlockCounts(t, reader)[attribute.NewSet(
		attr.InferenceContentBlockType("image"),
		attr.InferenceContentBlockSkipReason(reasonUnsupportedType),
	)])
}

// The whole path, from a signed delivery through storage to the verdict: an
// image block is archived and allowed, and the only trace that nothing scanned
// it is this counter and its log line.
func TestAttachReportsUnscannedContentBlocksOnSignedDelivery(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	reader := sdkmetric.NewManualReader()
	var logged bytes.Buffer
	service := NewService(slog.New(slog.NewJSONHandler(&logged, nil)), sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)), db, store.writer, &recordingScanner{})

	frame := exampleFrame()
	frame.TenantID = config.TenantID
	frame.Messages = []Message{
		{Role: "user", Content: json.RawMessage(`[{"type":"text","text":"what does this show?"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"pixels"}}]`)},
	}
	body, err := json.Marshal(frame)
	require.NoError(t, err)
	key := []byte("EXAMPLE-signing-secret")
	config.SigningSecrets = []string{"whsec_" + base64.StdEncoding.EncodeToString(key)}

	mux := goahttp.NewMuxer()
	Attach(mux, testenv.NewLogger(t), service, &testResolver{config: config, err: nil})
	request := httptest.NewRequest(http.MethodPost, "/hooks/anthropic-inference/example", bytes.NewReader(body))
	request.Header = signedHeaders(body, key, time.Now())
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.JSONEq(t, `{"action":"allow"}`, response.Body.String())
	require.Equal(t, int64(1), unscannedBlockCounts(t, reader)[attribute.NewSet(
		attr.InferenceContentBlockType("image"),
		attr.InferenceContentBlockSkipReason(reasonUnsupportedType),
	)])
	require.Contains(t, logged.String(), "inference content block not scanned")
	require.NotContains(t, logged.String(), "pixels")
}

// A transcript naming a new type per block must not turn into a log line per
// block.
func TestCoverageBoundsDistinctBlockTypesPerDelivery(t *testing.T) {
	t.Parallel()
	blocks := make([]string, 0, 100)
	for index := range 100 {
		blocks = append(blocks, fmt.Sprintf(`{"type":"future_%d"}`, index))
	}
	_, unscanned, err := policyInputs([]Message{
		{Role: "user", Content: json.RawMessage("[" + strings.Join(blocks, ",") + "]")},
	})
	require.NoError(t, err)
	require.Len(t, unscanned, maxTrackedTypes+1, "named types up to the cap, plus one aggregate entry")
	total := 0
	for _, count := range unscanned {
		total += count
	}
	require.Equal(t, 100, total, "folding a type into the aggregate must not lose its count")
	require.Equal(t, 100-maxTrackedTypes, unscanned[unscannedBlock{blockType: otherBlockType, reason: reasonUnsupportedType}])
}

func TestLoggedBlockTypeIsBounded(t *testing.T) {
	t.Parallel()
	require.Equal(t, "image", loggedBlockType("image"))
	require.Len(t, []rune(loggedBlockType(strings.Repeat("é", 400))), maxLoggedBlockType)
}
