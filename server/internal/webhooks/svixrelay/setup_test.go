package svixrelay_test

import (
	"context"
	"log"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	svix "github.com/svix/svix-webhooks/go"

	webhooksv1 "github.com/speakeasy-api/gram/infra/gen/gram/webhooks/v1"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	svixtest "github.com/speakeasy-api/gram/server/internal/thirdparty/svix/svixtest"
	"github.com/speakeasy-api/gram/server/internal/webhooks/svixrelay"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
	if err != nil {
		log.Fatalf("failed to launch test infrastructure: %v", err)
	}
	infra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Printf("cleanup failed: %v", err)
	}
	os.Exit(code)
}

type handlerTestInstance struct {
	conn    *pgxpool.Pool
	handler *svixrelay.Handler
	svixSrv *svixtest.MockServer
}

func newHandlerTestInstance(t *testing.T, dispatch bool) *handlerTestInstance {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	logger := testenv.NewLogger(t)

	svixSrv := svixtest.NewMockServer(logger)
	t.Cleanup(svixSrv.Close)

	// Retries disabled: the handler's job is to classify one response, and SDK
	// retries would hide which status it actually saw.
	svixClient, err := svix.New("test-token", &svix.SvixOptions{
		ServerUrl:     svixSrv.URL(),
		RetrySchedule: &[]time.Duration{},
	})
	require.NoError(t, err)

	return &handlerTestInstance{
		conn:    conn,
		handler: svixrelay.NewHandler(logger, testenv.NewMeterProvider(t), conn, svixClient),
		svixSrv: svixSrv,
	}
}

// seedOrg creates an organization and configures its webhook state.
func seedOrg(t *testing.T, conn *pgxpool.Pool, svixAppID string, webhooksEnabled bool) string {
	t.Helper()

	ctx := t.Context()
	orgID := uuid.NewString()

	_, err := orgsrepo.New(conn).UpsertOrganizationMetadata(ctx, orgsrepo.UpsertOrganizationMetadataParams{
		ID:   orgID,
		Name: orgID,
		Slug: orgID,
	})
	require.NoError(t, err)

	err = testrepo.New(conn).SetOrgWebhookConfig(ctx, testrepo.SetOrgWebhookConfigParams{
		OrganizationID:  orgID,
		SvixAppID:       conv.ToPGTextEmpty(svixAppID),
		WebhooksEnabled: pgtype.Bool{Bool: webhooksEnabled, Valid: true},
	})
	require.NoError(t, err)

	return orgID
}

func newEvent(orgID, eventID string, payload []byte) *webhooksv1.Event {
	eventType := "audit_log.asset_event_v1"
	createdAt := time.Now().UTC().Format(time.RFC3339)

	return webhooksv1.Event_builder{
		EventId:        &eventID,
		OrganizationId: &orgID,
		EventType:      &eventType,
		Payload:        payload,
		CreatedAt:      &createdAt,
	}.Build()
}
