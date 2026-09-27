package remotesessions

import (
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// These tagged projections preserve the database snapshots' JSON field names
// and order. Direct struct conversions ensure schema changes cannot silently
// omit a field from the federation fingerprint.
type federatedClientSnapshot struct {
	ID                              uuid.UUID          `json:"ID"`
	ProjectID                       uuid.NullUUID      `json:"ProjectID"`
	OrganizationID                  pgtype.Text        `json:"OrganizationID"`
	AttachmentScope                 pgtype.Text        `json:"AttachmentScope"`
	RemoteSessionIssuerID           uuid.UUID          `json:"RemoteSessionIssuerID"`
	ClientID                        string             `json:"ClientID"`
	ClientSecretEncrypted           pgtype.Text        `json:"ClientSecretEncrypted"`
	ClientIDIssuedAt                pgtype.Timestamptz `json:"ClientIDIssuedAt"`
	ClientSecretExpiresAt           pgtype.Timestamptz `json:"ClientSecretExpiresAt"`
	TokenEndpointAuthMethod         pgtype.Text        `json:"TokenEndpointAuthMethod"`
	JsonWebKeySetID                 uuid.NullUUID      `json:"JsonWebKeySetID"`
	Scope                           []string           `json:"Scope"`
	GrantTypes                      []string           `json:"GrantTypes"`
	Audience                        pgtype.Text        `json:"Audience"`
	TokenEndpointAuthAudienceFormat pgtype.Text        `json:"TokenEndpointAuthAudienceFormat"`
	ClientIDMetadataUri             pgtype.Text        `json:"ClientIDMetadataUri"`
	LegacyCallbackUrl               bool               `json:"LegacyCallbackUrl"`
	ResourceIdentifier              pgtype.Text        `json:"ResourceIdentifier"`
	ResourceName                    pgtype.Text        `json:"ResourceName"`
	ResourceDocumentation           pgtype.Text        `json:"ResourceDocumentation"`
	ResourcePolicyUri               pgtype.Text        `json:"ResourcePolicyUri"`
	ResourceTosUri                  pgtype.Text        `json:"ResourceTosUri"`
	UpstreamRejectedAt              pgtype.Timestamptz `json:"UpstreamRejectedAt"`
	IdentityProviderConnectionID    uuid.NullUUID      `json:"IdentityProviderConnectionID"`
	CreatedAt                       pgtype.Timestamptz `json:"CreatedAt"`
	UpdatedAt                       pgtype.Timestamptz `json:"UpdatedAt"`
	DeletedAt                       pgtype.Timestamptz `json:"DeletedAt"`
	Deleted                         bool               `json:"Deleted"`
}

type federatedIssuerSnapshot struct {
	ID                                         uuid.UUID          `json:"ID"`
	ProjectID                                  uuid.NullUUID      `json:"ProjectID"`
	OrganizationID                             pgtype.Text        `json:"OrganizationID"`
	AttachmentScope                            pgtype.Text        `json:"AttachmentScope"`
	Slug                                       string             `json:"Slug"`
	Issuer                                     string             `json:"Issuer"`
	AuthorizationEndpoint                      pgtype.Text        `json:"AuthorizationEndpoint"`
	TokenEndpoint                              pgtype.Text        `json:"TokenEndpoint"`
	RevocationEndpoint                         pgtype.Text        `json:"RevocationEndpoint"`
	RegistrationEndpoint                       pgtype.Text        `json:"RegistrationEndpoint"`
	JwksUri                                    pgtype.Text        `json:"JwksUri"`
	Jwks                                       []byte             `json:"Jwks"`
	JwksFetchedAt                              pgtype.Timestamptz `json:"JwksFetchedAt"`
	JwksLastError                              pgtype.Text        `json:"JwksLastError"`
	JwksLastErrorAt                            pgtype.Timestamptz `json:"JwksLastErrorAt"`
	JwksCacheExpiresAt                         pgtype.Timestamptz `json:"JwksCacheExpiresAt"`
	JwksEtag                                   pgtype.Text        `json:"JwksEtag"`
	ServiceDocumentation                       pgtype.Text        `json:"ServiceDocumentation"`
	OpPolicyUri                                pgtype.Text        `json:"OpPolicyUri"`
	OpTosUri                                   pgtype.Text        `json:"OpTosUri"`
	ScopesSupported                            []string           `json:"ScopesSupported"`
	GrantTypesSupported                        []string           `json:"GrantTypesSupported"`
	AuthorizationGrantProfilesSupported        []string           `json:"AuthorizationGrantProfilesSupported"`
	ResponseTypesSupported                     []string           `json:"ResponseTypesSupported"`
	TokenEndpointAuthMethodsSupported          []string           `json:"TokenEndpointAuthMethodsSupported"`
	CodeChallengeMethodsSupported              []string           `json:"CodeChallengeMethodsSupported"`
	ClientIDMetadataDocumentSupported          bool               `json:"ClientIDMetadataDocumentSupported"`
	UserinfoEndpoint                           pgtype.Text        `json:"UserinfoEndpoint"`
	IntrospectionEndpoint                      pgtype.Text        `json:"IntrospectionEndpoint"`
	IntrospectionEndpointAuthMethodsSupported  []string           `json:"IntrospectionEndpointAuthMethodsSupported"`
	IDTokenSigningAlgValuesSupported           []string           `json:"IDTokenSigningAlgValuesSupported"`
	ClaimsSupported                            []string           `json:"ClaimsSupported"`
	BackchannelLogoutSupported                 pgtype.Bool        `json:"BackchannelLogoutSupported"`
	AuthorizationResponseIssParameterSupported pgtype.Bool        `json:"AuthorizationResponseIssParameterSupported"`
	ScopeOverride                              []string           `json:"ScopeOverride"`
	ResourceIndicatorSupported                 pgtype.Bool        `json:"ResourceIndicatorSupported"`
	Oidc                                       bool               `json:"Oidc"`
	Passthrough                                bool               `json:"Passthrough"`
	TunneledMcpServerID                        uuid.NullUUID      `json:"TunneledMcpServerID"`
	Name                                       pgtype.Text        `json:"Name"`
	LogoAssetID                                uuid.NullUUID      `json:"LogoAssetID"`
	ClientSetupDocumentationUrl                pgtype.Text        `json:"ClientSetupDocumentationUrl"`
	Metadata                                   []byte             `json:"Metadata"`
	MetadataFetchedAt                          pgtype.Timestamptz `json:"MetadataFetchedAt"`
	MetadataLastError                          pgtype.Text        `json:"MetadataLastError"`
	MetadataLastErrorAt                        pgtype.Timestamptz `json:"MetadataLastErrorAt"`
	MetadataLastErrorUrl                       pgtype.Text        `json:"MetadataLastErrorUrl"`
	CreatedAt                                  pgtype.Timestamptz `json:"CreatedAt"`
	UpdatedAt                                  pgtype.Timestamptz `json:"UpdatedAt"`
	DeletedAt                                  pgtype.Timestamptz `json:"DeletedAt"`
	Deleted                                    bool               `json:"Deleted"`
}
