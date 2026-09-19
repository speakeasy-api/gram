// Package oinmanifest exports the Okta Integration Network (OIN) Cross App
// Access manifest: the platform-global remote session catalog (global ID-JAG
// issuers and their global clients) shaped the way Okta's listing
// questionnaire asks for it. It reads catalog rows only; tenant issuers,
// tenant clients, organization ids, and project ids never enter the output.
package oinmanifest

import (
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/oinmanifest/repo"
)

const (
	// ManifestVersion is bumped when the JSON shape changes incompatibly.
	ManifestVersion = 1

	// MaxRegistrations bounds the export; the catalog is expected to stay far
	// below it, so exceeding it signals a bug rather than a big catalog.
	MaxRegistrations = 500

	grantTypeJWTBearer = "urn:ietf:params:oauth:grant-type:jwt-bearer" //nolint:gosec // URN identifier, not a credential.
	grantProfileIDJAG  = "urn:ietf:params:oauth:grant-profile:id-jag"

	subjectTokenTypeSAML2 = "urn:ietf:params:oauth:token-type:saml2" //nolint:gosec // URN identifier, not a credential.

	registrationStatic = "static"
	registrationCIMD   = "cimd"
	registrationNone   = "none"

	scopesSourceClient   = "client"
	scopesSourceIssuer   = "issuer_metadata"
	scopesSourceNone     = "none"
	evidenceUnavailable  = "unavailable"
	unsetValue           = "unset"
	metadataMaxAge       = 30 * 24 * time.Hour
	blockerNoClient      = "no global client registered"
	blockerRejected      = "client rejected upstream (invalid_client)"
	blockerNoScopes      = "no scopes recorded"
	blockerAudience      = "audience unknown"
	blockerNoMetadata    = "issuer metadata never fetched"
	blockerStaleMetadata = "issuer metadata older than 30 days"
)

// Config carries the requesting-app facts that are configuration rather than
// catalog rows. Empty ListingName and OrgDomain render as "unset" so a local
// export never invents a listing identity.
type Config struct {
	ListingName string
	OrgDomain   string
	ServerURL   string
}

// Manifest is the exported document. Both renderers read this one struct so
// the JSON and Markdown outputs cannot drift.
type Manifest struct {
	ManifestVersion       int            `json:"manifest_version"`
	GeneratedAt           time.Time      `json:"generated_at"`
	RequestingApp         RequestingApp  `json:"requesting_app"`
	TrustRequirements     []string       `json:"trust_requirements"`
	ResourceRegistrations []Registration `json:"resource_registrations"`
	Summary               Summary        `json:"summary"`
}

// RequestingApp describes Speakeasy in the OIN requesting-app role.
type RequestingApp struct {
	Name                   string `json:"name"`
	OrgDomain              string `json:"org_domain"`
	SSOMode                string `json:"sso_mode"`
	Role                   string `json:"role"`
	RedirectURI            string `json:"redirect_uri"`
	SubjectTokenType       string `json:"subject_token_type"`
	SendsResourceParameter bool   `json:"sends_resource_parameter"`
}

// Registration is one resource app pairing: a global ID-JAG issuer and the
// global client Speakeasy uses against it.
type Registration struct {
	ResourceName            string                  `json:"resource_name"`
	ResourceASIssuer        string                  `json:"resource_as_issuer"`
	ResourceIdentifier      string                  `json:"resource_identifier"`
	XAAAudience             *string                 `json:"xaa_audience"`
	ClientID                *string                 `json:"client_id"`
	Registration            string                  `json:"registration"`
	TokenEndpointAuthMethod *string                 `json:"token_endpoint_auth_method"`
	Scopes                  []string                `json:"scopes"`
	ScopesSource            string                  `json:"scopes_source"`
	CIMDSupported           bool                    `json:"cimd_supported"`
	Advertises              Advertises              `json:"advertises"`
	MetadataFetchedAt       *time.Time              `json:"metadata_fetched_at"`
	ResourceAppExpectations ResourceAppExpectations `json:"resource_app_expectations"`
	Blockers                []string                `json:"blockers"`
	Evidence                Evidence                `json:"evidence"`
}

// Advertises repeats the issuer's relevant RFC 8414 capability sets.
type Advertises struct {
	GrantTypes    []string `json:"grant_types"`
	GrantProfiles []string `json:"grant_profiles"`
}

