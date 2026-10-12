package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func serveSpeakeasyAIHeaders(t *testing.T, header http.Header) http.Header {
	t.Helper()

	var got http.Header
	handler := SpeakeasyAIHeaders(testenv.NewMeterProvider(t))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
	}))
	req := httptest.NewRequest(http.MethodGet, "/rpc/x", nil)
	req.Header = header
	handler.ServeHTTP(httptest.NewRecorder(), req)
	return got
}

func TestSpeakeasyAIHeaders_NewNameReachesLegacyHeader(t *testing.T) {
	t.Parallel()

	for name, legacy := range SpeakeasyAIHeaderAliases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			header := http.Header{}
			header.Set(name, "new-value")
			got := serveSpeakeasyAIHeaders(t, header)

			require.Equal(t, "new-value", got.Get(legacy))
			require.Empty(t, got.Values(name), "the new name is folded into the legacy one")
		})
	}
}

func TestSpeakeasyAIHeaders_LegacyNameStillWorks(t *testing.T) {
	t.Parallel()

	for _, legacy := range SpeakeasyAIHeaderAliases {
		t.Run(legacy, func(t *testing.T) {
			t.Parallel()

			header := http.Header{}
			header.Set(legacy, "legacy-value")
			got := serveSpeakeasyAIHeaders(t, header)

			require.Equal(t, "legacy-value", got.Get(legacy))
		})
	}
}

func TestSpeakeasyAIHeaders_NewNameWins(t *testing.T) {
	t.Parallel()

	for name, legacy := range SpeakeasyAIHeaderAliases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			header := http.Header{}
			header.Set(legacy, "legacy-value")
			header.Set(name, "new-value")
			got := serveSpeakeasyAIHeaders(t, header)

			require.Equal(t, []string{"new-value"}, got.Values(legacy))
		})
	}
}

func TestSpeakeasyAIHeaders_EmptyNewNameKeepsLegacyValue(t *testing.T) {
	t.Parallel()

	for name, legacy := range SpeakeasyAIHeaderAliases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			header := http.Header{}
			header.Set(legacy, "legacy-value")
			header.Set(name, " ")
			got := serveSpeakeasyAIHeaders(t, header)

			require.Equal(t, []string{"legacy-value"}, got.Values(legacy))
			require.Empty(t, got.Values(name))
		})
	}
}

func TestSpeakeasyAIHeaders_NamesAreCanonical(t *testing.T) {
	t.Parallel()

	for name, legacy := range SpeakeasyAIHeaderAliases {
		require.True(t, strings.HasPrefix(name, "Speakeasy-AI-"), name)
		require.Equal(t, http.CanonicalHeaderKey(legacy), legacy)
	}
}

func TestCORSMiddleware_AllowsSpeakeasyAIHeaders(t *testing.T) {
	t.Parallel()

	handler := CORSMiddleware("prod", "https://app.getgram.ai", nil, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	req := httptest.NewRequest(http.MethodOptions, "https://app.getgram.ai/rpc/x", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	allowed := rec.Header().Get("Access-Control-Allow-Headers")
	for name := range SpeakeasyAIHeaderAliases {
		require.Contains(t, allowed, name)
	}
}

func TestSpeakeasyAIHeaders_CountsWhichFormArrived(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	handler := SpeakeasyAIHeaders(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	for _, header := range []http.Header{
		{"Speakeasy-Ai-Key": {"new"}},
		{"Gram-Key": {"legacy"}},
		{"Gram-Key": {"legacy"}},
		{},
	} {
		req := httptest.NewRequest(http.MethodGet, "/rpc/x", nil)
		req.Header = header
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	counts := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != meterHeaderAlias {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			for _, dp := range sum.DataPoints {
				name, _ := dp.Attributes.Value(attr.HTTPHeaderAliasNameKey)
				form, _ := dp.Attributes.Value(attr.HTTPHeaderAliasFormKey)
				counts[name.AsString()+"/"+form.AsString()] = dp.Value
			}
		}
	}
	require.Equal(t, map[string]int64{
		"Speakeasy-AI-Key/speakeasy_ai": 1,
		"Speakeasy-AI-Key/gram":         2,
	}, counts)
}
