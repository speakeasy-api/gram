package remotesessions

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

func catalogIssuer(issuer, authorizationEndpoint, name string, logo uuid.NullUUID) repo.RemoteSessionIssuer {
	return repo.RemoteSessionIssuer{
		Issuer:                issuer,
		AuthorizationEndpoint: pgtype.Text{String: authorizationEndpoint, Valid: true},
		Name:                  pgtype.Text{String: name, Valid: name != ""},
		LogoAssetID:           logo,
	}
}

func newLogo() uuid.NullUUID {
	return uuid.NullUUID{UUID: uuid.New(), Valid: true}
}

func TestApplyCatalogBranding_FillsMissingNameAndLogo(t *testing.T) {
	t.Parallel()

	logo := newLogo()
	clients := []Client{{
		IssuerSlug:            "notion-0123abcd",
		IssuerURL:             "https://mcp.example.com",
		AuthorizationEndpoint: "https://mcp.example.com/authorize",
	}}

	got := applyCatalogBranding(clients, []repo.RemoteSessionIssuer{
		catalogIssuer("https://mcp.example.com", "https://mcp.example.com/authorize", "Example", logo),
	})

	require.NotNil(t, got[0].IssuerName)
	require.Equal(t, "Example", *got[0].IssuerName)
	require.Equal(t, logo, got[0].IssuerLogoAssetID)
	require.Nil(t, clients[0].IssuerName, "input must not be mutated")
}

func TestApplyCatalogBranding_KeepsOperatorBranding(t *testing.T) {
	t.Parallel()

	own := newLogo()
	name := "Corporate Example"
	got := applyCatalogBranding([]Client{{
		IssuerName:            &name,
		IssuerLogoAssetID:     own,
		IssuerURL:             "https://mcp.example.com",
		AuthorizationEndpoint: "https://mcp.example.com/authorize",
	}}, []repo.RemoteSessionIssuer{
		catalogIssuer("https://mcp.example.com", "https://mcp.example.com/authorize", "Example", newLogo()),
	})

	require.Equal(t, "Corporate Example", *got[0].IssuerName)
	require.Equal(t, own, got[0].IssuerLogoAssetID)
}

// An operator-set name stays while the missing logo is still borrowed.
func TestApplyCatalogBranding_FillsOnlyTheMissingField(t *testing.T) {
	t.Parallel()

	logo := newLogo()
	name := "Corporate Example"
	got := applyCatalogBranding([]Client{{
		IssuerName:            &name,
		IssuerURL:             "https://mcp.example.com",
		AuthorizationEndpoint: "https://mcp.example.com/authorize",
	}}, []repo.RemoteSessionIssuer{
		catalogIssuer("https://mcp.example.com", "https://mcp.example.com/authorize", "Example", logo),
	})

	require.Equal(t, "Corporate Example", *got[0].IssuerName)
	require.Equal(t, logo, got[0].IssuerLogoAssetID)
}

func TestApplyCatalogBranding_IgnoresDifferentAuthorizationEndpoint(t *testing.T) {
	t.Parallel()

	got := applyCatalogBranding([]Client{{
		IssuerURL:             "https://mcp.example.com",
		AuthorizationEndpoint: "https://elsewhere.example.net/authorize",
	}}, []repo.RemoteSessionIssuer{
		catalogIssuer("https://mcp.example.com", "https://mcp.example.com/authorize", "Example", newLogo()),
	})

	require.Nil(t, got[0].IssuerName)
	require.False(t, got[0].IssuerLogoAssetID.Valid)
}

func TestApplyCatalogBranding_IgnoresDifferentIssuer(t *testing.T) {
	t.Parallel()

	got := applyCatalogBranding([]Client{{
		IssuerURL:             "https://other.example.com",
		AuthorizationEndpoint: "https://mcp.example.com/authorize",
	}}, []repo.RemoteSessionIssuer{
		catalogIssuer("https://mcp.example.com", "https://mcp.example.com/authorize", "Example", newLogo()),
	})

	require.Nil(t, got[0].IssuerName)
	require.False(t, got[0].IssuerLogoAssetID.Valid)
}

// Trailing slash and default port are the same authorization server.
func TestApplyCatalogBranding_MatchesIssuerSpellingVariants(t *testing.T) {
	t.Parallel()

	got := applyCatalogBranding([]Client{{
		IssuerURL:             "https://mcp.example.com/",
		AuthorizationEndpoint: "https://mcp.example.com/authorize",
	}}, []repo.RemoteSessionIssuer{
		catalogIssuer("https://mcp.example.com:443", "https://mcp.example.com/authorize", "Example", uuid.NullUUID{}),
	})

	require.NotNil(t, got[0].IssuerName)
	require.Equal(t, "Example", *got[0].IssuerName)
}

// Duplicate catalog rows never mix one row's name with another's logo.
func TestApplyCatalogBranding_BorrowsFromOneCatalogRow(t *testing.T) {
	t.Parallel()

	got := applyCatalogBranding([]Client{{
		IssuerURL:             "https://mcp.example.com",
		AuthorizationEndpoint: "https://mcp.example.com/authorize",
	}}, []repo.RemoteSessionIssuer{
		catalogIssuer("https://mcp.example.com", "https://mcp.example.com/authorize", "First", uuid.NullUUID{}),
		catalogIssuer("https://mcp.example.com", "https://mcp.example.com/authorize", "Second", newLogo()),
	})

	require.NotNil(t, got[0].IssuerName)
	require.Equal(t, "First", *got[0].IssuerName)
	require.False(t, got[0].IssuerLogoAssetID.Valid)
}
