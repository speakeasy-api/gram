package agent

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type progressTransport func(*http.Request) (*http.Response, error)

func (f progressTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPassiveProgressPhases(t *testing.T) {
	t.Parallel()
	d := newDiagnostics()
	entered, headers := make(chan struct{}), make(chan struct{})
	transport := observedTransport{diagnostics: d, base: progressTransport(func(*http.Request) (*http.Response, error) {
		close(entered)
		<-headers
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("PRIVATE_PAYLOAD_SENTINEL"))}, nil
	})}
	var response *http.Response
	var err error
	done := make(chan struct{})
	go func() { defer close(done); response, err = transport.RoundTrip(&http.Request{}) }()
	<-entered
	waiting := d.snapshot()
	require.EqualValues(t, 1, waiting.HTTPProgress.WaitingHeaders)
	require.Zero(t, waiting.HTTPProgress.OpenResponses)
	close(headers)
	<-done
	require.NoError(t, err)
	open := d.snapshot()
	require.Zero(t, open.HTTPProgress.WaitingHeaders)
	require.EqualValues(t, 1, open.HTTPProgress.OpenResponses)
	// Snapshots must not alias counters that are subsequently mutated.
	require.EqualValues(t, 1, waiting.HTTPProgress.WaitingHeaders)
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, "PRIVATE_PAYLOAD_SENTINEL", string(body))
	require.Zero(t, d.snapshot().HTTPProgress.OpenResponses)
	require.NoError(t, response.Body.Close())
	require.NoError(t, response.Body.Close())
	require.Zero(t, d.snapshot().HTTPProgress.OpenResponses)
	encoded, err := json.Marshal(d.snapshot())
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "PRIVATE_PAYLOAD_SENTINEL")
}

type upgradeBody struct{ bytes.Buffer }

func (*upgradeBody) Close() error { return nil }

func TestPassiveProgressPreservesUpgradeWriter(t *testing.T) {
	t.Parallel()
	d := newDiagnostics()
	body := &upgradeBody{}
	transport := observedTransport{diagnostics: d, base: progressTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 101, Body: body}, nil
	})}
	response, err := transport.RoundTrip(&http.Request{})
	require.NoError(t, err)
	writer, ok := response.Body.(io.Writer)
	require.True(t, ok)
	_, err = writer.Write([]byte("forwarded"))
	require.NoError(t, err)
	require.Equal(t, "forwarded", body.String())
	require.NoError(t, response.Body.Close())
	require.Zero(t, d.snapshot().HTTPProgress.OpenResponses)
}

func TestPassiveProgressConcurrentTraffic(t *testing.T) {
	t.Parallel()
	d := newDiagnostics()
	transport := observedTransport{diagnostics: d, base: progressTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok"))}, nil
	})}
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			for range 100 {
				response, err := transport.RoundTrip(&http.Request{})
				if err != nil {
					t.Error(err)
					return
				}
				_, _ = io.Copy(io.Discard, response.Body)
				_ = response.Body.Close()
				_ = d.snapshot()
			}
		})
	}
	wg.Wait()
	report := d.snapshot()
	require.EqualValues(t, 10000, report.RequestsTotal)
	require.Zero(t, report.HTTPProgress.WaitingHeaders)
	require.Zero(t, report.HTTPProgress.OpenResponses)
}

func BenchmarkPassiveProgress(b *testing.B) {
	for _, observed := range []bool{false, true} {
		name := "baseline"
		if observed {
			name = "observed"
		}
		b.Run(name, func(b *testing.B) {
			var transport http.RoundTripper = progressTransport(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok"))}, nil
			})
			if observed {
				transport = observedTransport{base: transport, diagnostics: newDiagnostics()}
			}
			request := &http.Request{}
			b.ReportAllocs()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					response, _ := transport.RoundTrip(request)
					_, _ = io.Copy(io.Discard, response.Body)
					_ = response.Body.Close()
				}
			})
		})
	}
}
