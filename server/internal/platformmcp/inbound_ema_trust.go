//nolint:exhaustruct // Management payloads and tool outputs use optional zero values.
package platformmcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	orgremotegen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_issuers"
	orgusergen "github.com/speakeasy-api/gram/server/gen/organization_user_session_issuers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	directoryrepo "github.com/speakeasy-api/gram/server/internal/directory/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/usersessions/authserver"
)

// InboundEMAUserIssuerManagement is the organization user session issuer
// management API. Calling it keeps its authorization, validation, locking and
// audit for every trust change.
type InboundEMAUserIssuerManagement interface {
	ListIssuers(context.Context, *orgusergen.ListIssuersPayload) (*orgusergen.ListOrganizationUserSessionIssuersResult, error)
	GetIssuer(context.Context, *orgusergen.GetIssuerPayload) (*types.UserSessionIssuer, error)
	UpdateIssuer(context.Context, *orgusergen.UpdateIssuerPayload) (*types.UserSessionIssuer, error)
}

// InboundEMARemoteIssuerManagement is the organization remote session issuer
// management API: metadata discovery, duplicate lookup, reads and creation.
type InboundEMARemoteIssuerManagement interface {
	FetchIssuerMetadata(context.Context, *orgremotegen.FetchIssuerMetadataPayload) (*types.RemoteSessionIssuerDraft, error)
	GetIssuerDuplicatePreflight(context.Context, *orgremotegen.GetIssuerDuplicatePreflightPayload) (*types.RemoteSessionIssuerDuplicatePreflight, error)
	GetIssuer(context.Context, *orgremotegen.GetIssuerPayload) (*types.RemoteSessionIssuer, error)
	CreateIssuer(context.Context, *orgremotegen.CreateIssuerPayload) (*types.RemoteSessionIssuer, error)
}

const (
	// inboundEMAIssuerListLimit bounds the organization-level issuers one read
	// reports. Organizations have one or two; the cap keeps the result small.
	inboundEMAIssuerListLimit = 20

	// inboundEMAUnlockTimeout bounds releasing the provider lock after the
	// request context ends; a failed release closes the session instead.
	inboundEMAUnlockTimeout = 5 * time.Second

	// inboundEMASlugPrefix names identity providers this flow creates.
	inboundEMASlugPrefix = "inbound-ema-"

	// Duplicate preflight tiers, as the remote issuer API names them.
	remoteIssuerTierOrganization = "organization-level"
	remoteIssuerTierPlatform     = "platform-level"
)

// Refusal codes the inbound EMA trust tools return.
const (
	inboundEMARefusalInvalidRequest       = "invalid_request"
	inboundEMARefusalNotFound             = "not_found"
	inboundEMARefusalNotOrganizationLevel = "not_organization_level"
	inboundEMARefusalNoKeySet             = "identity_provider_has_no_key_set"
	inboundEMARefusalMetadataUnavailable  = "identity_provider_metadata_unavailable"
	inboundEMARefusalBrowserSignIn        = "browser_sign_in_depends_on_trust"
	inboundEMARefusalRejected             = "rejected"
	inboundEMARefusalConflict             = "conflict"
	inboundEMARefusalPermissionDenied     = "permission_denied"
)

// InboundEMATrustRefusalError is a bounded, readable refusal.
type InboundEMATrustRefusalError struct {
	// Code is a stable machine-readable refusal code.
	Code string `json:"code"`

	// Message says what was refused and the next safe action.
	Message string `json:"message"`
}

func (e *InboundEMATrustRefusalError) Error() string { return e.Code + ": " + e.Message }

func inboundEMARefuse(code, message string) error {
	return &InboundEMATrustRefusalError{Code: code, Message: message}
}

// InboundEMAIdentityProvider is an identity provider an issuer trusts, or
// would trust, for inbound enterprise-managed authorization.
type InboundEMAIdentityProvider struct {
	// RemoteSessionIssuerID is the identity provider record, empty in a
	// preview that would create one.
	RemoteSessionIssuerID string `json:"remote_session_issuer_id,omitempty"`

	// IssuerURL is the identity provider's issuer, the iss of its ID-JAGs.
	IssuerURL string `json:"issuer_url"`

	// KeySetURL is where Speakeasy fetches the keys that verify ID-JAGs.
	KeySetURL string `json:"key_set_url,omitempty"`

	// Name is the record's display name.
	Name string `json:"name,omitempty"`

	// Tier is organization or platform.
	Tier string `json:"tier,omitempty" jsonschema:"organization for a record this organization owns; platform for a Speakeasy catalog record"`
}

