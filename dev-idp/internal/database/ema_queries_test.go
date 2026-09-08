package database_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/dev-idp/internal/bootstrap"
	"github.com/speakeasy-api/gram/dev-idp/internal/config"
	"github.com/speakeasy-api/gram/dev-idp/internal/database/repo"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := bootstrap.Open(t.Context(), config.DB{Mode: config.DBModeMemory, Path: ""})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestEmaResourceIdentifierRejectsBlankWrites(t *testing.T) {
	t.Parallel()

	queries := repo.New(openTestDB(t))
	_, err := queries.CreateEmaResource(t.Context(), repo.CreateEmaResourceParams{
		ID:                 uuid.New(),
		Slug:               "blank",
		Name:               "Blank",
		ResourceIdentifier: "  ",
	})
	require.ErrorContains(t, err, "resource_identifier must not be blank")

	resource, err := queries.CreateEmaResource(t.Context(), repo.CreateEmaResourceParams{
		ID:                 uuid.New(),
		Slug:               "chat",
		Name:               "Chat",
		ResourceIdentifier: "https://mcp.example/chat",
	})
	require.NoError(t, err)
	_, err = queries.UpdateEmaResource(t.Context(), repo.UpdateEmaResourceParams{
		Slug:               sql.NullString{String: "", Valid: false},
		Name:               sql.NullString{String: "", Valid: false},
		ResourceIdentifier: sql.NullString{String: "\t", Valid: true},
		Ts:                 time.Now(),
		ID:                 resource.ID,
	})
	require.ErrorContains(t, err, "resource_identifier must not be blank")
}

func TestEmaUpsertsRefreshUpdatedAt(t *testing.T) {
	t.Parallel()

	queries := repo.New(openTestDB(t))
	user, err := queries.CreateUser(t.Context(), repo.CreateUserParams{
		ID:           uuid.New(),
		Email:        "upsert@example.com",
		DisplayName:  "Upsert",
		PhotoUrl:     sql.NullString{String: "", Valid: false},
		GithubHandle: sql.NullString{String: "", Valid: false},
		Admin:        false,
		Whitelisted:  true,
	})
	require.NoError(t, err)
	app, err := queries.CreateEmaApp(t.Context(), repo.CreateEmaAppParams{
		ID:           uuid.New(),
		ClientID:     "upsert-client",
		ClientSecret: "",
		Jwks:         "",
		Name:         "Upsert client",
		Enabled:      true,
	})
	require.NoError(t, err)
	resource, err := queries.CreateEmaResource(t.Context(), repo.CreateEmaResourceParams{
		ID:                 uuid.New(),
		Slug:               "upsert",
		Name:               "Upsert",
		ResourceIdentifier: "https://mcp.example/upsert",
	})
	require.NoError(t, err)

	assignment, err := queries.CreateEmaAppAssignment(t.Context(), repo.CreateEmaAppAssignmentParams{
		ID:            uuid.New(),
		AppID:         app.ID,
		UserID:        user.ID,
		ResourceID:    resource.ID,
		GrantedScopes: "read",
		Ts:            time.Now(),
	})
	require.NoError(t, err)
	old := time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)
	_, err = queries.UpdateEmaAppAssignment(t.Context(), repo.UpdateEmaAppAssignmentParams{
		GrantedScopes: assignment.GrantedScopes,
		Ts:            old,
		ID:            assignment.ID,
	})
	require.NoError(t, err)
	assignment, err = queries.CreateEmaAppAssignment(t.Context(), repo.CreateEmaAppAssignmentParams{
		ID:            uuid.New(),
		AppID:         app.ID,
		UserID:        user.ID,
		ResourceID:    resource.ID,
		GrantedScopes: "write",
		Ts:            time.Now(),
	})
	require.NoError(t, err)
	require.True(t, assignment.UpdatedAt.After(old))

	rule, err := queries.CreateEmaTrustRule(t.Context(), repo.CreateEmaTrustRuleParams{
		ID:               uuid.New(),
		ResourceID:       resource.ID,
		TrustedIssuer:    "https://issuer.example",
		AllowedClientIds: "[]",
		AllowedScopes:    "read",
		Enabled:          true,
		Ts:               time.Now(),
	})
	require.NoError(t, err)
	_, err = queries.UpdateEmaTrustRule(t.Context(), repo.UpdateEmaTrustRuleParams{
		TrustedIssuer:    sql.NullString{String: "", Valid: false},
		AllowedClientIds: sql.NullString{String: "", Valid: false},
		AllowedScopes:    sql.NullString{String: "", Valid: false},
		EnabledSet:       false,
		Enabled:          false,
		Ts:               old,
		ID:               rule.ID,
	})
	require.NoError(t, err)
	rule, err = queries.CreateEmaTrustRule(t.Context(), repo.CreateEmaTrustRuleParams{
		ID:               uuid.New(),
		ResourceID:       resource.ID,
		TrustedIssuer:    "https://issuer.example",
		AllowedClientIds: "[]",
		AllowedScopes:    "write",
		Enabled:          true,
		Ts:               time.Now(),
	})
	require.NoError(t, err)
	require.True(t, rule.UpdatedAt.After(old))
}
