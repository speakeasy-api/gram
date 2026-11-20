package remotesessions

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/stretchr/testify/require"
	otellog "go.opentelemetry.io/otel/log"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/tunnel/route"
	"github.com/speakeasy-api/gram/tunnel/wire"

	"github.com/speakeasy-api/gram/server/internal/mcp/tunnelrouting"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

func TestRefreshHappened_InvalidClientFollowsWhoCanRepairIt(t *testing.T) {
	t.Parallel()

	withEndpoint := func(change func(*remotesessions_repo.GetRemoteSessionClientWithIssuerByIDRow)) remotesessions_repo.GetRemoteSessionClientWithIssuerByIDRow {
		client := remotesessions_repo.GetRemoteSessionClientWithIssuerByIDRow{IssuerRegistrationEndpoint: pgtype.Text{String: "https://idp.example.com/register", Valid: true}}
		change(&client)
		return client
	}
	for _, tc := range []struct {
		name   string
		client remotesessions_repo.GetRemoteSessionClientWithIssuerByIDRow
		want   mcpconnectionattempts.Code
	}{
		{name: "dynamic", client: withEndpoint(func(*remotesessions_repo.GetRemoteSessionClientWithIssuerByIDRow) {}), want: mcpconnectionattempts.CodeRefreshInvalidClient},
		{name: "static", client: remotesessions_repo.GetRemoteSessionClientWithIssuerByIDRow{}, want: mcpconnectionattempts.CodeProviderOAuthInvalidClient},
		{name: "cimd", client: withEndpoint(func(c *remotesessions_repo.GetRemoteSessionClientWithIssuerByIDRow) {
			c.ClientIDMetadataUri = pgtype.Text{String: "https://client.example.com/metadata", Valid: true}
		}), want: mcpconnectionattempts.CodeProviderOAuthInvalidClient},
		{name: "managed", client: withEndpoint(func(c *remotesessions_repo.GetRemoteSessionClientWithIssuerByIDRow) {
			c.IdentityProviderConnectionID = uuid.NullUUID{UUID: uuid.New(), Valid: true}
		}), want: mcpconnectionattempts.CodeProviderOAuthInvalidClient},
		{name: "private key", client: withEndpoint(func(c *remotesessions_repo.GetRemoteSessionClientWithIssuerByIDRow) {
			c.TokenEndpointAuthMethod = pgtype.Text{String: string(TokenEndpointAuthMethodPrivateKeyJWT), Valid: true}
		}), want: mcpconnectionattempts.CodeProviderOAuthInvalidClient},
		{name: "self", client: withEndpoint(func(c *remotesessions_repo.GetRemoteSessionClientWithIssuerByIDRow) {
			c.CredentialOwner = string(CredentialOwnerSelf)
		}), want: mcpconnectionattempts.CodeProviderOAuthInvalidClient},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			what, ok := refreshHappened(remotesessionmetrics.RefreshOutcomeInvalidClient, tc.client, nil)
			require.True(t, ok)
			require.Equal(t, tc.want, what)
		})
	}
}

func TestRefreshHappened_DownTunnelIsTheTunnel(t *testing.T) {
	t.Parallel()

	client := remotesessions_repo.GetRemoteSessionClientWithIssuerByIDRow{TunneledMcpServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true}}
	for _, tc := range []struct {
		name    string
		outcome remotesessionmetrics.RefreshOutcome
		err     error
	}{
		{name: "unanswered", outcome: remotesessionmetrics.RefreshOutcomeUnreachable, err: errors.New("post refresh")},
		{name: "gateway 502", outcome: remotesessionmetrics.RefreshOutcomeUpstreamError, err: &TokenRefreshError{statusCode: http.StatusBadGateway, tunnelError: wire.TunnelErrorNoRoute}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			what, ok := refreshHappened(tc.outcome, client, tc.err)
			require.True(t, ok)
			require.Equal(t, mcpconnectionattempts.CodeTunnelUnavailable, what)
		})
	}
}