// InboundEMAIssuer is one organization-level user session issuer's inbound
// enterprise-managed authorization state.
type InboundEMAIssuer struct {
	// UserSessionIssuerID is the organization-level user session issuer.
	UserSessionIssuerID string `json:"user_session_issuer_id"`

	// Slug is the issuer's slug.
	Slug string `json:"slug"`

	// AuthorizationServerMode is shared or endpoint.
	AuthorizationServerMode string `json:"authorization_server_mode"`

	// Audience is what to configure as Speakeasy's audience in the identity
	// provider, set for a shared authorization server.
	Audience string `json:"audience,omitempty" jsonschema:"the value the identity provider must put in the ID-JAG aud claim: this issuer's shared authorization server issuer URL"`

	// AudienceNote explains a missing Audience.
	AudienceNote string `json:"audience_note,omitempty"`

	// TrustedIdentityProvider is the identity provider ID-JAGs are accepted
	// from; absent when inbound enterprise-managed authorization is off.
	TrustedIdentityProvider *InboundEMAIdentityProvider `json:"trusted_identity_provider,omitempty"`

	// FederatedBrowserSignIn reports that interactive sign-in also goes
	// through the trusted identity provider, so the trust cannot be removed here.
	FederatedBrowserSignIn bool `json:"federated_browser_sign_in"`

	// OrganizationGatewayURL is the organization gateway, set when it signs
	// users in through this issuer.
	OrganizationGatewayURL string `json:"organization_gateway_url,omitempty"`
}

// InboundEMADirectory reports whether ID-JAG users can be resolved.
type InboundEMADirectory struct {
	// ResolvableDirectoryUsers counts active directory users linked, or
	// matched by email, to a live member, as ID-JAG user resolution requires.
	ResolvableDirectoryUsers int64 `json:"resolvable_directory_users"`

	// Synced is true when at least one directory user can be resolved.
	Synced bool `json:"synced" jsonschema:"false means every ID-JAG exchange is refused: users resolve only through an active synced directory user, with the ID-JAG's email, who is a live member"`
}

// GetInboundEMATrustOutput is the organization's inbound enterprise-managed
// authorization configuration.
type GetInboundEMATrustOutput struct {
	// OrganizationID is the caller's organization.
	OrganizationID string `json:"organization_id"`

	// Issuers are the organization-level user session issuers.
	Issuers []InboundEMAIssuer `json:"issuers"`

	// Truncated reports more issuers than were listed.
	Truncated bool `json:"truncated"`

	// Directory reports the directory sync users resolve through.
	Directory InboundEMADirectory `json:"directory"`

	// NextStep is the next safe action.
	NextStep string `json:"next_step"`
}

// TrustInboundEMAIdentityProviderInput names the issuer and identity provider.
type TrustInboundEMAIdentityProviderInput struct {
	// UserSessionIssuerID is the exact organization-level issuer to change.
	UserSessionIssuerID string `json:"user_session_issuer_id" jsonschema:"exact organization-level user session issuer UUID from get_inbound_ema_trust"`

	// IssuerURL is the identity provider's issuer URL.
	IssuerURL string `json:"issuer_url" jsonschema:"the identity provider's issuer URL, for example the Okta org or authorization server URL; its metadata must publish jwks_uri"`

	// ExpectedTrustedRemoteSessionIssuerID is the trust the preview read.
	ExpectedTrustedRemoteSessionIssuerID string `json:"expected_trusted_remote_session_issuer_id,omitempty" jsonschema:"on confirmed=true, the expected_trusted_remote_session_issuer_id the preview returned; empty means the issuer trusted no provider. A different current value is refused as a conflict"`

	// Confirmed applies the change; false only previews it.
	Confirmed bool `json:"confirmed" jsonschema:"false previews without changing anything; true only after the user confirms this exact issuer and identity provider"`
}

// TrustInboundEMAIdentityProviderOutput reports the preview or committed trust.
type TrustInboundEMAIdentityProviderOutput struct {
	// Applied is false for a preview.
	Applied bool `json:"applied"`

	// Changed reports whether the trust link changed, or would change.
	Changed bool `json:"changed"`

	// IdentityProviderReused is true when an existing record is used rather
	// than a new one created.
	IdentityProviderReused bool `json:"identity_provider_reused"`

	// IdentityProvider is the identity provider trusted, or to be trusted.
	IdentityProvider InboundEMAIdentityProvider `json:"identity_provider"`

	// ExpectedTrustedRemoteSessionIssuerID is the issuer's trust as read; pass
	// it back on the confirmed call.
	ExpectedTrustedRemoteSessionIssuerID string `json:"expected_trusted_remote_session_issuer_id"`

	// ReplacedRemoteSessionIssuerID is the identity provider the link pointed
	// at before, when the change replaces one.
	ReplacedRemoteSessionIssuerID string `json:"replaced_remote_session_issuer_id,omitempty"`

	// Warnings are conditions that may stop ID-JAGs from being accepted.
	Warnings []string `json:"warnings,omitempty"`

	// Issuer is the issuer's state, read again after a committed change.
	Issuer InboundEMAIssuer `json:"issuer"`

	// Directory reports the directory sync users resolve through.
	Directory InboundEMADirectory `json:"directory"`

	// NextStep is the next safe action.
	NextStep string `json:"next_step"`
}

