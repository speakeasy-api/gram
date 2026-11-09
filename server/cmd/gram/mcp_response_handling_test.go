package gram

import (
	"bytes"
	"context"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/klauspost/compress/gzip"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
	goahttp "goa.design/goa/v3/http"

	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// capturingMeterPublisher records meter readings so a test can read billed bytes.
type capturingMeterPublisher struct {
	mu       sync.Mutex
	messages []*meteringv1.MeterReading
}

func (p *capturingMeterPublisher) Publish(_ context.Context, message *meteringv1.MeterReading, _ ...gcp.PublishOption) gcp.PublishResult {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.messages = append(p.messages, message)
	return publishedResult{}
}

func (p *capturingMeterPublisher) Stop(context.Context) error { return nil }

func (p *capturingMeterPublisher) published() []*meteringv1.MeterReading {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]*meteringv1.MeterReading(nil), p.messages...)
}

// publishedResult reports an immediately successful publish.
type publishedResult struct{}

func (publishedResult) Ready() <-chan struct{} {
	ready := make(chan struct{})
	close(ready)
	return ready
}

func (publishedResult) Get(context.Context) (string, error) { return "id", nil }

// newResponseHandlingTestMux builds the real MCP listener chain.
func newResponseHandlingTestMux(t *testing.T, publisher *capturingMeterPublisher) goahttp.Muxer {
	t.Helper()

	infra, cleanup, err := testenv.Launch(t.Context(), testenv.LaunchOptions{Postgres: true})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cleanup()) })
	pool, err := infra.CloneTestDatabase(t, "mcp_response_handling")
	require.NoError(t, err)

	flags := flag.NewFlagSet("mcp-response-handling", flag.ContinueOnError)
	flags.String("server-url", "https://app.example.com", "")
	flags.String("site-url", "https://app.example.com", "")
	flags.String("environment", "prod", "")
	origin, err := url.Parse("https://app.example.com")
	require.NoError(t, err)
	host, err := mcp.NewAuthenticationHost("https://auth.example.com", origin, "prod")
	require.NoError(t, err)

	mux, err := newMCPServerMux(cli.NewContext(nil, flags, nil), testenv.NewLogger(t), pool, origin, host, nil, nil, &background.Publishers{MeterReadings: publisher})
	require.NoError(t, err)
	return mux
}

// The order of the mux.Use calls is the contract: compression outside the meter
// keeps billed egress at the uncompressed size, and swapping them would silently
// shrink every customer's metered bytes.
func TestMCPMuxMetersEgressBeforeCompression(t *testing.T) {
	t.Parallel()

	publisher := &capturingMeterPublisher{}
	mux := newResponseHandlingTestMux(t, publisher)

	body := `{"jsonrpc":"2.0","result":{"tools":[` + strings.Repeat(`{"name":"widget","description":"A widget tool."},`, 100) + `{}]}}`
	mux.Handle(http.MethodPost, "/mcp/response-handling", func(w http.ResponseWriter, r *http.Request) {
		metering.AttributeMCPBandwidth(r.Context(), "org-test", uuid.New())
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	})

	request := httptest.NewRequest(http.MethodPost, "https://app.example.com/mcp/response-handling", nil)
	request.Header.Set("Accept-Encoding", "gzip")
	request.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, request)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "gzip", rec.Header().Get(constants.HeaderContentEncoding))
	require.Less(t, rec.Body.Len(), len(body), "the wire body must actually be compressed")

	reader, err := gzip.NewReader(bytes.NewReader(rec.Body.Bytes()))
	require.NoError(t, err)
	decoded, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.JSONEq(t, body, string(decoded))

	var egress []int64
	for _, message := range publisher.published() {
		if message.GetMeterId() == string(metering.MeterMCPBandwidthEgress) {
			egress = append(egress, message.GetValue())
		}
	}
	require.Equal(t, []int64{int64(len(body))}, egress)
}

func TestMCPMuxRejectsOversizedChunkedUpload(t *testing.T) {
	t.Parallel()

	mux := newResponseHandlingTestMux(t, &capturingMeterPublisher{})

	var readErr error
	mux.Handle(http.MethodPost, "/mcp/response-handling", func(w http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusBadRequest)
	})

	upload := io.MultiReader(bytes.NewReader(make([]byte, 16*constants.MiB)))
	request := httptest.NewRequest(http.MethodPost, "https://app.example.com/mcp/response-handling", upload)
	request.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, request)

	require.Error(t, readErr)
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"name":"request_too_large"`)
}