// A forwarded 502 came from the provider; gateway status distinguishes capacity and routing faults.
func TestRefreshHappened_BadGatewayFollowsTheTunnelStatus(t *testing.T) {
	t.Parallel()

	client := remotesessions_repo.GetRemoteSessionClientWithIssuerByIDRow{TunneledMcpServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true}}
	for tunnelError, want := range map[string]mcpconnectionattempts.Code{
		"":                                mcpconnectionattempts.CodeProviderServerError,
		wire.TunnelErrorTunnelBusy:        mcpconnectionattempts.CodeTunnelAtCapacity,
		wire.TunnelErrorRouteLookupFailed: mcpconnectionattempts.CodeInternalError,
	} {
		what, ok := refreshHappened(remotesessionmetrics.RefreshOutcomeUpstreamError, client, &TokenRefreshError{statusCode: http.StatusBadGateway, tunnelError: tunnelError})
		require.True(t, ok)
		require.Equal(t, want, what, tunnelError)
	}
}

func TestRefreshHappened_InternalErrorFollowsTheRemedy(t *testing.T) {
	t.Parallel()

	var client remotesessions_repo.GetRemoteSessionClientWithIssuerByIDRow
	for _, tc := range []struct {
		name string
		err  error
		want mcpconnectionattempts.Code
	}{
		{name: "administrator", err: newTokenRefreshError("no token endpoint", nil, refreshRemedyAdministrator), want: mcpconnectionattempts.CodeCredentialMisconfigured},
		{name: "reconnect", err: newTokenRefreshError("unreadable token", nil, refreshRemedyReconnect), want: mcpconnectionattempts.CodeUpstreamReconnectRequired},
		{name: "stored refresh token unreadable", err: newTokenRefreshError("unreadable token", fmt.Errorf("%w: %w", errStoredTokenUnreadable, errors.New("decrypt")), refreshRemedyReconnect), want: mcpconnectionattempts.CodeStoredSignInUnreadable},
		{name: "retry", err: newTokenRefreshError("lost the race", nil, refreshRemedyRetry), want: mcpconnectionattempts.CodeInternalError},
		{name: "plain", err: errors.New("database"), want: mcpconnectionattempts.CodeInternalError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			what, ok := refreshHappened(remotesessionmetrics.RefreshOutcomeInternalError, client, tc.err)
			require.True(t, ok)
			require.Equal(t, tc.want, what)
		})
	}
}

func TestRefreshHappened_NoRow(t *testing.T) {
	t.Parallel()

	var client remotesessions_repo.GetRemoteSessionClientWithIssuerByIDRow
	for _, outcome := range []remotesessionmetrics.RefreshOutcome{remotesessionmetrics.RefreshOutcomeSessionInactive, remotesessionmetrics.RefreshOutcomeCanceled, remotesessionmetrics.RefreshOutcomeDeferred} {
		_, ok := refreshHappened(outcome, client, context.Canceled)
		require.False(t, ok, outcome)
	}
}

func TestRefreshHappened_LocalTunnelTransportIsInternal(t *testing.T) {
	t.Parallel()
	client := remotesessions_repo.GetRemoteSessionClientWithIssuerByIDRow{TunneledMcpServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true}}
	for _, err := range []error{newTokenRefreshError("no transport", errRefreshTunnelTransport, refreshRemedyRetry), tunnelrouting.ErrTransportConfiguration} {
		what, ok := refreshHappened(refreshOutcomeForError(context.Background(), err), client, err)
		require.True(t, ok)
		require.Equal(t, mcpconnectionattempts.CodeInternalError, what)
		require.True(t, refreshOutagePaced(remotesessionmetrics.RefreshOutcomeInternalError, err))
	}
}

func TestRefreshHappened_TemporaryOAuthErrorsPreserveResponse(t *testing.T) {
	t.Parallel()
	for _, code := range []string{"server_error", "temporarily_unavailable"} {
		for _, status := range []int{http.StatusOK, http.StatusBadRequest, http.StatusTooManyRequests} {
			err := newTokenRefreshErrorFromHTTP(status, http.StatusText(status), []byte(`{"error":"`+code+`"}`))
			outcome := refreshOutcomeForError(context.Background(), err)
			want := remotesessionmetrics.RefreshOutcomeRejected
			if status == http.StatusTooManyRequests {
				want = remotesessionmetrics.RefreshOutcomeRateLimited
			}
			require.Equal(t, want, outcome)
			require.Equal(t, status == http.StatusTooManyRequests, refreshOutagePaced(outcome, err))
		}
	}
}

func TestRefreshHappened_MissingTokenDoesNotStartCooldown(t *testing.T) {
	t.Parallel()
	err := newTokenRefreshError("no access token", errRefreshUnusableAnswer, refreshRemedyAdministrator)
	require.False(t, refreshOutagePaced(refreshOutcomeForError(context.Background(), err), err))
}