// UntrustInboundEMAIdentityProviderInput names the issuer.
type UntrustInboundEMAIdentityProviderInput struct {
	// UserSessionIssuerID is the exact organization-level issuer to change.
	UserSessionIssuerID string `json:"user_session_issuer_id" jsonschema:"exact organization-level user session issuer UUID from get_inbound_ema_trust"`

	// ExpectedTrustedRemoteSessionIssuerID is the trust the preview read.
	ExpectedTrustedRemoteSessionIssuerID string `json:"expected_trusted_remote_session_issuer_id,omitempty" jsonschema:"on confirmed=true, the expected_trusted_remote_session_issuer_id the preview returned. A different current value is refused as a conflict"`

	// Confirmed applies the change; false only previews it.
	Confirmed bool `json:"confirmed" jsonschema:"false previews without changing anything; true only after the user confirms removing this issuer's trust"`
}

// UntrustInboundEMAIdentityProviderOutput reports the preview or committed removal.
type UntrustInboundEMAIdentityProviderOutput struct {
	// Applied is false for a preview.
	Applied bool `json:"applied"`

	// Changed reports whether the trust link changed, or would change.
	Changed bool `json:"changed"`

	// RemovedIdentityProvider is the identity provider no longer trusted, or
	// to be no longer trusted in a preview.
	RemovedIdentityProvider *InboundEMAIdentityProvider `json:"removed_identity_provider,omitempty"`

	// ExpectedTrustedRemoteSessionIssuerID is the issuer's trust as read; pass
	// it back on the confirmed call.
	ExpectedTrustedRemoteSessionIssuerID string `json:"expected_trusted_remote_session_issuer_id"`

	// Issuer is the issuer's state, read again after a committed change.
	Issuer InboundEMAIssuer `json:"issuer"`

	// NextStep is the next safe action.
	NextStep string `json:"next_step"`
}

// InboundEMATrustService configures which identity provider an
// organization-level issuer accepts ID-JAGs from, through the management API.
type InboundEMATrustService struct {
	users  InboundEMAUserIssuerManagement
	remote InboundEMARemoteIssuerManagement
	db     *pgxpool.Pool
	logger *slog.Logger

	// gateway reads the organization gateway; nil omits its URL.
	gateway func() *OrganizationGatewayService
}

// WithInboundEMATrust adds the inbound enterprise-managed authorization tools.
func (r *PostgresReader) WithInboundEMATrust(users InboundEMAUserIssuerManagement, remote InboundEMARemoteIssuerManagement) *PostgresReader {
	if users == nil || remote == nil {
		return r
	}
	r.inboundEMATrust = &InboundEMATrustService{users: users, remote: remote, db: r.db, logger: r.logger, gateway: func() *OrganizationGatewayService { return r.organizationGateway }}
	return r
}

func (s *InboundEMATrustService) valid() bool {
	return s != nil && s.users != nil && s.remote != nil && s.db != nil
}

func (s *InboundEMATrustService) Get(ctx context.Context, principal Principal) (GetInboundEMATrustOutput, error) {
	if !s.valid() {
		return GetInboundEMATrustOutput{}, ErrUnavailable
	}
	listed, err := s.users.ListIssuers(ctx, &orgusergen.ListIssuersPayload{Tier: new("organization"), Limit: new(inboundEMAIssuerListLimit)})
	if err != nil {
		return GetInboundEMATrustOutput{}, fmt.Errorf("list organization user session issuers: %w", err)
	}
	gateway, err := s.organizationGateway(ctx, principal)
	if err != nil {
		return GetInboundEMATrustOutput{}, err
	}
	output := GetInboundEMATrustOutput{OrganizationID: principal.OrganizationID, Issuers: []InboundEMAIssuer{}, Truncated: listed.NextCursor != nil}
	for _, issuer := range listed.Items {
		if issuer == nil || issuer.ProjectID != "" {
			continue
		}
		view, err := s.issuerState(ctx, issuer, gateway)
		if err != nil {
			return GetInboundEMATrustOutput{}, err
		}
		output.Issuers = append(output.Issuers, view)
	}
	output.Directory, err = s.directory(ctx, principal)
	if err != nil {
		return GetInboundEMATrustOutput{}, err
	}
	output.NextStep = inboundEMAReadNextStep(output)
	return output, nil
}

