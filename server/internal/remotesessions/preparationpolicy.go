package remotesessions

import (
	"context"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urls"
)

func PreparationEligibility(profiles, grants []string) string {
	if !slices.Contains(profiles, oauthwire.GrantProfileIDJAG) {
		return PreparationStateUnsupportedProfile
	}
	if !slices.Contains(grants, oauthwire.GrantTypeJWTBearer) {
		return PreparationStateIncompleteMetadata
	}
	return preparationEligible
}

// Grant arrays without provenance cannot establish registration readiness.
func preparationGrantSourceRecorded(source string) bool {
	switch source {
	case PreparationGrantSourceProviderReturned, PreparationGrantSourceAdministratorDeclared, PreparationGrantSourceCIMDPublished:
		return true
	default:
		return false
	}
}

// Registration grant records are sets, with NULL distinct from explicit empty.
func samePreparationGrants(a, b []string) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	a = slices.Clone(a)
	b = slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(slices.Compact(a), slices.Compact(b))
}

// RFC 6749 section 3.3: scope-token = %x21 / %x23-5B / %x5D-7E.
func validPreparationScopeToken(scope string) bool {
	if scope == "" {
		return false
	}
	for i := range len(scope) {
		c := scope[i]
		if c < 0x21 || c > 0x7e || c == '"' || c == '\\' {
			return false
		}
	}
	return true
}

func normalizePreparationInput(in PreparationInput) (PreparationInput, error) {
	u, err := url.Parse(in.Resource)
	if err != nil || !urls.IsAbsoluteHTTPSOrLoopback(in.Resource) || u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.ForceQuery {
		return in, oops.E(oops.CodeBadRequest, err, "invalid canonical resource")
	}
	// RFC 8707 identifiers are exact: a trailing slash is not discarded.
	if in.UserSessionIssuerID == uuid.Nil || in.RemoteSessionIssuerID == uuid.Nil {
		return in, oops.C(oops.CodeBadRequest)
	}
	scopes := make([]string, 0, len(in.Scopes))
	for _, scope := range in.Scopes {
		if !validPreparationScopeToken(scope) {
			return in, oops.E(oops.CodeBadRequest, nil, "invalid scope")
		}
		scopes = append(scopes, scope)
	}
	slices.Sort(scopes)
	in.Scopes = slices.Compact(scopes)
	if in.Mechanism != "" && in.Mechanism != PreparationMechanismManual && in.Mechanism != PreparationMechanismCIMD && in.Mechanism != PreparationMechanismDCR {
		return in, oops.E(oops.CodeBadRequest, nil, "invalid preparation mechanism")
	}
	for _, g := range in.ConfirmGrants {
		if g == "" || strings.ContainsAny(g, " \t\r\n") {
			return in, oops.E(oops.CodeBadRequest, nil, "invalid grant type")
		}
	}
	in.ConfirmGrants = slices.Clone(in.ConfirmGrants)
	slices.Sort(in.ConfirmGrants)
	in.ConfirmGrants = slices.Compact(in.ConfirmGrants)
	return in, nil
}

// A completed provider registration can recover as discovery or credentials
// change, but only current grant evidence can make it ready.
func preparationRegistrationReadiness(ctx context.Context, q *repo.Queries, client repo.RemoteSessionClient, issuer repo.RemoteSessionIssuer, org string, readOnly ...bool) string {
	if eligibility := PreparationEligibility(issuer.AuthorizationGrantProfilesSupported, issuer.GrantTypesSupported); eligibility != preparationEligible {
		return eligibility
	}
	if preparationMetadataTransient(issuer) {
		return PreparationStateTransientFailure
	}
	if !preparationClientConfigurationValid(ctx, q, client, issuer, org, readOnly...) {
		return PreparationStateManualSetupRequired
	}
	if !slices.Contains(client.GrantTypes, oauthwire.GrantTypeJWTBearer) {
		return preparationMissingGrantsState(client.GrantTypes)
	}
	return PreparationStateReady
}

// Readiness is a current local configuration check, never a provider acceptance
// claim. Reads and idempotent DCR lookups must not keep expired credentials ready.
func preparationClientConfigurationValid(ctx context.Context, q *repo.Queries, client repo.RemoteSessionClient, issuer repo.RemoteSessionIssuer, org string, readOnly ...bool) bool {
	method := client.TokenEndpointAuthMethod.String
	if client.ClientSecretExpiresAt.Valid && !client.ClientSecretExpiresAt.Time.After(time.Now()) {
		return false
	}
	authMethodSupported := slices.Contains(issuer.TokenEndpointAuthMethodsSupported, method)
	if method == "" || !authMethodSupported || ((method == oauthwire.AuthMethodClientSecretBasic || method == oauthwire.AuthMethodClientSecretPost) && !client.ClientSecretEncrypted.Valid) || (method == oauthwire.AuthMethodPrivateKeyJWT && !client.JsonWebKeySetID.Valid) {
		return false
	}
	if method == oauthwire.AuthMethodPrivateKeyJWT && client.JsonWebKeySetID.Valid {
		var err error
		if len(readOnly) > 0 && readOnly[0] {
			_, err = q.ReadEMAJsonWebKeySet(ctx, repo.ReadEMAJsonWebKeySetParams{ID: client.JsonWebKeySetID.UUID, OrganizationID: org})
		} else {
			_, err = q.LockJsonWebKeySetForClientAttach(ctx, repo.LockJsonWebKeySetForClientAttachParams{ID: client.JsonWebKeySetID.UUID, OrganizationID: org})
		}
		if err != nil {
			return false
		}
	}
	return true
}

func preparationMissingGrantsState(grants []string) string {
	if grants == nil {
		return PreparationStateUnknownGrants
	}
	return PreparationStateManualSetupRequired
}

func preparationMetadataTransient(issuer repo.RemoteSessionIssuer) bool {
	_, transient := issuerMetadataFailureState(IssuerMetadataUse{
		ID: issuer.ID, IssuerURL: issuer.Issuer, ProjectID: issuer.ProjectID, OrganizationID: issuer.OrganizationID,
		MetadataFetchedAt: issuer.MetadataFetchedAt, MetadataLastErrorAt: issuer.MetadataLastErrorAt, MetadataLastErrorUrl: issuer.MetadataLastErrorUrl, NeedsReprojection: false,
	})
	return transient
}
