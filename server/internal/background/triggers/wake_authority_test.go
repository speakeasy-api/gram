package triggers

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	tokenrepo "github.com/speakeasy-api/gram/server/internal/auth/assistanttokens/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

type wakeAuthorityDB struct {
	pgx.Tx
	queryRow func(context.Context, string, ...any) pgx.Row
	query    func(context.Context, string, ...any) (pgx.Rows, error)
}

func (db wakeAuthorityDB) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	return db.queryRow(ctx, query, args...)
}
func (db wakeAuthorityDB) Query(ctx context.Context, query string, args ...any) (pgx.Rows, error) {
	return db.query(ctx, query, args...)
}

type wakeAuthorityRow func(...any) error

func (row wakeAuthorityRow) Scan(dest ...any) error { return row(dest...) }

type wakeAuthorityRows struct {
	pgx.Rows
	t     *testing.T
	rows  []tokenrepo.ListKitWorkloadBindingsRow
	index int
}

func (r *wakeAuthorityRows) Close()     {}
func (r *wakeAuthorityRows) Err() error { return nil }
func (r *wakeAuthorityRows) Next() bool { r.index++; return r.index <= len(r.rows) }
func (r *wakeAuthorityRows) Scan(dest ...any) error {
	row := r.rows[r.index-1]
	*wakeScanDestination[uuid.UUID](r.t, dest[0]) = row.RuntimeID
	*wakeScanDestination[uuid.UUID](r.t, dest[1]) = row.AssistantID
	*wakeScanDestination[[]byte](r.t, dest[2]) = row.BackendMetadataJson
	*wakeScanDestination[bool](r.t, dest[3]) = row.Active
	return nil
}

func TestWakeRuntimeAuthority(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                                                                                                    string
		user, noSession, wrongProject, wrongOrg, retired, ambiguous, missingSession, infraSession, infraRuntime bool
		wantError                                                                                               bool
		wantQueries                                                                                             int
	}{
		{name: "user", user: true},
		{name: "ordinary principal workload", noSession: true, wantQueries: 1},
		{name: "ordinary assertion workload", wantQueries: 2},
		{name: "retired managed generation", retired: true, wantError: true, wantQueries: 2},
		{name: "ambiguous managed generation", ambiguous: true, wantError: true, wantQueries: 2},
		{name: "foreign project", wrongProject: true, wantError: true},
		{name: "foreign organization", wrongOrg: true, wantError: true},
		{name: "missing or revoked session", missingSession: true, wantError: true, wantQueries: 1},
		{name: "session database failure", infraSession: true, wantQueries: 1},
		{name: "runtime database failure", infraRuntime: true, wantQueries: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			projectID, assistantID, issuerID := uuid.New(), uuid.New(), uuid.New()
			jti := "authenticated-session"
			// A service-account-shaped ordinary subject is not sufficient to classify a runtime.
			subject := "system:serviceaccount:example:kit-ordinary"
			actor := urn.NewWorkloadPrincipal(issuerID, subject)
			if tc.user {
				actor = urn.NewPrincipal(urn.PrincipalTypeUser, "test-user")
			}
			auth := &contextvalues.AuthContext{ActiveOrganizationID: "test-org", ProjectID: &projectID, SessionID: &jti}
			if tc.noSession {
				auth.SessionID = nil
			}
			if tc.wrongProject {
				other := uuid.New()
				auth.ProjectID = &other
			}
			if tc.wrongOrg {
				auth.ActiveOrganizationID = "other-org"
			}
			ctx := contextvalues.WithAuthenticatedActor(t.Context(), auth, actor)
			infraErr := errors.New("database unavailable")
			calls := 0
			db := wakeAuthorityDB{
				queryRow: func(_ context.Context, query string, args ...any) pgx.Row {
					calls++
					require.Contains(t, query, "GetWakeWorkloadSession")
					require.Contains(t, query, "deleted IS FALSE AND expires_at > clock_timestamp()")
					require.Equal(t, []any{jti, urn.NewWorkloadSubject(issuerID, subject), pgtype.Text{String: "test-org", Valid: true}, uuid.NullUUID{UUID: projectID, Valid: true}}, args)
					return wakeAuthorityRow(func(dest ...any) error {
						if tc.infraSession {
							return infraErr
						}
						if tc.missingSession {
							return pgx.ErrNoRows
						}
						*wakeScanDestination[uuid.UUID](t, dest[0]) = uuid.New()
						return nil
					})
				},
				query: func(_ context.Context, query string, args ...any) (pgx.Rows, error) {
					calls++
					require.Contains(t, query, "ListKitWorkloadBindings")
					require.Equal(t, []any{projectID, "test-org", issuerID.String(), subject}, args)
					if tc.infraRuntime {
						return nil, infraErr
					}
					rows := &wakeAuthorityRows{t: t}
					if tc.retired {
						rows.rows = []tokenrepo.ListKitWorkloadBindingsRow{{Active: false}}
					}
					if tc.ambiguous {
						rows.rows = []tokenrepo.ListKitWorkloadBindingsRow{{Active: true}, {Active: true}}
					}
					return rows, nil
				},
			}
			err := validateWakeRuntimeAuthority(ctx, db, CreateWakeInstanceParams{OrganizationID: "test-org", ProjectID: projectID, AssistantID: assistantID})
			if tc.infraSession || tc.infraRuntime {
				require.ErrorIs(t, err, infraErr)
				require.NotErrorIs(t, err, ErrBadRequest)
			} else if tc.wantError {
				require.ErrorIs(t, err, ErrBadRequest)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tc.wantQueries, calls)
		})
	}
}