func TestRefreshHappened_TruncatedBodyPrecedesTunnelOutage(t *testing.T) {
	t.Parallel()
	err := fmt.Errorf("read refresh response: %w: %w: %w", errRefreshUpstreamUnreachable, errRefreshUnusableAnswer, io.ErrUnexpectedEOF)
	for _, tunneled := range []bool{false, true} {
		client := remotesessions_repo.GetRemoteSessionClientWithIssuerByIDRow{TunneledMcpServerID: uuid.NullUUID{UUID: uuid.New(), Valid: tunneled}}
		outcome := refreshOutcomeForError(context.Background(), err)
		require.Equal(t, remotesessionmetrics.RefreshOutcomeUnreachable, outcome)
		require.ErrorIs(t, &RefreshError{Outcome: outcome, err: err}, ErrRemoteSessionUnavailable)
		what, ok := refreshHappened(outcome, client, err)
		require.True(t, ok)
		require.Equal(t, mcpconnectionattempts.CodeProviderServerError, what)
		for _, suppressed := range []remotesessionmetrics.RefreshOutcome{remotesessionmetrics.RefreshOutcomeCanceled, remotesessionmetrics.RefreshOutcomeDeferred, remotesessionmetrics.RefreshOutcomeSessionInactive} {
			_, ok := refreshHappened(suppressed, client, err)
			require.False(t, ok)
		}
	}
}

func TestRefreshHappened_ExhaustedTunnelPreservesGatewayCause(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		status string
		want   mcpconnectionattempts.Code
	}{
		{wire.TunnelErrorTunnelBusy, mcpconnectionattempts.CodeTunnelAtCapacity},
		{wire.TunnelErrorActiveCheckFailed, mcpconnectionattempts.CodeInternalError},
	} {
		t.Run(tc.status, func(t *testing.T) {
			t.Parallel()
			gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set(tunnelrouting.ErrorHeader, tc.status)
				w.WriteHeader(http.StatusBadGateway)
			}))
			defer gateway.Close()
			routes := route.NewRouteTable()
			require.NoError(t, routes.Publish(t.Context(), "tunnel", gateway.URL, time.Minute))
			tunnels := tunnelrouting.NewHTTPClient(routes, "forward-token", guardian.NewDefaultPolicy(testenv.NewTracerProvider(t)), []string{"127.0.0.0/8"})
			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://idp.example.com/token", nil)
			require.NoError(t, err)
			resp, err := tunnels.Do(req, "tunnel")
			if resp != nil {
				require.NoError(t, resp.Body.Close())
			}
			require.Nil(t, resp)
			require.ErrorIs(t, err, tunnelrouting.ErrNoTunnelRoute)
			// This is the unchanged wrapping applied by postRefreshGrant.
			failure := fmt.Errorf("post refresh: %w: %w", errRefreshUpstreamUnreachable, err)
			outcome := refreshOutcomeForError(t.Context(), failure)
			require.Equal(t, remotesessionmetrics.RefreshOutcomeUnreachable, outcome)
			require.ErrorIs(t, &RefreshError{Outcome: outcome, err: failure}, ErrRemoteSessionUnavailable)
			client := remotesessions_repo.GetRemoteSessionClientWithIssuerByIDRow{TunneledMcpServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true}}
			what, ok := refreshHappened(outcome, client, failure)
			require.True(t, ok)
			require.Equal(t, tc.want, what)
		})
	}
}

func TestRefreshFailureRecorded_UnmarkedErrors(t *testing.T) {
	t.Parallel()
	for _, outcome := range []remotesessionmetrics.RefreshOutcome{remotesessionmetrics.RefreshOutcomeDeferred, remotesessionmetrics.RefreshOutcomeCanceled, remotesessionmetrics.RefreshOutcomeSessionInactive, remotesessionmetrics.RefreshOutcomeRefreshed, remotesessionmetrics.RefreshOutcomeUpstreamError} {
		require.False(t, RefreshFailureRecorded(&RefreshError{Outcome: outcome, err: errors.New("unrecorded")}))
	}
	require.False(t, RefreshFailureRecorded(nil))
	require.False(t, RefreshFailureRecorded(fmt.Errorf("unrelated: %w", ErrRemoteSessionUnavailable)))
}

