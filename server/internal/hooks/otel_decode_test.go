package hooks

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	srv "github.com/speakeasy-api/gram/server/gen/http/hooks/server"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// These tests exercise the OTLP/JSON decode path end-to-end without spinning
// up the full hooks service. They guard the two shapes that real producers
// emit and have diverged on in production:
//
//   - Claude Code's own exporter ships int64 as raw JSON numbers
//     ({"asInt": 12345}, {"intValue": 67890}).
//   - The OpenTelemetry Collector re-serializes through protobuf and emits
//     canonical OTLP/JSON ({"asInt": "12345"}, {"intValue": "67890"}), and
//     defaults to gzip compression on the wire.
//
// Both must round-trip through the decoder and extractor.

func TestParseLooseInt64(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   any
		want int64
		ok   bool
	}{
		{"raw json number", float64(12345), 12345, true},
		{"canonical otlp string", "12345", 12345, true},
		{"json.Number", json.Number("12345"), 12345, true},
		{"go int", int(7), 7, true},
		{"go int64", int64(8), 8, true},
		{"nil", nil, 0, false},
		{"empty string", "", 0, false},
		{"non-integral float", float64(1.5), 0, false},
		{"garbage string", "not-a-number", 0, false},
		{"bool rejected", true, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := parseLooseInt64(tc.in)
			assert.Equal(t, tc.ok, ok)
			if tc.ok {
				assert.Equal(t, tc.want, got)
			}
		})
	}
}

func TestIsDeltaTemporality(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   any
		want bool
	}{
		{"raw number 1 (claude direct)", float64(1), true},
		{"string '1'", "1", true},
		{"enum string DELTA", "AGGREGATION_TEMPORALITY_DELTA", true},
		{"json.Number 1", json.Number("1"), true},
		{"int 1", 1, true},
		{"raw number 2 cumulative", float64(2), false},
		{"enum string CUMULATIVE", "AGGREGATION_TEMPORALITY_CUMULATIVE", false},
		{"nil", nil, false},
		{"unspecified zero", float64(0), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, isDeltaTemporality(tc.in))
		})
	}
}

// buildMetricsBody returns an OTLP/JSON metrics body with token + cost data
// points. asIntEncoder controls how int64 fields and aggregationTemporality
// are serialized — pass numberEncoded for Claude-direct shape and
// stringEncoded for canonical OTLP/JSON.
type encoding int

const (
	numberEncoded encoding = iota
	stringEncoded
)