func (s *InboundEMATrustService) Trust(ctx context.Context, principal Principal, input TrustInboundEMAIdentityProviderInput) (TrustInboundEMAIdentityProviderOutput, error) {
	if !s.valid() {
		return TrustInboundEMAIdentityProviderOutput{}, ErrUnavailable
	}
	current, err := s.organizationIssuer(ctx, input.UserSessionIssuerID)
	if err != nil {
		return TrustInboundEMAIdentityProviderOutput{}, err
	}
	previous := conv.PtrValOr(current.TrustedRemoteSessionIssuerID, "")
	if input.Confirmed && strings.TrimSpace(input.ExpectedTrustedRemoteSessionIssuerID) != previous {
		return TrustInboundEMAIdentityProviderOutput{}, inboundEMARefuse(inboundEMARefusalConflict, "This issuer's trusted identity provider changed since the preview. Preview again and confirm with the user before retrying.")
	}
	issuerURL := strings.TrimSpace(input.IssuerURL)
	if issuerURL == "" {
		return TrustInboundEMAIdentityProviderOutput{}, inboundEMARefuse(inboundEMARefusalInvalidRequest, "Provide the identity provider's issuer URL.")
	}
	draft, err := s.remote.FetchIssuerMetadata(ctx, &orgremotegen.FetchIssuerMetadataPayload{Issuer: issuerURL})
	if err != nil {
		if shareable, ok := errors.AsType[*oops.ShareableError](err); ok && shareable.Code != oops.CodeForbidden && shareable.Code != oops.CodeUnauthorized && shareable.Code != oops.CodeUnexpected {
			return TrustInboundEMAIdentityProviderOutput{}, inboundEMARefuse(inboundEMARefusalMetadataUnavailable, "Speakeasy could not read the identity provider's metadata at that issuer URL: "+shareable.Error()+". Check the issuer URL and try again.")
		}
		return TrustInboundEMAIdentityProviderOutput{}, fmt.Errorf("fetch identity provider metadata: %w", err)
	}
	if conv.PtrValOr(draft.JwksURI, "") == "" {
		return TrustInboundEMAIdentityProviderOutput{}, inboundEMARefuse(inboundEMARefusalNoKeySet, "The identity provider's metadata publishes no jwks_uri, so Speakeasy could not verify its ID-JAGs. Use the issuer URL of an authorization server that publishes a key set.")
	}
	if current.TrustedRemoteSessionClientID != nil {
		// A trust shared with federated sign-in is only ever left as it is.
		existing, reused, err := s.reusableProvider(ctx, draft.Issuer)
		if err != nil {
			return TrustInboundEMAIdentityProviderOutput{}, err
		}
		if !reused || existing.RemoteSessionIssuerID != previous {
			return TrustInboundEMAIdentityProviderOutput{}, inboundEMARefuse(inboundEMARefusalBrowserSignIn, "Interactive sign-in on this issuer goes through its currently trusted identity provider, and replacing it here would break that sign-in. Change the sign-in identity provider in the dashboard (IDP and SSO) instead.")
		}
	}
	directory, err := s.directory(ctx, principal)
	if err != nil {
		return TrustInboundEMAIdentityProviderOutput{}, err
	}

	if !input.Confirmed {
		provider, reused, err := s.reusableProvider(ctx, draft.Issuer)
		if err != nil {
			return TrustInboundEMAIdentityProviderOutput{}, err
		}
		if !reused {
			provider = InboundEMAIdentityProvider{IssuerURL: draft.Issuer, KeySetURL: conv.PtrValOr(draft.JwksURI, ""), Name: inboundEMAProviderName(draft.Issuer), Tier: "organization"}
		}
		output := inboundEMATrustOutput(previous, provider, reused, directory)
		if authserver.Mode(current.AuthorizationServerMode) != authserver.ModeShared {
			output.Warnings = append(output.Warnings, "This issuer gives each MCP server its own authorization server, so ID-JAGs must name each server's URL as their audience, and Speakeasy may refuse an inbound-only trust on it. Prefer an issuer with a shared authorization server.")
		}
		gateway, err := s.organizationGateway(ctx, principal)
		if err != nil {
			return TrustInboundEMAIdentityProviderOutput{}, err
		}
		output.Issuer, err = s.issuerState(ctx, current, gateway)
		if err != nil {
			return TrustInboundEMAIdentityProviderOutput{}, err
		}
		output.NextStep = "Nothing has changed. Show the user this issuer, the identity provider, whether its record is reused or created, and any warnings. Once they agree, call again with confirmed=true and expected_trusted_remote_session_issuer_id set to the value returned here."
		return output, nil
	}

	provider, reused, err := s.findOrCreateProvider(ctx, principal, draft)
	if err != nil {
		return TrustInboundEMAIdentityProviderOutput{}, err
	}
	output := inboundEMATrustOutput(previous, provider, reused, directory)
	if output.Changed {
		if _, err := s.users.UpdateIssuer(ctx, &orgusergen.UpdateIssuerPayload{ID: current.ID, TrustedRemoteSessionIssuerID: new(provider.RemoteSessionIssuerID)}); err != nil {
			refused := inboundEMAManagementError("trust identity provider", err)
			if refusal, ok := errors.AsType[*InboundEMATrustRefusalError](refused); ok && !reused {
				refusal.Message += " The identity provider record " + provider.RemoteSessionIssuerID + " was created and kept; a retry reuses it."
			}
			return TrustInboundEMAIdentityProviderOutput{}, refused
		}
	}
	output.Applied = true
	committed, err := s.organizationIssuer(ctx, current.ID)
	if err != nil {
		return TrustInboundEMAIdentityProviderOutput{}, err
	}
	gateway, err := s.organizationGateway(ctx, principal)
	if err != nil {
		return TrustInboundEMAIdentityProviderOutput{}, err
	}
	output.Issuer, err = s.issuerState(ctx, committed, gateway)
	if err != nil {
		return TrustInboundEMAIdentityProviderOutput{}, err
	}
	output.ExpectedTrustedRemoteSessionIssuerID = conv.PtrValOr(committed.TrustedRemoteSessionIssuerID, "")
	output.NextStep = inboundEMATrustNextStep(output.Issuer, output.Directory)
	return output, nil
}

