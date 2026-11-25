package mcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

func TestHostedAssignmentsDoNotBorrowSiblingRegistration(t *testing.T) {
	t.Parallel()
	project, issuer := uuid.New(), uuid.New()
	first, sibling, missing, noAuth, ambiguous := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	a, b := uuid.New(), uuid.New()
	row := func(server, registration uuid.UUID, requirement pgtype.Bool) upstreamclientsrepo.ListHostedUpstreamAssignmentsRow {
		return upstreamclientsrepo.ListHostedUpstreamAssignmentsRow{
			ServerID: server, ProjectID: project, UserSessionIssuerID: uuid.NullUUID{UUID: issuer, Valid: true},
			RegistrationID: uuid.NullUUID{UUID: registration, Valid: registration != uuid.Nil}, RequiresUpstreamAuth: requirement,
		}
	}
	unknown, notRequired := pgtype.Bool{Bool: false, Valid: false}, pgtype.Bool{Bool: false, Valid: true}
	assignments := indexHostedAssignments([]upstreamclientsrepo.ListHostedUpstreamAssignmentsRow{
		row(first, a, unknown), row(sibling, b, unknown), row(missing, uuid.Nil, unknown), row(noAuth, uuid.Nil, notRequired),
		row(ambiguous, a, unknown), row(ambiguous, b, unknown),
	})
	for _, tc := range []struct {
		name          string
		server        uuid.UUID
		want          uuid.UUID
		misconfigured bool
	}{
		{name: "first uses its own registration", server: first, want: a},
		{name: "sibling uses its own registration", server: sibling, want: b},
		{name: "explicit no upstream", server: noAuth},
		{name: "missing assignment", server: missing, misconfigured: true},
		{name: "ambiguous assignment", server: ambiguous, misconfigured: true},
		{name: "missing server", server: uuid.New(), misconfigured: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := assignments[tc.server].registration(project)
			if tc.misconfigured {
				require.ErrorIs(t, err, remotesessions.ErrRemoteSessionMisconfigured)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
	t.Run("wrong project", func(t *testing.T) {
		t.Parallel()
		_, err := assignments[first].registration(uuid.New())
		require.ErrorIs(t, err, remotesessions.ErrRemoteSessionMisconfigured)
	})
}

func TestExactHostedMemberTokensRejectMissingOrRefusedCredential(t *testing.T) {
	t.Parallel()
	registration, issuer := uuid.New(), uuid.New()
	for _, token := range []remotesessions.UpstreamToken{
		{},
		{RemoteSessionClientID: registration},
		{RemoteSessionClientID: uuid.New(), Token: "sibling"},
		{RemoteSessionClientID: registration, Token: "qualified", Resource: "https://example.test"},
		{RemoteSessionClientID: registration, ClientCredentialErr: remotesessions.ErrClientCredentialUnavailable},
	} {
		got, err := exactHostedMemberTokens(map[uuid.UUID]remotesessions.UpstreamToken{issuer: token}, registration)
		require.Error(t, err)
		require.Empty(t, got)
	}
}
