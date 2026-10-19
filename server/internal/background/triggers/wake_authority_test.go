package triggers

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type wakeAuthorityDB struct {
	triggerrepo.DBTX
	queryRow func(context.Context, string, ...any) pgx.Row
}

func (db wakeAuthorityDB) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	return db.queryRow(ctx, query, args...)
}

type wakeAuthorityRow func(...any) error

func (row wakeAuthorityRow) Scan(dest ...any) error { return row(dest...) }

func TestWakeRuntimeAuthority(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                                                                                  string
		user, noSession, legacySession, wrongProject, retired, wrongAssistant, missingSession bool
		infraSession, infraRuntime                                                            bool
		wantError                                                                             bool
		wantQueries                                                                           int
	}{
		{name: "session database failure", infraSession: true, wantQueries: 1},
		{name: "runtime database failure", infraRuntime: true, wantQueries: 2},
		{name: "user", user: true},
		{name: "legacy principal credential", noSession: true},
		{name: "legacy OAuth workload", legacySession: true, wantQueries: 1},
		{name: "live runtime", wantQueries: 2},
		{name: "ended or revoked runtime", retired: true, wantError: true, wantQueries: 2},
		{name: "another assistant", wrongAssistant: true, wantError: true, wantQueries: 2},
		{name: "another project", wrongProject: true, wantError: true},
		{name: "missing persisted session", missingSession: true, wantError: true, wantQueries: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			projectID, assistantID, clientID := uuid.New(), uuid.New(), uuid.New()
			jti := "authenticated-session"
			actor := urn.NewWorkloadPrincipal(uuid.New(), "assistant-trigger:test")
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
			ctx := contextvalues.WithAuthenticatedActor(t.Context(), auth, actor)
			infraErr := errors.New("database unavailable")
			calls := 0
			db := wakeAuthorityDB{queryRow: func(_ context.Context, query string, args ...any) pgx.Row {
				calls++
				require.Len(t, args, 4)
				require.Equal(t, jti, args[0])
				issuer, subject, err := actor.Workload()
				require.NoError(t, err)
				require.Equal(t, urn.NewWorkloadSubject(issuer, subject), args[1])
				require.Equal(t, pgtype.Text{String: "test-org", Valid: true}, args[2])
				if strings.Contains(query, "GetWakeWorkloadSessionClient") {
					require.Equal(t, uuid.NullUUID{UUID: projectID, Valid: true}, args[3])
				} else {
					require.Equal(t, projectID, args[3])
				}
				return wakeAuthorityRow(func(dest ...any) error {
					if strings.Contains(query, "GetWakeWorkloadSessionClient") {
						if tc.infraSession {
							return infraErr
						}
						if tc.missingSession {
							return pgx.ErrNoRows
						}
						*wakeScanDestination[uuid.NullUUID](t, dest[0]) = uuid.NullUUID{UUID: clientID, Valid: !tc.legacySession}
						return nil
					}
					require.Contains(t, query, "GetAssistantRuntimeForWorkloadSession")
					if tc.infraRuntime {
						return infraErr
					}
					if tc.retired {
						return pgx.ErrNoRows
					}
					*wakeScanDestination[uuid.UUID](t, dest[0]) = uuid.New()
					*wakeScanDestination[uuid.UUID](t, dest[1]) = assistantID
					if tc.wrongAssistant {
						*wakeScanDestination[uuid.UUID](t, dest[1]) = uuid.New()
					}
					*wakeScanDestination[uuid.UUID](t, dest[2]) = clientID
					return nil
				})
			}}
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

func wakeScanDestination[T any](t *testing.T, dest any) *T {
	t.Helper()
	value, ok := dest.(*T)
	require.True(t, ok, "unexpected scan destination type")
	return value
}