// ResourceAppExpectations is the resource-app half of the OIN questionnaire
// for this pairing, so Okta's reviewers see both sides at once. The issuer URL
// is the resource AS Speakeasy redeems at; the ID-JAG audience stays separate
// in XAAAudience until the catalog can hold it.
type ResourceAppExpectations struct {
	XAAIssuerURL        *string  `json:"xaa_issuer_url"`
	ResourceIdentifiers []string `json:"resource_identifiers"`
	Scopes              []string `json:"scopes"`
}

// Evidence is reserved for the AIM-62 diagnostic; until it lands every pair
// reports "unavailable" and nothing else.
type Evidence struct {
	Status string `json:"status"`
}

// Summary counts registrations by readiness.
type Summary struct {
	Registrations int `json:"registrations"`
	Ready         int `json:"ready"`
	Blocked       int `json:"blocked"`
}

// Ready reports whether the registration has no blockers.
func (r Registration) Ready() bool {
	return len(r.Blockers) == 0
}

// TrustRequirements is what Speakeasy asks of every resource authorization
// server in the listing.
var TrustRequirements = []string{
	"Resource authorization servers must key trust on ID-JAG iss (the tenant issuer) plus sub, with aud equal to the resource.",
	"client_id is platform-global and must never identify a tenant.",
	"Speakeasy sends resource on every exchange, so invalid_target is meaningful.",
}

// AdvertisesIDJAG mirrors the SQL predicate in ListGlobalIDJAGIssuers: an
// issuer qualifies when it advertises both the jwt-bearer grant type and the
// ID-JAG grant profile. A test asserts the two agree on shared fixtures.
func AdvertisesIDJAG(grantTypes, grantProfiles []string) bool {
	return containsString(grantTypes, grantTypeJWTBearer) && containsString(grantProfiles, grantProfileIDJAG)
}

func containsString(values []string, want string) bool {
	return slices.Contains(values, want)
}

// Build folds the query rows into a Manifest. Rows arrive one per
// (issuer, client) pair; issuers with several global clients collapse into one
// registration carrying the first client and a blocker naming the extra ones.
func Build(rows []repo.ListGlobalIDJAGIssuersRow, cfg Config, now time.Time) Manifest {
	now = now.UTC()
	groups := groupByIssuer(rows)
	registrations := make([]Registration, 0, len(groups))
	for _, group := range groups {
		registrations = append(registrations, buildRegistration(group, now))
	}
	sort.SliceStable(registrations, func(i, j int) bool {
		if registrations[i].ResourceASIssuer != registrations[j].ResourceASIssuer {
			return registrations[i].ResourceASIssuer < registrations[j].ResourceASIssuer
		}
		return registrations[i].ResourceName < registrations[j].ResourceName
	})

	summary := Summary{Registrations: len(registrations), Ready: 0, Blocked: 0}
	for _, registration := range registrations {
		if registration.Ready() {
			summary.Ready++
		} else {
			summary.Blocked++
		}
	}

	return Manifest{
		ManifestVersion: ManifestVersion,
		GeneratedAt:     now,
		RequestingApp: RequestingApp{
			Name:                   orUnset(cfg.ListingName),
			OrgDomain:              orUnset(cfg.OrgDomain),
			SSOMode:                "saml",
			Role:                   "requesting_app",
			RedirectURI:            strings.TrimRight(cfg.ServerURL, "/") + "/oauth/callback",
			SubjectTokenType:       subjectTokenTypeSAML2,
			SendsResourceParameter: true,
		},
		TrustRequirements:     append([]string(nil), TrustRequirements...),
		ResourceRegistrations: registrations,
		Summary:               summary,
	}
}

type issuerGroup struct {
	issuer  repo.ListGlobalIDJAGIssuersRow
	clients []repo.ListGlobalIDJAGIssuersRow
}

func groupByIssuer(rows []repo.ListGlobalIDJAGIssuersRow) []issuerGroup {
	var groups []issuerGroup
	index := map[uuid.UUID]int{}
	for _, row := range rows {
		position, seen := index[row.IssuerRowID]
		if !seen {
			position = len(groups)
			index[row.IssuerRowID] = position
			groups = append(groups, issuerGroup{issuer: row, clients: nil})
		}
		if row.ClientRowID.Valid {
			groups[position].clients = append(groups[position].clients, row)
		}
	}
	return groups
}

