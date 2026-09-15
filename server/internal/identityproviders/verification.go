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
	capabilitySignInProvisioning        = "sign_in_provisioning"
)

var requiredOktaScopes = []string{
	"okta.apps.read",
	"okta.groups.read",
	"okta.users.read",
	"okta.apps.manage",
}

// OktaClient is the provider boundary used by identity provider verification.
type OktaClient interface {
	AcquireToken(context.Context, okta.TokenRequest) (okta.Token, error)
	ListGroups(context.Context, string, string, okta.PageRequest) (okta.Page, error)
	ListUsers(context.Context, string, string, okta.PageRequest) (okta.Page, error)
	ListApplications(context.Context, string, string, okta.PageRequest) (okta.Page, error)
	CreateOIDCApplication(context.Context, string, string, okta.CreateOIDCApplicationInput) (okta.Application, error)
	GetApplication(context.Context, string, string, string) (okta.Application, error)
	ResolveApplicationByClientID(context.Context, string, string, string) (okta.Application, error)
	FindEveryoneGroup(context.Context, string, string) (okta.Group, error)
	AssignGroupToApplication(context.Context, string, string, string, string) error
}

// WorkOSClient is the provider boundary used by sign-in verification.
type WorkOSClient interface {
	ListConnections(context.Context, string) ([]workos.Connection, error)
	CreateOIDCConnection(context.Context, workos.CreateOIDCConnectionInput) (workos.Connection, error)
	GetConnection(context.Context, string) (workos.Connection, error)
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
	token, tokenErr := s.okta.AcquireToken(ctx, okta.TokenRequest{
		ConnectionID: before.ID,
		TenantDomain: tenantDomain,
		ClientID:     before.ClientID.String,
		KeyID:        signingKey.Kid,
		PrivateKey:   privateKey,
		Scopes:       append([]string(nil), requiredOktaScopes...),
	})
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

	capabilities := make([]string, 0, 3)
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
			Reads:     []*gen.IdentityProviderCapabilityRead{groups, users, apps},
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