func buildMetricsBody(t *testing.T, enc encoding) []byte {
	t.Helper()

	intVal := func(n int64) any {
		if enc == stringEncoded {
			return jsonNumString(n)
		}
		return n
	}
	temporality := func() any {
		if enc == stringEncoded {
			return "AGGREGATION_TEMPORALITY_DELTA"
		}
		return 1
	}

	body := map[string]any{
		"resourceMetrics": []any{
			map[string]any{
				"resource": map[string]any{
					"attributes": []any{
						map[string]any{
							"key":   "service.name",
							"value": map[string]any{"stringValue": "claude-code"},
						},
					},
				},
				"scopeMetrics": []any{
					map[string]any{
						"scope": map[string]any{"name": "claude-code", "version": "1.0"},
						"metrics": []any{
							map[string]any{
								"name": "claude_code.token.usage",
								"sum": map[string]any{
									"aggregationTemporality": temporality(),
									"isMonotonic":            true,
									"dataPoints": []any{
										map[string]any{
											"attributes": []any{
												map[string]any{"key": "session.id", "value": map[string]any{"stringValue": "sess-1"}},
												map[string]any{"key": "model", "value": map[string]any{"stringValue": "claude-opus-4"}},
												map[string]any{"key": "user.email", "value": map[string]any{"stringValue": "u@example.com"}},
												map[string]any{"key": "type", "value": map[string]any{"stringValue": "input"}},
											},
											"timeUnixNano": "1700000000000000000",
											"asInt":        intVal(1234),
										},
										map[string]any{
											"attributes": []any{
												map[string]any{"key": "session.id", "value": map[string]any{"stringValue": "sess-1"}},
												map[string]any{"key": "model", "value": map[string]any{"stringValue": "claude-opus-4"}},
												map[string]any{"key": "user.email", "value": map[string]any{"stringValue": "u@example.com"}},
												map[string]any{"key": "type", "value": map[string]any{"stringValue": "output"}},
											},
											"timeUnixNano": "1700000000000000000",
											"asInt":        intVal(567),
										},
									},
								},
							},
							map[string]any{
								"name": "claude_code.cost.usage",
								"sum": map[string]any{
									"aggregationTemporality": temporality(),
									"dataPoints": []any{
										map[string]any{
											"attributes": []any{
												map[string]any{"key": "session.id", "value": map[string]any{"stringValue": "sess-1"}},
												map[string]any{"key": "model", "value": map[string]any{"stringValue": "claude-opus-4"}},
											},
											"timeUnixNano": "1700000000000000000",
											"asDouble":     0.0125,
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	raw, err := json.Marshal(body)
	require.NoError(t, err)
	return raw
}

// jsonNumString lets us emit "12345" instead of 12345 in a map literal by
// pre-marshaling. We can't just stringify because json.Marshal would quote
// the string again — but a json.RawMessage stays verbatim.
func jsonNumString(n int64) any {
	return json.RawMessage([]byte(`"` + itoa(n) + `"`))
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func gzipBytes(t *testing.T, in []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	_, err := gz.Write(in)
	require.NoError(t, err)
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

// decodeMetricsBody runs the bytes through the production decoder factory
// exactly as the goa-generated DecodeMetricsRequest does, then runs the
// generated validator. Returns the validated request body or an error.
func decodeMetricsBody(t *testing.T, body []byte, contentEncoding string) (*srv.MetricsRequestBody, error) {
	t.Helper()

	r := httptest.NewRequest(http.MethodPost, "/rpc/hooks.otel/v1/metrics", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if contentEncoding != "" {
		r.Header.Set("Content-Encoding", contentEncoding)
	}

	dec := newHooksRequestDecoder(testenv.NewLogger(t))(r)
	var out srv.MetricsRequestBody
	if err := dec.Decode(&out); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if err := srv.ValidateMetricsRequestBody(&out); err != nil {
		return nil, fmt.Errorf("validate: %w", err)
	}
	return &out, nil
}

func TestHooksRequestDecoder_AcceptsBothOTLPShapes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		enc         encoding
		contentEnc  string // "" or "gzip"
		gzipPayload bool
	}{
		{"raw-number (claude direct), plain", numberEncoded, "", false},
		{"raw-number, gzipped", numberEncoded, "gzip", true},
		{"canonical OTLP/JSON (collector), plain", stringEncoded, "", false},
		{"canonical OTLP/JSON, gzipped", stringEncoded, "gzip", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			body := buildMetricsBody(t, tc.enc)
			if tc.gzipPayload {
				body = gzipBytes(t, body)
			}

			req, err := decodeMetricsBody(t, body, tc.contentEnc)
			require.NoError(t, err, "decode + validate should succeed")
			require.NotNil(t, req)
			require.Len(t, req.ResourceMetrics, 1)
			require.Len(t, req.ResourceMetrics[0].ScopeMetrics, 1)
		})
	}
}

func TestHooksRequestDecoder_MalformedBodyReturnsError(t *testing.T) {
	t.Parallel()

	_, err := decodeMetricsBody(t, []byte("not json at all"), "")
	require.Error(t, err)
}

func TestHooksRequestDecoder_GzipMagicMismatchReturnsError(t *testing.T) {
	t.Parallel()

	// Body claims gzip in headers but is plain JSON — gzip.NewReader should
	// reject it and we should surface a decode error rather than a panic.
	body := buildMetricsBody(t, numberEncoded)
	_, err := decodeMetricsBody(t, body, "gzip")
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "gzip")
}

// TestExtractMetricsForClickHouse_BothShapes runs the post-decode extractor
// against both real-world payload shapes and confirms aggregated token/cost
// totals are identical.
func TestExtractMetricsForClickHouse_BothShapes(t *testing.T) {
	t.Parallel()

	shapes := []struct {
		name string
		enc  encoding
	}{
		{"raw-number (claude direct)", numberEncoded},
		{"canonical OTLP/JSON", stringEncoded},
	}

	for _, tc := range shapes {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			body := buildMetricsBody(t, tc.enc)
			req, err := decodeMetricsBody(t, body, "")
			require.NoError(t, err)

			payload := srv.NewMetricsPayload(req, nil, nil)
			points, err := extractMetricsForClickHouse(payload)
			require.NoError(t, err)
			require.Len(t, points, 1)

			p := points[0]
			assert.Equal(t, "sess-1", p.SessionID)
			assert.Equal(t, "claude-opus-4", p.Model)
			assert.Equal(t, "u@example.com", p.UserEmail)
			assert.Equal(t, int64(1234), p.InputTokens)
			assert.Equal(t, int64(567), p.OutputTokens)
			assert.InDelta(t, 0.0125, p.Cost, 1e-9)
			assert.Equal(t, int64(1700000000000000000), p.TimestampNano)
		})
	}
}

// TestExtractMetricsForClickHouse_RejectsCumulative ensures we still bounce
// non-DELTA temporality regardless of which shape the temporality arrived in.
func TestExtractMetricsForClickHouse_RejectsCumulative(t *testing.T) {
	t.Parallel()

	for _, temporality := range []any{float64(2), "AGGREGATION_TEMPORALITY_CUMULATIVE", "2"} {
		t.Run("temporality_"+stringify(temporality), func(t *testing.T) {
			t.Parallel()

			body := buildMetricsBody(t, numberEncoded)
			// Surgically rewrite temporality in the encoded JSON.
			var generic map[string]any
			require.NoError(t, json.Unmarshal(body, &generic))
			rms, ok := generic["resourceMetrics"].([]any)
			require.True(t, ok)
			rm0, ok := rms[0].(map[string]any)
			require.True(t, ok)
			sms, ok := rm0["scopeMetrics"].([]any)
			require.True(t, ok)
			sm0, ok := sms[0].(map[string]any)
			require.True(t, ok)
			metrics, ok := sm0["metrics"].([]any)
			require.True(t, ok)
			for _, m := range metrics {
				mObj, ok := m.(map[string]any)
				require.True(t, ok)
				sum, ok := mObj["sum"].(map[string]any)
				require.True(t, ok)
				sum["aggregationTemporality"] = temporality
			}
			rewritten, err := json.Marshal(generic)
			require.NoError(t, err)

			req, err := decodeMetricsBody(t, rewritten, "")
			require.NoError(t, err, "design must accept non-DELTA temporality at decode time")

			payload := srv.NewMetricsPayload(req, nil, nil)
			_, err = extractMetricsForClickHouse(payload)
			require.Error(t, err)
		})
	}
}

func stringify(v any) string {
	switch t := v.(type) {
	case string:
		return t
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}

// Ensure context import isn't dropped if future changes pull from r.Context().
var _ = context.Background