func inboundEMATrustOutput(previous string, provider InboundEMAIdentityProvider, reused bool, directory InboundEMADirectory) TrustInboundEMAIdentityProviderOutput {
	output := TrustInboundEMAIdentityProviderOutput{
		Changed:                              !reused || previous != provider.RemoteSessionIssuerID,
		IdentityProviderReused:               reused,
		IdentityProvider:                     provider,
		ExpectedTrustedRemoteSessionIssuerID: previous,
		Directory:                            directory,
	}
	if previous != "" && output.Changed {
		output.ReplacedRemoteSessionIssuerID = previous
	}
	return output
}

// findOrCreateProvider reuses the record for the discovered issuer or creates
// one. A per-organization, per-issuer advisory lock held across the lookup
// and the create keeps concurrent trusts from creating two records.
func (s *InboundEMATrustService) findOrCreateProvider(ctx context.Context, principal Principal, draft *types.RemoteSessionIssuerDraft) (InboundEMAIdentityProvider, bool, error) {
	conn, err := s.db.Acquire(ctx)
	if err != nil {
		return InboundEMAIdentityProvider{}, false, fmt.Errorf("acquire identity provider lock connection: %w", err)
	}
	queries := platformrepo.New(conn)
	lock := platformrepo.AcquireInboundEMAProviderLockParams{OrganizationID: principal.OrganizationID, Issuer: draft.Issuer}
	if err := queries.AcquireInboundEMAProviderLock(ctx, lock); err != nil {
		conn.Release()
		return InboundEMAIdentityProvider{}, false, fmt.Errorf("lock identity provider creation: %w", err)
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), inboundEMAUnlockTimeout)
		defer cancel()
		unlocked, unlockErr := queries.ReleaseInboundEMAProviderLock(unlockCtx, platformrepo.ReleaseInboundEMAProviderLockParams(lock))
		if unlockErr != nil || !unlocked {
			if s.logger != nil {
				s.logger.ErrorContext(unlockCtx, "release inbound EMA identity provider lock", attr.SlogError(unlockErr), attr.SlogOrganizationID(principal.OrganizationID))
			}
			// Closing the session drops a lock it still holds.
			_ = conn.Hijack().Close(unlockCtx)
			return
		}
		conn.Release()
	}()

	provider, reused, err := s.reusableProvider(ctx, draft.Issuer)
	if err != nil || reused {
		return provider, reused, err
	}
	created, err := s.remote.CreateIssuer(ctx, inboundEMACreatePayload(draft))
	if err != nil {
		return InboundEMAIdentityProvider{}, false, inboundEMAManagementError("create identity provider", err)
	}
	return inboundEMAProvider(created), false, nil
}

