package service

import (
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	emaapps "github.com/speakeasy-api/gram/dev-idp/gen/ema_apps"
	ematrustrules "github.com/speakeasy-api/gram/dev-idp/gen/ema_trust_rules"
	"github.com/speakeasy-api/gram/dev-idp/internal/cimd"
	"github.com/speakeasy-api/gram/dev-idp/internal/database/repo"
	"github.com/speakeasy-api/gram/dev-idp/internal/oops"
)

func TestEmaAppUpdateRejectsCIMDWhenStoredSecretWouldRemain(t *testing.T) {
	t.Parallel()

	db := testDB(t)
	service := testEmaAppsService(t, db)
	app, err := service.Create(t.Context(), &emaapps.CreatePayload{
		ClientID:     "secret-client",
		ClientSecret: new("secret"),
		Jwks:         nil,
		Name:         nil,
		Enabled:      nil,
	})
	require.NoError(t, err)

	_, err = service.Update(t.Context(), &emaapps.UpdatePayload{
		ID:           app.ID,
		ClientID:     new("https://client.example/metadata.json"),
		ClientSecret: nil,
		Jwks:         nil,
		Name:         nil,
		Enabled:      nil,
	})
	var publicErr *oops.ShareableError
	require.ErrorAs(t, err, &publicErr)
	require.Equal(t, oops.CodeBadRequest, publicErr.Code)

	stored, err := repo.New(db).GetEmaApp(t.Context(), uuid.MustParse(app.ID))
	require.NoError(t, err)
	require.Equal(t, "secret-client", stored.ClientID)
	require.Equal(t, "secret", stored.ClientSecret)

	updated, err := service.Update(t.Context(), &emaapps.UpdatePayload{
		ID:           app.ID,
		ClientID:     new("https://client.example/metadata.json"),
		ClientSecret: new(""),
		Jwks:         nil,
		Name:         nil,
		Enabled:      nil,
	})
	require.NoError(t, err)
	require.Equal(t, "https://client.example/metadata.json", updated.ClientID)
	require.Empty(t, updated.ClientSecret)
}

func TestEmaAppConcurrentCredentialPatchesPreserveInvariant(t *testing.T) {
	t.Parallel()

	db := testDB(t)
	queries := repo.New(db)
	app, err := queries.CreateEmaApp(t.Context(), repo.CreateEmaAppParams{
		ID:           uuid.New(),
		ClientID:     "public-client",
		ClientSecret: "",
		Jwks:         "",
		Name:         "Public client",
		Enabled:      true,
	})
	require.NoError(t, err)

	start := make(chan struct{})
	results := make(chan error, 2)
	go func() {
		<-start
		_, updateErr := queries.UpdateEmaApp(t.Context(), repo.UpdateEmaAppParams{
			ClientID:     sql.NullString{String: "https://client.example/metadata.json", Valid: true},
			ClientSecret: sql.NullString{String: "", Valid: false},
			Jwks:         sql.NullString{String: "", Valid: false},
			Name:         sql.NullString{String: "", Valid: false},
			EnabledSet:   false,
			Enabled:      false,
			Ts:           time.Now(),
			ID:           app.ID,
		})
		results <- updateErr
	}()
	go func() {
		<-start
		_, updateErr := queries.UpdateEmaApp(t.Context(), repo.UpdateEmaAppParams{
			ClientID:     sql.NullString{String: "", Valid: false},
			ClientSecret: sql.NullString{String: "secret", Valid: true},
			Jwks:         sql.NullString{String: "", Valid: false},
			Name:         sql.NullString{String: "", Valid: false},
			EnabledSet:   false,
			Enabled:      false,
			Ts:           time.Now(),
			ID:           app.ID,
		})
		results <- updateErr
	}()
	close(start)

	succeeded := 0
	for range 2 {
		if updateErr := <-results; updateErr == nil {
			succeeded++
		} else {
			require.ErrorIs(t, updateErr, sql.ErrNoRows)
		}
	}
	require.Equal(t, 1, succeeded)

	stored, err := queries.GetEmaApp(t.Context(), app.ID)
	require.NoError(t, err)
	require.False(t, cimd.IsClientID(stored.ClientID) && stored.ClientSecret != "")
}

func TestEmaTrustRulePartialUpdateDoesNotRewriteEnabled(t *testing.T) {
	t.Parallel()

	db := testDB(t)
	queries := repo.New(db)
	resource, err := queries.CreateEmaResource(t.Context(), repo.CreateEmaResourceParams{
		ID:                 uuid.New(),
		Slug:               "chat",
		Name:               "Chat",
		ResourceIdentifier: "https://mcp.example/chat",
	})
	require.NoError(t, err)
	rule, err := testEmaTrustRulesService(t, db).Create(t.Context(), &ematrustrules.CreatePayload{
		ResourceID:       resource.ID.String(),
		TrustedIssuer:    "https://issuer.example",
		AllowedClientIds: nil,
		AllowedScopes:    nil,
		Enabled:          new(false),
	})
	require.NoError(t, err)

	updated, err := testEmaTrustRulesService(t, db).Update(t.Context(), &ematrustrules.UpdatePayload{
		ID:               rule.ID,
		TrustedIssuer:    nil,
		AllowedClientIds: nil,
		AllowedScopes:    new("chat.read"),
		Enabled:          nil,
	})
	require.NoError(t, err)
	require.False(t, updated.Enabled)
}

func TestValidateAllowedClientIDsRejectsNullValues(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"null", `["client", null]`} {
		require.Error(t, validateAllowedClientIDs(raw), "value %s must be rejected", raw)
	}
	require.NoError(t, validateAllowedClientIDs(`[]`))
	require.NoError(t, validateAllowedClientIDs(`["client"]`))
}
