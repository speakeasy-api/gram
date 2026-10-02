package assistantidentity_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var cloneTestDatabase testenv.PostgresDBCloneFunc

func TestMain(m *testing.M) {
	ctx := context.Background()
	container, clone, err := testenv.NewTestPostgres(ctx)
	if err != nil {
		log.Fatalf("launch identity test postgres: %v", err)
	}
	cloneTestDatabase = clone
	code := m.Run()
	if err := container.Terminate(ctx); err != nil {
		log.Fatalf("terminate identity test postgres: %v", err)
	}
	os.Exit(code)
}

type fixture struct {
	db        *pgxpool.Pool
	org       string
	project   uuid.UUID
	assistant uuid.UUID
	trigger   uuid.UUID
	actor     string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	db, err := cloneTestDatabase(t, "assistant_identity")
	require.NoError(t, err)
	f := fixture{db: db, org: "org-identity-test", project: uuid.New(), assistant: uuid.New(), trigger: uuid.New(), actor: "identity-creator"}
	q := repo.New(db)
	require.NoError(t, q.FixtureCreateOrganization(t.Context(), f.org))
	require.NoError(t, q.FixtureCreateUser(t.Context(), repo.FixtureCreateUserParams{ID: f.actor, Email: "identity@example.com"}))
	require.NoError(t, q.FixtureCreateMembership(t.Context(), repo.FixtureCreateMembershipParams{OrganizationID: f.org, UserID: conv.ToPGText(f.actor)}))
	require.NoError(t, q.FixtureCreateProject(t.Context(), repo.FixtureCreateProjectParams{ID: f.project, OrganizationID: f.org}))
	require.NoError(t, q.FixtureCreateAssistant(t.Context(), repo.FixtureCreateAssistantParams{ID: f.assistant, OrganizationID: f.org, ProjectID: f.project, Creator: conv.ToPGText(f.actor)}))
	require.NoError(t, q.FixtureCreateRoot(t.Context(), repo.FixtureCreateRootParams{ID: f.trigger, OrganizationID: f.org, ProjectID: f.project, DefinitionSlug: "dashboard", TargetRef: f.assistant.String()}))
	return f
}

func inTx(t *testing.T, db *pgxpool.Pool, fn func(pgx.Tx) error) error {
	t.Helper()
	tx := testenv.BeginTx(t, t.Context(), db)
	defer o11y.NoLogDefer(func() error { return tx.Rollback(t.Context()) })
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(t.Context()); err != nil {
		return fmt.Errorf("commit fixture transaction: %w", err)
	}
	return nil
}

func (f fixture) provision(t *testing.T) assistantidentity.Identity {
	t.Helper()
	require.NoError(t, inTx(t, f.db, func(tx pgx.Tx) error {
		_, err := testIdentityService.Upgrade(t.Context(), tx, assistantidentity.ProvisionParams{OrganizationID: f.org, ProjectID: f.project, AssistantID: f.assistant, ActorUserID: f.actor})
		if err != nil {
			return fmt.Errorf("upgrade fixture: %w", err)
		}
		return nil
	}))
	result, err := testIdentityService.Resolve(t.Context(), f.db, f.org, f.project, f.assistant, f.trigger)
	require.NoError(t, err)
	require.Equal(t, assistantidentity.Active, result.State)
	require.NotNil(t, result.Identity)
	return *result.Identity
}

func (f fixture) attachMCP(t *testing.T) uuid.UUID {
	t.Helper()
	remote, server := uuid.New(), uuid.New()
	q := repo.New(f.db)
	require.NoError(t, q.FixtureCreateRemote(t.Context(), repo.FixtureCreateRemoteParams{ID: remote, ProjectID: f.project}))
	require.NoError(t, q.FixtureCreateMCPServer(t.Context(), repo.FixtureCreateMCPServerParams{ID: server, ProjectID: f.project, RemoteID: uuid.NullUUID{UUID: remote, Valid: true}}))
	require.NoError(t, q.FixtureAttachMCPServer(t.Context(), repo.FixtureAttachMCPServerParams{AssistantID: f.assistant, ServerID: server, ProjectID: f.project}))
	return server
}

func (f fixture) grant(t *testing.T, principal urn.Principal, scope authz.Scope, resource string) {
	t.Helper()
	selector, err := json.Marshal(authz.NewSelector(scope, resource))
	require.NoError(t, err)
	_, err = accessrepo.New(f.db).InsertPrincipalGrantIfAbsent(t.Context(), accessrepo.InsertPrincipalGrantIfAbsentParams{OrganizationID: f.org, PrincipalUrn: principal, Scope: string(scope), Selectors: selector})
	require.NoError(t, err)
}

var errInjected = errors.New("injected identity write failure")

type failureTx struct {
	pgx.Tx
	table string
}
type failedRow struct{}

func (failedRow) Scan(...any) error { return errInjected }

// Forwarding is necessary to inject a failure between real generated writes;
// no fixture SQL is constructed or executed by this adapter.
func (tx failureTx) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	if strings.Contains(query, "INSERT INTO "+tx.table+" ") {
		return pgconn.CommandTag{}, errInjected
	}
	result, err := tx.Tx.Exec(ctx, query, args...) //nolint:glint // notestingrawsql: fault-injection adapter forwards SQLc's query unchanged.
	if err != nil {
		return result, fmt.Errorf("execute generated fixture write: %w", err)
	}
	return result, nil
}
func (tx failureTx) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	if strings.Contains(query, "INSERT INTO "+tx.table+" ") {
		return failedRow{}
	}
	return tx.Tx.QueryRow(ctx, query, args...) //nolint:glint // notestingrawsql: fault-injection adapter forwards SQLc's query unchanged.
}

var testIdentityService = func() *assistantidentity.Service {
	service, err := assistantidentity.New("https://platform.example.invalid", false)
	if err != nil {
		panic(err)
	}
	return service
}()