func (s *InboundEMATrustService) Untrust(ctx context.Context, principal Principal, input UntrustInboundEMAIdentityProviderInput) (UntrustInboundEMAIdentityProviderOutput, error) {
	if !s.valid() {
		return UntrustInboundEMAIdentityProviderOutput{}, ErrUnavailable
	}
	current, err := s.organizationIssuer(ctx, input.UserSessionIssuerID)
	if err != nil {
		return UntrustInboundEMAIdentityProviderOutput{}, err
	}
	if current.TrustedRemoteSessionClientID != nil {
		return UntrustInboundEMAIdentityProviderOutput{}, inboundEMARefuse(inboundEMARefusalBrowserSignIn, "Interactive sign-in on this issuer goes through its trusted identity provider, so removing the trust here would break that sign-in. Change the sign-in identity provider in the dashboard (IDP and SSO) instead.")
	}
	previous := conv.PtrValOr(current.TrustedRemoteSessionIssuerID, "")
	if input.Confirmed && strings.TrimSpace(input.ExpectedTrustedRemoteSessionIssuerID) != previous {
		return UntrustInboundEMAIdentityProviderOutput{}, inboundEMARefuse(inboundEMARefusalConflict, "This issuer's trusted identity provider changed since the preview. Preview again and confirm with the user before retrying.")
	}
	gateway, err := s.organizationGateway(ctx, principal)
	if err != nil {
		return UntrustInboundEMAIdentityProviderOutput{}, err
	}
	state, err := s.issuerState(ctx, current, gateway)
	if err != nil {
		return UntrustInboundEMAIdentityProviderOutput{}, err
	}
	output := UntrustInboundEMAIdentityProviderOutput{Changed: state.TrustedIdentityProvider != nil, RemovedIdentityProvider: state.TrustedIdentityProvider, ExpectedTrustedRemoteSessionIssuerID: previous, Issuer: state}
	if !input.Confirmed {
		output.NextStep = "Nothing has changed. Removing the trust refuses every ID-JAG this issuer would accept. Once the user agrees, call again with confirmed=true and expected_trusted_remote_session_issuer_id set to the value returned here."
		if !output.Changed {
			output.NextStep = "This issuer trusts no identity provider; there is nothing to remove."
		}
		return output, nil
	}
	if output.Changed {
		if _, err := s.users.UpdateIssuer(ctx, &orgusergen.UpdateIssuerPayload{ID: current.ID, TrustedRemoteSessionIssuerID: new("")}); err != nil {
			return UntrustInboundEMAIdentityProviderOutput{}, inboundEMAManagementError("remove identity provider trust", err)
		}
	}
	output.Applied = true
	committed, err := s.organizationIssuer(ctx, current.ID)
	if err != nil {
		return UntrustInboundEMAIdentityProviderOutput{}, err
	}
	output.Issuer, err = s.issuerState(ctx, committed, gateway)
	if err != nil {
		return UntrustInboundEMAIdentityProviderOutput{}, err
	}
	output.ExpectedTrustedRemoteSessionIssuerID = conv.PtrValOr(committed.TrustedRemoteSessionIssuerID, "")
	output.NextStep = "ID-JAGs for this issuer are now refused. The identity provider record is kept; delete it in the dashboard if nothing else uses it."
	return output, nil
}

// organizationIssuer reads one exact issuer and requires the organization tier.
func (s *InboundEMATrustService) organizationIssuer(ctx context.Context, rawID string) (*types.UserSessionIssuer, error) {
	id, err := uuid.Parse(strings.TrimSpace(rawID))
	if err != nil || id == uuid.Nil {
		return nil, inboundEMARefuse(inboundEMARefusalInvalidRequest, "Provide an exact user session issuer UUID from get_inbound_ema_trust.")
	}
	issuer, err := s.users.GetIssuer(ctx, &orgusergen.GetIssuerPayload{ID: id.String()})
	if err != nil {
		return nil, inboundEMAManagementError("read user session issuer", err)
	}
	if issuer.ProjectID != "" {
		return nil, inboundEMARefuse(inboundEMARefusalNotOrganizationLevel, "Only organization-level issuers accept ID-JAGs. Choose an issuer listed by get_inbound_ema_trust.")
	}
	return issuer, nil
}

// reusableProvider is the organization-level, else platform-level, record
// whose issuer is exactly issuerURL and that publishes a key set. The
// validator compares an ID-JAG's iss to the record byte for byte, so a record
// spelled differently is never reused.
func (s *InboundEMATrustService) reusableProvider(ctx context.Context, issuerURL string) (InboundEMAIdentityProvider, bool, error) {
	preflight, err := s.remote.GetIssuerDuplicatePreflight(ctx, &orgremotegen.GetIssuerDuplicatePreflightPayload{Issuer: &issuerURL})
	if err != nil {
		return InboundEMAIdentityProvider{}, false, fmt.Errorf("find existing identity provider: %w", err)
	}
	withoutKeys := ""
	for _, tier := range []string{remoteIssuerTierOrganization, remoteIssuerTierPlatform} {
		for _, match := range preflight.Matches {
			if match == nil || match.Tier != tier || match.Issuer != issuerURL {
				continue
			}
			record, err := s.remote.GetIssuer(ctx, &orgremotegen.GetIssuerPayload{ID: match.ID})
			if err != nil {
				return InboundEMAIdentityProvider{}, false, fmt.Errorf("read existing identity provider: %w", err)
			}
			if record.Issuer != issuerURL {
				continue
			}
			if conv.PtrValOr(record.JwksURI, "") == "" {
				withoutKeys = record.ID
				continue
			}
			return inboundEMAProvider(record), true, nil
		}
	}
	if withoutKeys != "" {
		return InboundEMAIdentityProvider{}, false, inboundEMARefuse(inboundEMARefusalNoKeySet, "An identity provider record for this issuer URL already exists ("+withoutKeys+") but has no key set URL. Refresh its metadata in the dashboard (IDP and SSO > Identity providers), then try again.")
	}
	return InboundEMAIdentityProvider{}, false, nil
}

