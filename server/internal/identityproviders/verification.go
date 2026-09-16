package identityproviders

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/identity_providers"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/identityproviders/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/okta"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	capabilityDirectoryRead             = "directory_read"
	capabilityApplicationAssignmentRead = "application_assignment_read"
	capabilityGroupAssignment           = "group_assignment"
	capabilitySignInProvisioning        = "sign_in_provisioning"
	capabilityClaimsProvisioning        = "claims_provisioning"
)

var requestedOktaScopes = []string{
	"okta.apps.read",
	"okta.groups.read",
	"okta.groups.manage",
	"okta.users.read",
	"okta.apps.manage",
	"okta.authorizationServers.read",
	"okta.authorizationServers.manage",
}

var requiredOktaReadScopes = []string{
	"okta.apps.read",
	"okta.groups.read",
	"okta.users.read",
}

var oktaApplicationProvisioningScopes = []string{
	"okta.apps.read",
	"okta.groups.read",
	"okta.users.read",
	"okta.apps.manage",
}

var oktaDirectoryProvisioningScopes = []string{
	"okta.apps.read",
	"okta.groups.read",
	"okta.groups.manage",
	"okta.users.read",
	"okta.apps.manage",
}

var oktaClaimsProvisioningScopes = []string{
	"okta.authorizationServers.read",
	"okta.authorizationServers.manage",
}

var oktaClaimsVerificationScopes = []string{
	"okta.apps.read",
	"okta.groups.read",
	"okta.users.read",
	"okta.authorizationServers.read",
	"okta.authorizationServers.manage",
}

// OktaClient is the provider boundary used by identity provider verification.
type OktaClient interface {
	AcquireToken(context.Context, okta.TokenRequest) (okta.Token, error)
	AcquireFreshToken(context.Context, okta.TokenRequest) (okta.Token, error)
	InvalidateToken(uuid.UUID)
	ListGroups(context.Context, string, string, okta.PageRequest) (okta.Page, error)
	ListUsers(context.Context, string, string, okta.PageRequest) (okta.Page, error)
	ListApplications(context.Context, string, string, okta.PageRequest) (okta.Page, error)
	ListApplicationsOnce(context.Context, string, string, okta.PageRequest) (okta.Page, error)
	ListApplicationGroups(context.Context, string, string, string, okta.PageRequest) (okta.Page, error)
	ListApplicationGroupsOnce(context.Context, string, string, string, okta.PageRequest) (okta.Page, error)
	GetGroupOnce(context.Context, string, string, string) (okta.Group, okta.RateLimit, error)
	ListApplicationUsers(context.Context, string, string, string, okta.PageRequest) (okta.Page, error)
	ListApplicationUsersOnce(context.Context, string, string, string, okta.PageRequest) (okta.Page, error)
	ListAuthorizationServers(context.Context, string, string) ([]okta.AuthorizationServer, error)
	CreateOIDCApplication(context.Context, string, string, okta.CreateOIDCApplicationInput) (okta.Application, error)
	CreateDirectoryApplication(context.Context, string, string) (okta.Application, error)
	EnsureOIDCApplicationRedirectURI(context.Context, string, string, string, string) error
	EnsureSignInClaims(context.Context, string, string, string) ([]string, error)
	FindActiveApplicationByLabel(context.Context, string, string, string) (okta.Application, string, bool, error)
	GetApplication(context.Context, string, string, string) (okta.Application, error)
	GetProvisioningConnection(context.Context, string, string, string) (okta.ProvisioningConnection, error)
	ResolveApplicationByClientID(context.Context, string, string, string) (okta.Application, error)
	FindEveryoneGroup(context.Context, string, string) (okta.Group, error)
	AssignGroupToApplication(context.Context, string, string, string, string) error
}