// stalledPool is a pool whose database accepts connections and never answers.
func stalledPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	var mu sync.Mutex
	var held []net.Conn
	t.Cleanup(func() {
		_ = listener.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, conn := range held {
			_ = conn.Close()
		}
	})
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			held = append(held, conn)
			mu.Unlock()
		}
	}()
	pool, err := pgxpool.New(t.Context(), "postgres://stalled:stalled@"+listener.Addr().String()+"/stalled?sslmode=disable")
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

func boundSession() remotesessions_repo.RemoteSession {
	return remotesessions_repo.RemoteSession{RemoteSessionClientID: uuid.New(), UserSessionIssuerID: uuid.New()}
}

// A database that accepts and never answers holds the refresh only for the lookup's bound.
func TestInvalidClientHappened_AStalledLookupDoesNotHoldTheRefresh(t *testing.T) {
	t.Parallel()
	s := &RefreshService{db: stalledPool(t), connections: mcpconnectionattemptstest.New(t).Recorder}

	started := time.Now()
	what := s.invalidClientHappened(t.Context(), boundSession())

	require.Equal(t, mcpconnectionattempts.CodeProviderOAuthInvalidClient, what, "an unknown binding never recommends re-registration")
	require.Less(t, time.Since(started), 3*time.Second, "well under the old 5 second wait")
}

type refreshExportSink struct {
	mu      sync.Mutex
	records []otellog.Record
}

func (s *refreshExportSink) Publish(_ context.Context, _, _, _ string, record otellog.Record) gcp.PublishResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, record)
	return gcp.NewSuccessPublishResult()
}

func (*refreshExportSink) Close() {}

// storingConn accepts every insert, so the recorder goes on to export.
type storingConn struct{ driver.Conn }

func (storingConn) PrepareBatch(context.Context, string, ...driver.PrepareBatchOption) (driver.Batch, error) {
	return storedBatch{}, nil
}

type storedBatch struct{ driver.Batch }

func (storedBatch) Append(...any) error { return nil }
func (storedBatch) Send() error         { return nil }
func (storedBatch) Abort() error        { return nil }

// exportRefresh records one failed refresh through a real recorder and returns its exported attributes.
func exportRefresh(t *testing.T, outcome remotesessionmetrics.RefreshOutcome, err error) map[string]otellog.Value {
	t.Helper()

	sink := &refreshExportSink{mu: sync.Mutex{}, records: nil}
	logsOn := func(context.Context, string) (bool, error) { return true, nil }
	recorder := mcpconnectionattempts.NewRecorder(testenv.NewLogger(t), testenv.NewMeterProvider(t), storingConn{}, logsOn, mcpconnectionattempts.WithExport(sink))
	s := &RefreshService{connections: recorder}
	attribution := RefreshIn(mcpconnectionattempts.ServerScope("org-test", uuid.New(), uuid.New(), mcpconnectionattempts.AppDCR))
	client := remotesessions_repo.GetRemoteSessionClientWithIssuerByIDRow{IssuerUrl: "https://login.example.com:8443"}

	standalone, _ := s.recordRefresh(t.Context(), attribution, remotesessions_repo.RemoteSession{}, client, remotesessionmetrics.RefreshTriggerRequest, outcome, err)
	require.True(t, standalone)
	require.NoError(t, recorder.Shutdown(t.Context()))

	require.Len(t, sink.records, 1)
	got := make(map[string]otellog.Value)
	sink.records[0].WalkAttributes(func(kv otellog.KeyValue) bool {
		got[kv.Key] = kv.Value
		return true
	})
	return got
}

func TestRecordRefresh_UnreadableStoredSignInDoesNotNameTheProvider(t *testing.T) {
	t.Parallel()

	got := exportRefresh(t, remotesessionmetrics.RefreshOutcomeInternalError, fmt.Errorf("%w: %w", errStoredTokenUnreadable, errors.New("decrypt")))
	require.Equal(t, mcpconnectionattempts.CodeStoredSignInUnreadable.String(), got[string(attr.ErrorTypeKey)].AsString())
	require.NotContains(t, got, string(attr.ServerAddressKey))
	require.NotContains(t, got, string(attr.ServerPortKey))
}

func TestRecordRefresh_ProviderRefusalNamesTheProvider(t *testing.T) {
	t.Parallel()

	got := exportRefresh(t, remotesessionmetrics.RefreshOutcomeInvalidGrant, errors.New("invalid_grant"))
	require.Equal(t, mcpconnectionattempts.CodeInvalidGrant.String(), got[string(attr.ErrorTypeKey)].AsString())
	require.Equal(t, "login.example.com", got[string(attr.ServerAddressKey)].AsString())
}
