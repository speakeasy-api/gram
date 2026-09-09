package background

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/background/interceptors"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/worker"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/networkingress"
	"github.com/speakeasy-api/gram/server/internal/networkingress/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type ingressHistoryLogs struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (l *ingressHistoryLogs) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	n, err := l.buffer.Write(p)
	if err != nil {
		return n, fmt.Errorf("capture ingress log: %w", err)
	}
	return n, nil
}

func (l *ingressHistoryLogs) text() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buffer.String()
}

type historyIngressProvider struct {
	credentialSeen chan []byte
	failure        bool
}

func (p *historyIngressProvider) Apply(_ context.Context, desired k8s.NetworkIngressDesired) (k8s.NetworkIngressObservation, error) {
	p.credentialSeen <- bytes.Clone(desired.Credentials)
	if p.failure {
		return k8s.NetworkIngressObservation{Status: "error", DNSName: "", ErrorCode: "invalid_credentials", ConnectedAt: nil}, fmt.Errorf("provider echoed %s", desired.Credentials)
	}
	return k8s.NetworkIngressObservation{Status: "online", DNSName: "history.example.ts.net", ErrorCode: "", ConnectedAt: nil}, nil
}

func (*historyIngressProvider) Observe(context.Context, k8s.NetworkIngressResourceNames) (k8s.NetworkIngressObservation, error) {
	return k8s.NetworkIngressObservation{Status: "pending", DNSName: "", ErrorCode: "", ConnectedAt: nil}, nil
}

func (*historyIngressProvider) Delete(context.Context, k8s.NetworkIngressResourceNames) error {
	return nil
}

func newIngressHistoryTest(t *testing.T, failing bool) (*NetworkIngressClient, uuid.UUID, *historyIngressProvider, *ingressHistoryLogs) {
	t.Helper()
	pg, cleanup, err := testenv.Launch(t.Context(), testenv.LaunchOptions{Postgres: true})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cleanup()) })
	db, err := pg.CloneTestDatabase(t, "ingresshistory")
	require.NoError(t, err)
	env, _ := infra.NewTemporalEnv(t)
	id := uuid.New()
	names, err := k8s.NewNetworkIngressResourceNames(id)
	require.NoError(t, err)
	resources, err := names.Marshal()
	require.NoError(t, err)
	enc := testenv.NewEncryptionClient(t)
	ciphertext, err := enc.Encrypt([]byte(`{"client_id":"history-client","client_secret":"` + ingressHistorySecret + `"}`))
	require.NoError(t, err)
	_, err = repo.New(db).CreateNetworkIngress(t.Context(), repo.CreateNetworkIngressParams{
		ID: id, OrganizationID: "history-test-org", Provider: "tailscale", Hostname: "history",
		EndpointNamespaceKind: "platform", CustomDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Enabled: true, IdentityRequired: false, CredentialsEncrypted: conv.ToPGText(ciphertext),
		AttestorNamespace: names.Namespace, AttestorServiceAccount: names.AttestorServiceAccount, ProviderResources: resources,
	})
	require.NoError(t, err)
	logs := &ingressHistoryLogs{}
	logger := slog.New(slog.NewTextHandler(logs, nil))
	provider := &historyIngressProvider{credentialSeen: make(chan []byte, 10), failure: failing}
	registry, err := k8s.NewNetworkIngressProvisionerRegistry(map[string]k8s.NetworkIngressProvisioner{"tailscale": provider}, logger, k8s.NewNetworkIngressMetrics(logger, testenv.NewMeterProvider(t)))
	require.NoError(t, err)
	executor := networkingress.NewExecutor(db, enc, registry, networkingress.ExecutorOptions{Queue: string(env.Queue()), Image: "test-image", BackendService: "backend", BackendPort: 443, CanApply: func(context.Context) error { return nil }})
	a := &networkIngressActivities{executor: executor, db: db, queue: string(env.Queue())}
	var clientOptions client.Options
	clientOptions.Namespace = string(env.Namespace())
	clientOptions.Logger = logger
	capturedClient, err := client.NewClientFromExisting(env.Client(), clientOptions)
	require.NoError(t, err)
	t.Cleanup(capturedClient.Close)
	w := worker.New(capturedClient, string(env.Queue()), worker.Options{Interceptors: []interceptor.WorkerInterceptor{
		&interceptors.Recovery{WorkerInterceptorBase: interceptor.WorkerInterceptorBase{}},
		&interceptors.InjectExecutionInfo{WorkerInterceptorBase: interceptor.WorkerInterceptorBase{}},
		&interceptors.Logging{WorkerInterceptorBase: interceptor.WorkerInterceptorBase{}},
	}})
	w.RegisterWorkflow(NetworkIngressReconcileWorkflow)
	w.RegisterActivityWithOptions(a.reconcile, activity.RegisterOptions{Name: NetworkIngressReconcileActivityName})
	require.NoError(t, w.Start())
	t.Cleanup(w.Stop)
	// Read a typed row to ensure this is the production SQL-backed path.
	row, err := repo.New(db).GetNetworkIngressForReconcile(t.Context(), id)
	require.NoError(t, err)
	require.Equal(t, pgtype.Text{String: ciphertext, Valid: true}, row.CredentialsEncrypted)
	return &NetworkIngressClient{Client: capturedClient, Queue: string(env.Queue())}, id, provider, logs
}