// WorkOSClient is the provider boundary used by sign-in verification.
type WorkOSClient interface {
	ListConnections(context.Context, string) ([]workos.Connection, error)
	CreateOIDCConnection(context.Context, workos.CreateOIDCConnectionInput) (workos.Connection, error)
	GetConnection(context.Context, string) (workos.Connection, error)
	ListDirectories(context.Context, string) ([]workos.Directory, error)
	ListDirectoryGroups(context.Context, string) ([]workos.DirectoryGroup, error)
	ListDirectoryUsers(context.Context, string) ([]workos.DirectoryUser, error)
}

type storedVerification struct {
	Outcome       string                   `json:"outcome"`
	Detail        string                   `json:"detail"`
	Capabilities  []string                 `json:"capabilities"`
	GrantedScopes []string                 `json:"granted_scopes"`
	CheckedAt     string                   `json:"checked_at"`
	Reads         []storedVerificationRead `json:"reads"`
}

type storedVerificationRead struct {
	Capability string  `json:"capability"`
	Resource   string  `json:"resource"`
	OK         bool    `json:"ok"`
	Count      *int    `json:"count,omitempty"`
	Detail     *string `json:"detail,omitempty"`
}

func (s *Service) VerifySetupStep(ctx context.Context, payload *gen.VerifySetupStepPayload) (*gen.IdentityProviderVerifyResult, error) {
	authCtx, logger, err := s.requireAccess(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}
	if payload.StepKey == setupStepSignIn {
		return s.verifySignInSetupStep(ctx, authCtx, logger)
	}
	if payload.StepKey == setupStepDirectory {
		return s.verifyDirectorySetupStep(ctx, authCtx, logger)
	}
	if payload.StepKey != setupStepConnect {
		return nil, oops.E(oops.CodeBadRequest, nil, "unknown identity provider setup step").LogError(ctx, logger)
	}

	before, err := repo.New(s.db).GetIdentityProviderConnectionByOrganization(ctx, authCtx.ActiveOrganizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, nil, "identity provider connection not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error loading identity provider connection").LogError(ctx, logger)
	}
	if !before.ClientID.Valid || strings.TrimSpace(before.ClientID.String) == "" || before.Status == "pending" {
		return nil, oops.E(oops.CodeBadRequest, nil, "Okta Client ID is required before verification").LogError(ctx, logger)
	}
	if before.Status != "awaiting_verification" && before.Status != "active" && before.Status != "failed" {
		return nil, oops.E(oops.CodeBadRequest, nil, "identity provider connection is not ready for verification").LogError(ctx, logger)
	}

	previousResult, err := mv.BuildIdentityProviderVerifyResultView(before)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error loading prior identity provider verification").LogError(ctx, logger)
	}
	previousOutcome := ""
	if previousResult != nil {
		previousOutcome = previousResult.Outcome
	}

	signingKey, err := repo.New(s.db).GetConfiguredIdentityProviderSigningKey(ctx, repo.GetConfiguredIdentityProviderSigningKeyParams{
		OrganizationID:               authCtx.ActiveOrganizationID,
		IdentityProviderConnectionID: before.ID,
		SigningKeyID:                 before.SigningKeyID.UUID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error loading identity provider signing key").LogError(ctx, logger)
	}
	privateKey, err := s.decryptSigningKey(signingKey.PrivateKeyEncrypted)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error loading identity provider signing key").LogError(ctx, logger)
	}

	tenantDomain := normalizeOktaDomain(before.TenantIdentifier)
	checkedAt := time.Now().UTC()
	var token okta.Token
	var tokenErr error
	for _, scopes := range [][]string{requestedOktaScopes, oktaClaimsVerificationScopes, oktaDirectoryProvisioningScopes, oktaApplicationProvisioningScopes, requiredOktaReadScopes} {
		token, tokenErr = s.okta.AcquireFreshToken(ctx, okta.TokenRequest{
			ConnectionID: before.ID,
			TenantDomain: tenantDomain,
			ClientID:     before.ClientID.String,
			KeyID:        signingKey.Kid,
			PrivateKey:   privateKey,
			Scopes:       append([]string(nil), scopes...),
		})
		if tokenErr == nil {
			break
		}
		if apiErr, ok := errors.AsType[*okta.APIError](tokenErr); !ok || apiErr.Code != "invalid_scope" {
			break
		}
	}
	var result *gen.IdentityProviderVerifyResult
	tokenUsed := tokenErr == nil
	if tokenErr != nil {
		result = tokenFailureResult(checkedAt, tokenErr)
	} else {
		result = s.probeCapabilities(ctx, checkedAt, tenantDomain, token)
	}

	stored, err := marshalStoredVerification(result)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error encoding identity provider verification").LogError(ctx, logger)
	}
	status := "failed"
	if result.Outcome == "passed" {
		status = "active"
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving identity provider verification").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)
	if err := queries.UpdateOktaIdentityProviderGrantedScopes(ctx, repo.UpdateOktaIdentityProviderGrantedScopesParams{
		GrantedScopes:                result.GrantedScopes,
		OrganizationID:               authCtx.ActiveOrganizationID,
		IdentityProviderConnectionID: before.ID,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving Okta granted scopes").LogError(ctx, logger)
	}
	if tokenUsed {
		if err := queries.MarkIdentityProviderSigningKeyUsed(ctx, repo.MarkIdentityProviderSigningKeyUsedParams{
			LastUsedAt:                   conv.ToPGTimestamptz(checkedAt),
			OrganizationID:               authCtx.ActiveOrganizationID,
			IdentityProviderConnectionID: before.ID,
			ID:                           signingKey.ID,
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error marking identity provider signing key used").LogError(ctx, logger)
		}
	}
	updated, err := queries.UpdateIdentityProviderVerification(ctx, repo.UpdateIdentityProviderVerificationParams{
		Status:                       status,
		StatusDetail:                 conv.ToPGText(result.Detail),
		Capabilities:                 result.Capabilities,
		LastVerifiedAt:               conv.ToPGTimestamptz(checkedAt),
		VerifyEvidence:               stored,
		OrganizationID:               authCtx.ActiveOrganizationID,
		IdentityProviderConnectionID: before.ID,
		ClientID:                     before.ClientID,
		SigningKeyID:                 before.SigningKeyID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving identity provider verification").LogError(ctx, logger)
	}
	if updated != 1 {
		return nil, oops.E(oops.CodeConflict, nil, "identity provider configuration changed during verification").LogError(ctx, logger)
	}
	after, err := queries.GetIdentityProviderConnectionByOrganization(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error loading verified identity provider connection").LogError(ctx, logger)
	}
	if err := s.audit.LogIdentityProviderConnectionVerified(ctx, dbtx, audit.LogIdentityProviderConnectionVerifiedEvent{
		OrganizationID:                           authCtx.ActiveOrganizationID,
		Actor:                                    urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:                         authCtx.Email,
		ActorSlug:                                nil,
		IdentityProviderConnectionURN:            urn.NewIdentityProviderConnectionID(after.ID),
		TenantIdentifier:                         after.TenantIdentifier,
		IdentityProviderConnectionSnapshotBefore: identityProviderConnectionSnapshot(before, previousOutcome),
		IdentityProviderConnectionSnapshotAfter:  identityProviderConnectionSnapshot(after, result.Outcome),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error recording identity provider verification").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving identity provider verification").LogError(ctx, logger)
	}
	if tokenErr != nil || result.Outcome == "capability_missing" {
		s.okta.InvalidateToken(before.ID)
	}
	return result, nil
}

func (s *Service) probeCapabilities(ctx context.Context, checkedAt time.Time, tenantDomain string, token okta.Token) *gen.IdentityProviderVerifyResult {
	granted := make(map[string]bool, len(token.GrantedScopes))
	for _, scope := range token.GrantedScopes {
		granted[scope] = true
	}
	pageRequest := okta.PageRequest{Limit: 1, After: ""}
	groups, groupsFailure := probeOktaCollection(ctx, capabilityDirectoryRead, "groups", "okta.groups.read", granted, func(ctx context.Context) (okta.Page, error) {
		return s.okta.ListGroups(ctx, tenantDomain, token.AccessToken, pageRequest)
	})
	users, usersFailure := probeOktaCollection(ctx, capabilityDirectoryRead, "users", "okta.users.read", granted, func(ctx context.Context) (okta.Page, error) {
		return s.okta.ListUsers(ctx, tenantDomain, token.AccessToken, pageRequest)
	})
	apps, appsFailure := probeOktaCollection(ctx, capabilityApplicationAssignmentRead, "apps", "okta.apps.read", granted, func(ctx context.Context) (okta.Page, error) {
		return s.okta.ListApplications(ctx, tenantDomain, token.AccessToken, pageRequest)
	})

	authorizationServers := &gen.IdentityProviderCapabilityRead{
		Capability: capabilityClaimsProvisioning,
		Resource:   "authorization_servers",
		OK:         false,
		Count:      nil,
		Detail:     nil,
	}
	if !granted["okta.authorizationServers.read"] || !granted["okta.authorizationServers.manage"] {
		detail := "Okta did not grant both optional authorization server scopes."
		authorizationServers.Detail = &detail
	} else {
		servers, err := s.okta.ListAuthorizationServers(ctx, tenantDomain, token.AccessToken)
		count := len(servers)
		authorizationServers.Count = &count
		switch {
		case err != nil:
			detail := "Unable to read Okta authorization_servers."
			if apiErr, ok := errors.AsType[*okta.APIError](err); ok && apiErr.Description != "" {
				detail = apiErr.Description
			}
			authorizationServers.Detail = &detail
		default:
			for _, server := range servers {
				if server.Name == "default" && server.ID != "" {
					authorizationServers.OK = true
					break
				}
			}
			detail := fmt.Sprintf("Found %d custom authorization server(s), including default.", count)
			if !authorizationServers.OK {
				detail = fmt.Sprintf("Found %d custom authorization server(s), but none named default.", count)
			}
			authorizationServers.Detail = &detail
		}
	}

	capabilities := make([]string, 0, 5)
	missing := make([]string, 0, 3)
	if groups.OK && users.OK {
		capabilities = append(capabilities, capabilityDirectoryRead)
	} else {
		missing = append(missing, groupsFailure, usersFailure)
	}
	if apps.OK {
		capabilities = append(capabilities, capabilityApplicationAssignmentRead)
	} else {
		missing = append(missing, appsFailure)
	}
	if granted["okta.apps.manage"] {
		capabilities = append(capabilities, capabilitySignInProvisioning)
	}
	if apps.OK && groups.OK && granted["okta.apps.manage"] && granted["okta.groups.manage"] {
		capabilities = append(capabilities, capabilityGroupAssignment)
	}
	if authorizationServers.OK {
		capabilities = append(capabilities, capabilityClaimsProvisioning)
	}

	outcome := "passed"
	detail := "Okta connection verified. Read counts are from the first page of each collection."
	if len(missing) > 0 {
		outcome = "capability_missing"
		detail = "Required Okta capabilities were not proven: " + strings.Join(compactStrings(missing), "; ") + "."
	}
	return &gen.IdentityProviderVerifyResult{
		Outcome:       outcome,
		Detail:        detail,
		Capabilities:  capabilities,
		GrantedScopes: append([]string(nil), token.GrantedScopes...),
		Evidence: &gen.IdentityProviderVerifyEvidence{
			CheckedAt: checkedAt.Format(time.RFC3339Nano),
			Reads:     []*gen.IdentityProviderCapabilityRead{groups, users, apps, authorizationServers},
		},
	}
}

func probeOktaCollection(
	ctx context.Context,
	capability string,
	resource string,
	requiredScope string,
	granted map[string]bool,
	read func(context.Context) (okta.Page, error),
) (*gen.IdentityProviderCapabilityRead, string) {
	if !granted[requiredScope] {
		detail := "Okta did not grant required scope " + requiredScope + "."
		return &gen.IdentityProviderCapabilityRead{Capability: capability, Resource: resource, OK: false, Count: nil, Detail: &detail}, "missing scope " + requiredScope
	}

	page, err := read(ctx)
	if err != nil {
		detail := "Unable to read Okta " + resource + "."
		if apiErr, ok := errors.AsType[*okta.APIError](err); ok {
			if apiErr.Description != "" {
				detail = apiErr.Description
			} else {
				detail = fmt.Sprintf("Okta returned status %d while reading %s.", apiErr.StatusCode, resource)
			}
		}
		return &gen.IdentityProviderCapabilityRead{Capability: capability, Resource: resource, OK: false, Count: nil, Detail: &detail}, resource + " read failed"
	}

	count := len(page.Items)
	detail := fmt.Sprintf("First page contained %d item(s).", count)
	if page.NextCursor != "" {
		detail = fmt.Sprintf("First page contained %d item(s); more pages are available.", count)
	}
	return &gen.IdentityProviderCapabilityRead{Capability: capability, Resource: resource, OK: true, Count: &count, Detail: &detail}, ""
}

func tokenFailureResult(checkedAt time.Time, err error) *gen.IdentityProviderVerifyResult {
	outcome := "unreachable"
	detail := "Unable to reach the Okta token endpoint."
	if apiErr, ok := errors.AsType[*okta.APIError](err); ok {
		if apiErr.StatusCode == http.StatusBadRequest || apiErr.StatusCode == http.StatusUnauthorized {
			outcome = "refused"
			switch {
			case apiErr.Code == "invalid_client":
				detail = oktaClientAuthenticationInstruction
			case apiErr.Description != "":
				detail = apiErr.Description
			default:
				detail = "Okta refused the client assertion."
			}
		} else {
			detail = fmt.Sprintf("Okta token endpoint returned status %d.", apiErr.StatusCode)
		}
		if apiErr.Code != "invalid_client" {
			detail += fmt.Sprintf(" Okta said: %s: %s", apiErr.Code, apiErr.Description)
		}
	}
	return &gen.IdentityProviderVerifyResult{
		Outcome:       outcome,
		Detail:        detail,
		Capabilities:  []string{},
		GrantedScopes: []string{},
		Evidence: &gen.IdentityProviderVerifyEvidence{
			CheckedAt: checkedAt.Format(time.RFC3339Nano),
			Reads:     []*gen.IdentityProviderCapabilityRead{},
		},
	}
}

func (s *Service) decryptSigningKey(encrypted string) (*rsa.PrivateKey, error) {
	plaintext, err := s.encryption.Decrypt(encrypted)
	if err != nil {
		return nil, fmt.Errorf("decrypt signing key: %w", err)
	}
	block, rest := pem.Decode([]byte(plaintext))
	if block == nil || len(strings.TrimSpace(string(rest))) > 0 {
		return nil, errors.New("decode signing key: invalid PEM")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse signing key: %w", err)
	}
	privateKey, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("parse signing key: key is not RSA")
	}
	return privateKey, nil
}

func marshalStoredVerification(result *gen.IdentityProviderVerifyResult) ([]byte, error) {
	reads := make([]storedVerificationRead, len(result.Evidence.Reads))
	for i, read := range result.Evidence.Reads {
		reads[i] = storedVerificationRead{
			Capability: read.Capability,
			Resource:   read.Resource,
			OK:         read.OK,
			Count:      read.Count,
			Detail:     read.Detail,
		}
	}
	encoded, err := json.Marshal(storedVerification{
		Outcome:       result.Outcome,
		Detail:        result.Detail,
		Capabilities:  result.Capabilities,
		GrantedScopes: result.GrantedScopes,
		CheckedAt:     result.Evidence.CheckedAt,
		Reads:         reads,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal verification evidence: %w", err)
	}
	return encoded, nil
}

func compactStrings(values []string) []string {
	result := values[:0]
	for _, value := range values {
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}