func TestWakeRuntimeIdentity(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"live", "ended", "runtime generation", "assistant", "project", "version", "issuer", "subject", "identity organization", "identity project", "identity assistant", "agent", "corrupt"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			runtimeID, projectID, assistantID, agentID, issuerID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
			subject := "system:serviceaccount:example:kit-" + runtimeID.String()
			identity := wakeIdentitySnapshot{OrganizationID: "test-org", ProjectID: projectID, AssistantID: assistantID, AgentID: agentID, TriggerID: uuid.New(), IssuerID: uuid.New(), Subject: "assistant-trigger:root"}
			root := map[string]any{"runtime_id": runtimeID, "assistant_id": assistantID, "project_id": projectID, "runtime_version": 3}
			workload := map[string]any{"issuer_id": issuerID, "subject": subject}
			switch change {
			case "runtime generation":
				root["runtime_id"] = uuid.New()
			case "assistant":
				root["assistant_id"] = uuid.New()
			case "project":
				root["project_id"] = uuid.New()
			case "version":
				root["runtime_version"] = 2
			case "issuer":
				workload["issuer_id"] = uuid.New()
			case "subject":
				workload["subject"] = "retired-generation"
			case "identity organization":
				identity.OrganizationID = "other-org"
			case "identity project":
				identity.ProjectID = uuid.New()
			case "identity assistant":
				identity.AssistantID = uuid.New()
			case "agent":
				identity.AgentID = uuid.New()
			}
			workload["identity"] = identity
			root["workload"] = workload
			data, err := json.Marshal(root)
			require.NoError(t, err)
			if change == "corrupt" {
				data = []byte("invalid")
			}
			ctx := contextvalues.WithAuthenticatedActor(t.Context(), &contextvalues.AuthContext{}, urn.NewWorkloadPrincipal(issuerID, subject))
			ctx = contextvalues.WithWorkloadAgent(ctx, urn.NewPrincipal(urn.PrincipalTypeAgent, agentID.String()))
			row := tokenrepo.ListKitWorkloadBindingsRow{RuntimeID: runtimeID, AssistantID: assistantID, BackendMetadataJson: data, Active: change != "ended"}
			params := CreateWakeInstanceParams{OrganizationID: "test-org", ProjectID: projectID, AssistantID: assistantID}
			got, err := wakeRuntimeIdentity(ctx, row, params, issuerID, subject)
			if change == "live" {
				require.NoError(t, err)
				require.Equal(t, identity, got)
			} else {
				require.ErrorIs(t, err, ErrBadRequest)
			}
		})
	}
}

func wakeScanDestination[T any](t *testing.T, dest any) *T {
	t.Helper()
	value, ok := dest.(*T)
	require.True(t, ok, "unexpected scan destination type")
	return value
}