// organizationGateway reads the organization gateway once per call.
func (s *InboundEMATrustService) organizationGateway(ctx context.Context, principal Principal) (organizationGatewayIssuer, error) {
	if s.gateway == nil {
		return organizationGatewayIssuer{}, nil
	}
	gateway := s.gateway()
	if !gateway.valid() {
		return organizationGatewayIssuer{}, nil
	}
	return gateway.servingIssuer(ctx, principal.OrganizationID)
}

func (s *InboundEMATrustService) issuerState(ctx context.Context, issuer *types.UserSessionIssuer, gateway organizationGatewayIssuer) (InboundEMAIssuer, error) {
	state := InboundEMAIssuer{
		UserSessionIssuerID:     issuer.ID,
		Slug:                    issuer.Slug,
		AuthorizationServerMode: issuer.AuthorizationServerMode,
		FederatedBrowserSignIn:  issuer.TrustedRemoteSessionClientID != nil,
	}
	switch {
	case authserver.Mode(issuer.AuthorizationServerMode) != authserver.ModeShared:
		state.AudienceNote = "This issuer gives each MCP server its own authorization server, so there is no single audience. Switch it to a shared authorization server in the dashboard to accept ID-JAGs."
	case issuer.IssuerURL == nil || *issuer.IssuerURL == "":
		state.AudienceNote = "This issuer's shared authorization server is unavailable: " + conv.PtrValOr(issuer.AuthorizationServerUnavailableReason, "unknown") + "."
	default:
		state.Audience = *issuer.IssuerURL
	}
	if trusted := conv.PtrValOr(issuer.TrustedRemoteSessionIssuerID, ""); trusted != "" {
		record, err := s.remote.GetIssuer(ctx, &orgremotegen.GetIssuerPayload{ID: trusted})
		if err != nil {
			return InboundEMAIssuer{}, fmt.Errorf("read trusted identity provider: %w", err)
		}
		provider := inboundEMAProvider(record)
		state.TrustedIdentityProvider = &provider
	}
	if gateway.IssuerID == issuer.ID {
		state.OrganizationGatewayURL = gateway.URL
	}
	return state, nil
}

func (s *InboundEMATrustService) directory(ctx context.Context, principal Principal) (InboundEMADirectory, error) {
	count, err := directoryrepo.New(s.db).CountIDJAGResolvableDirectoryUsers(ctx, principal.OrganizationID)
	if err != nil {
		return InboundEMADirectory{}, fmt.Errorf("count resolvable directory users: %w", err)
	}
	return InboundEMADirectory{ResolvableDirectoryUsers: count, Synced: count > 0}, nil
}

func inboundEMAProvider(record *types.RemoteSessionIssuer) InboundEMAIdentityProvider {
	tier := "organization"
	if record.OrganizationID == "" {
		tier = "platform"
	}
	return InboundEMAIdentityProvider{
		RemoteSessionIssuerID: record.ID,
		IssuerURL:             record.Issuer,
		KeySetURL:             conv.PtrValOr(record.JwksURI, ""),
		Name:                  conv.PtrValOr(record.Name, ""),
		Tier:                  tier,
	}
}

// inboundEMAProviderName names a new record after its issuer's host.
func inboundEMAProviderName(issuerURL string) string {
	parsed, err := url.Parse(issuerURL)
	if err != nil || parsed.Host == "" {
		return issuerURL
	}
	return parsed.Host
}

// inboundEMAProviderSlug is stable per issuer URL, so a retry after a failed
// trust update finds the record it created rather than adding another.
func inboundEMAProviderSlug(issuerURL string) string {
	sum := sha256.Sum256([]byte(issuerURL))
	return inboundEMASlugPrefix + hex.EncodeToString(sum[:8])
}