func buildRegistration(group issuerGroup, now time.Time) Registration {
	issuer := group.issuer
	blockers := make([]string, 0, 4)

	registration := Registration{
		ResourceName:            orDefault(issuer.IssuerName.String, ""),
		ResourceASIssuer:        issuer.Issuer,
		ResourceIdentifier:      issuer.Issuer,
		XAAAudience:             nil,
		ClientID:                nil,
		Registration:            registrationNone,
		TokenEndpointAuthMethod: nil,
		Scopes:                  []string{},
		ScopesSource:            scopesSourceNone,
		CIMDSupported:           issuer.CimdSupported,
		Advertises: Advertises{
			GrantTypes:    copyStrings(issuer.GrantTypesSupported),
			GrantProfiles: copyStrings(issuer.GrantProfilesSupported),
		},
		MetadataFetchedAt: nil,
		ResourceAppExpectations: ResourceAppExpectations{
			XAAIssuerURL:        nil,
			ResourceIdentifiers: nil,
			Scopes:              nil,
		},
		Blockers: nil,
		Evidence: Evidence{Status: evidenceUnavailable},
	}

	switch len(group.clients) {
	case 0:
		blockers = append(blockers, blockerNoClient)
	default:
		client := group.clients[0]
		registration.ClientID = new(client.ClientID.String)
		registration.Registration = registrationStatic
		if client.ClientIDMetadataUri.Valid && client.ClientIDMetadataUri.String != "" {
			registration.Registration = registrationCIMD
		}
		if client.TokenEndpointAuthMethod.Valid {
			registration.TokenEndpointAuthMethod = new(client.TokenEndpointAuthMethod.String)
		}
		if client.ResourceIdentifier.Valid && client.ResourceIdentifier.String != "" {
			registration.ResourceIdentifier = client.ResourceIdentifier.String
		}
		if registration.ResourceName == "" && client.ResourceName.Valid {
			registration.ResourceName = client.ResourceName.String
		}
		if len(client.ClientScope) > 0 {
			registration.Scopes = copyStrings(client.ClientScope)
			registration.ScopesSource = scopesSourceClient
		}
		if client.UpstreamRejectedAt.Valid {
			blockers = append(blockers, blockerRejected+" at "+client.UpstreamRejectedAt.Time.UTC().Format(time.RFC3339))
		}
		if len(group.clients) > 1 {
			blockers = append(blockers, "more than one global client registered ("+strconv.Itoa(len(group.clients))+")")
		}
	}

	if len(registration.Scopes) == 0 && len(issuer.ScopesSupported) > 0 {
		registration.Scopes = copyStrings(issuer.ScopesSupported)
		registration.ScopesSource = scopesSourceIssuer
	}
	if len(registration.Scopes) == 0 {
		blockers = append(blockers, blockerNoScopes)
	}

	// No column on the catalog holds the ID-JAG audience yet, so every pair
	// says so explicitly rather than guessing from the issuer URL.
	blockers = append(blockers, blockerAudience)

	switch {
	case !issuer.MetadataFetchedAt.Valid:
		blockers = append(blockers, blockerNoMetadata)
	default:
		fetched := issuer.MetadataFetchedAt.Time.UTC()
		registration.MetadataFetchedAt = &fetched
		if now.Sub(fetched) > metadataMaxAge {
			blockers = append(blockers, blockerStaleMetadata)
		}
	}
	if issuer.MetadataLastError.Valid && issuer.MetadataLastError.String != "" {
		blockers = append(blockers, "issuer metadata refresh failed: "+issuer.MetadataLastError.String)
	}

	if registration.ResourceName == "" {
		registration.ResourceName = registration.ResourceASIssuer
	}
	registration.ResourceAppExpectations = ResourceAppExpectations{
		XAAIssuerURL:        new(registration.ResourceASIssuer),
		ResourceIdentifiers: []string{registration.ResourceIdentifier},
		Scopes:              copyStrings(registration.Scopes),
	}
	registration.Blockers = blockers
	return registration
}

func orUnset(value string) string {
	return orDefault(strings.TrimSpace(value), unsetValue)
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func copyStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return append([]string{}, values...)
}