// inboundEMACreatePayload creates an organization-level identity provider
// from discovered metadata, as the dashboard's create form does.
func inboundEMACreatePayload(draft *types.RemoteSessionIssuerDraft) *orgremotegen.CreateIssuerPayload {
	return &orgremotegen.CreateIssuerPayload{
		Slug:                                inboundEMAProviderSlug(draft.Issuer),
		Issuer:                              draft.Issuer,
		Name:                                new(inboundEMAProviderName(draft.Issuer)),
		AuthorizationEndpoint:               draft.AuthorizationEndpoint,
		TokenEndpoint:                       draft.TokenEndpoint,
		RevocationEndpoint:                  draft.RevocationEndpoint,
		RegistrationEndpoint:                draft.RegistrationEndpoint,
		JwksURI:                             draft.JwksURI,
		ServiceDocumentation:                draft.ServiceDocumentation,
		OpPolicyURI:                         draft.OpPolicyURI,
		OpTosURI:                            draft.OpTosURI,
		ScopesSupported:                     draft.ScopesSupported,
		GrantTypesSupported:                 draft.GrantTypesSupported,
		AuthorizationGrantProfilesSupported: draft.AuthorizationGrantProfilesSupported,
		ResponseTypesSupported:              draft.ResponseTypesSupported,
		TokenEndpointAuthMethodsSupported:   draft.TokenEndpointAuthMethodsSupported,
		CodeChallengeMethodsSupported:       draft.CodeChallengeMethodsSupported,
		Oidc:                                new(draft.Oidc),
		ClientIDMetadataDocumentSupported:   new(draft.ClientIDMetadataDocumentSupported),
		UserinfoEndpoint:                    draft.UserinfoEndpoint,
		IntrospectionEndpoint:               draft.IntrospectionEndpoint,
		IntrospectionEndpointAuthMethodsSupported:  draft.IntrospectionEndpointAuthMethodsSupported,
		IDTokenSigningAlgValuesSupported:           draft.IDTokenSigningAlgValuesSupported,
		ClaimsSupported:                            draft.ClaimsSupported,
		BackchannelLogoutSupported:                 new(draft.BackchannelLogoutSupported),
		AuthorizationResponseIssParameterSupported: new(draft.AuthorizationResponseIssParameterSupported),
	}
}

// inboundEMAManagementError turns a management API refusal into a readable
// tool refusal; unexpected failures stay errors.
func inboundEMAManagementError(what string, err error) error {
	shareable, ok := errors.AsType[*oops.ShareableError](err)
	if !ok {
		return fmt.Errorf("%s: %w", what, err)
	}
	switch shareable.Code {
	case oops.CodeNotFound:
		return inboundEMARefuse(inboundEMARefusalNotFound, "That issuer or identity provider is not available in this organization. Read get_inbound_ema_trust again.")
	case oops.CodeForbidden, oops.CodeUnauthorized:
		return inboundEMARefuse(inboundEMARefusalPermissionDenied, "Changing inbound enterprise-managed authorization requires org:admin.")
	case oops.CodeBadRequest, oops.CodeInvalid:
		return inboundEMARefuse(inboundEMARefusalRejected, "Speakeasy refused this change: "+shareable.Error()+".")
	case oops.CodeConflict:
		return inboundEMARefuse(inboundEMARefusalConflict, "Speakeasy refused this change because it conflicts with current configuration: "+shareable.Error()+". Read get_inbound_ema_trust again before retrying.")
	default:
		return fmt.Errorf("%s: %w", what, err)
	}
}

func inboundEMAReadNextStep(output GetInboundEMATrustOutput) string {
	if len(output.Issuers) == 0 {
		return "This organization has no organization-level user session issuer. Create one with a shared authorization server in the dashboard first."
	}
	if !output.Directory.Synced {
		return "No directory users are synced, so every ID-JAG exchange is refused. Set up directory sync on IDP and SSO first; Speakeasy never creates users from an ID-JAG."
	}
	for _, issuer := range output.Issuers {
		if issuer.TrustedIdentityProvider != nil {
			return "Configure the identity provider to issue ID-JAGs with the reported audience. Users resolve only through an active synced directory user, with the ID-JAG's email, who is a live member."
		}
	}
	return "To accept ID-JAGs, call trust_inbound_ema_identity_provider with an issuer and the identity provider's issuer URL."
}

func inboundEMATrustNextStep(issuer InboundEMAIssuer, directory InboundEMADirectory) string {
	step := "In the identity provider, issue ID-JAGs for Speakeasy with audience " + issuer.Audience + "."
	if issuer.Audience == "" {
		step = issuer.AudienceNote
	}
	if !directory.Synced {
		step += " No directory users are synced yet, so every exchange is refused until directory sync is set up."
	}
	return step + " The trust does not prove the identity provider issues valid ID-JAGs; test a sign-in."
}
