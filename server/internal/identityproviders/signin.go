package identityproviders

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/identity_providers"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/identityproviders/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/okta"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	setupValueGroupsClaimConfirmed = "groups_claim_confirmed"
	setupValueGroupsSource         = "groups_source"
	setupValueClientSecret         = "client_secret"
)

type storedSignInEvidence struct {
	ClientID               string                   `json:"client_id,omitempty"`
	RedirectURI            string                   `json:"redirect_uri,omitempty"`
	GroupsClaimProvisioned bool                     `json:"groups_claim_provisioned,omitempty"`
	Outcome                string                   `json:"outcome,omitempty"`
	Detail                 string                   `json:"detail,omitempty"`
	CheckedAt              string                   `json:"checked_at,omitempty"`
	Reads                  []storedVerificationRead `json:"reads,omitempty"`
}

func (s *Service) submitSignInSetupStep(
	ctx context.Context,
	authCtx *contextvalues.AuthContext,
	logger *slog.Logger,
	payload *gen.SubmitSetupStepPayload,
) (*gen.SubmitSetupStepResult, error) {
	before, err := repo.New(s.db).GetIdentityProviderConnectionByOrganization(ctx, authCtx.ActiveOrganizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, nil, "identity provider connection not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error loading identity provider connection").LogError(ctx, logger)
	}
	if before.Status != "active" {
		return nil, oops.E(oops.CodeBadRequest, nil, "verify the Okta connection before configuring sign-in").LogError(ctx, logger)
	}

	if !before.SignInApplicationID.Valid {
		return s.submitSignInApplication(ctx, authCtx, logger, payload, before)
	}
	if len(payload.Values) == 0 {
		if _, err := s.getActiveStoredSignInApplication(ctx, authCtx.ActiveOrganizationID, before); err != nil {
			return nil, oops.E(oops.CodeGatewayError, err, "error reading the existing Okta sign-in application").LogError(ctx, logger)
		}
		if slices.Contains(before.Capabilities, capabilitySignInProvisioning) {
			if err := s.assignSignInApplicationToEveryone(ctx, authCtx.ActiveOrganizationID, before); err != nil {
				return nil, oops.E(oops.CodeGatewayError, err, "error assigning the Okta sign-in application to Everyone").LogError(ctx, logger)
			}
		}
		if slices.Contains(before.Capabilities, capabilityClaimsProvisioning) {
			before, err = s.provisionSignInClaims(ctx, authCtx, logger, before)
			if err != nil {
				return nil, oops.E(oops.CodeGatewayError, err, "error provisioning the Okta sign-in claims").LogError(ctx, logger)
			}
		}
		step, err := buildSignInSetupStep(before)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error building identity provider setup").LogError(ctx, logger)
		}
		return &gen.SubmitSetupStepResult{Step: step, FieldOutcomes: []*gen.IdentityProviderFieldOutcome{}, NextStepKey: nil}, nil
	}
	if len(payload.Values) == 2 && (!before.WorkosConnectionID.Valid || strings.TrimSpace(before.WorkosConnectionID.String) == "") {
		return s.submitSignInApplication(ctx, authCtx, logger, payload, before)
	}
	evidence, err := decodeStoredSignInEvidence(before.SignInEvidence)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error loading sign-in configuration").LogError(ctx, logger)
	}
	if len(payload.Values) == 2 {
		var clientID, clientSecret string
		for _, value := range payload.Values {
			if value == nil {
				continue
			}
			switch value.Key {
			case setupValueClientID:
				clientID = strings.TrimSpace(value.Value)
			case setupValueClientSecret:
				clientSecret = value.Value
			}
		}
		if clientID == evidence.ClientID && strings.TrimSpace(clientSecret) != "" {
			if _, err := s.getActiveStoredSignInApplication(ctx, authCtx.ActiveOrganizationID, before); err != nil {
				return nil, oops.E(oops.CodeGatewayError, err, "error reading the existing Okta sign-in application").LogError(ctx, logger)
			}
			step, buildErr := buildSignInSetupStep(before)
			if buildErr != nil {
				return nil, oops.E(oops.CodeUnexpected, buildErr, "error building identity provider setup").LogError(ctx, logger)
			}
			return &gen.SubmitSetupStepResult{
				Step: step,
				FieldOutcomes: []*gen.IdentityProviderFieldOutcome{
					{Key: setupValueClientID, Outcome: "accepted", Detail: "Okta sign-in Client ID saved."},
					{Key: setupValueClientSecret, Outcome: "accepted", Detail: "Client secret sent to WorkOS and not retained."},
				},
				NextStepKey: nil,
			}, nil
		}
	}
	if len(payload.Values) != 1 {
		return nil, oops.E(oops.CodeBadRequest, nil, "sign_in requires exactly one acknowledgement").LogError(ctx, logger)
	}

	groupsSource := conv.FromPGText[string](before.GroupsSource)
	groupsClaimConfirmed := before.GroupsClaimConfirmed
	fieldOutcomes := make([]*gen.IdentityProviderFieldOutcome, 0, 1)
	for _, value := range payload.Values {
		if value == nil {
			return nil, oops.E(oops.CodeBadRequest, nil, "sign_in values must not contain null entries").LogError(ctx, logger)
		}
		switch value.Key {
		case setupValueGroupsClaimConfirmed:
			if strings.TrimSpace(value.Value) != "true" {
				return nil, oops.E(oops.CodeBadRequest, nil, "groups_claim_confirmed must be true").LogError(ctx, logger)
			}
			groupsClaimConfirmed = true
			groupsSource = new("token")
			fieldOutcomes = append(fieldOutcomes, &gen.IdentityProviderFieldOutcome{Key: value.Key, Outcome: "accepted", Detail: "Okta groups claim confirmed."})
		case setupValueGroupsSource:
			if strings.TrimSpace(value.Value) != "directory" {
				return nil, oops.E(oops.CodeBadRequest, nil, "groups_source must be directory").LogError(ctx, logger)
			}
			groupsSource = new("directory")
			groupsClaimConfirmed = false
			fieldOutcomes = append(fieldOutcomes, &gen.IdentityProviderFieldOutcome{Key: value.Key, Outcome: "accepted", Detail: "Directory groups selected."})
		default:
			return nil, oops.E(oops.CodeBadRequest, nil, "unknown sign_in setup value").LogError(ctx, logger)
		}
	}

	after, err := s.persistSignInUpdate(ctx, authCtx, logger, before, func(queries *repo.Queries) error {
		return queries.UpdateOktaIdentityProviderSignInAcknowledgement(ctx, repo.UpdateOktaIdentityProviderSignInAcknowledgementParams{
			GroupsSource:                 conv.PtrToPGTextEmpty(groupsSource),
			GroupsClaimConfirmed:         groupsClaimConfirmed,
			SignInEvidence:               before.SignInEvidence,
			OrganizationID:               authCtx.ActiveOrganizationID,
			IdentityProviderConnectionID: before.ID,
		})
	})
	if err != nil {
		return nil, err
	}
	step, err := buildSignInSetupStep(after)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error building identity provider setup").LogError(ctx, logger)
	}
	return &gen.SubmitSetupStepResult{Step: step, FieldOutcomes: fieldOutcomes, NextStepKey: nil}, nil
}

func (s *Service) submitSignInApplication(
	ctx context.Context,
	authCtx *contextvalues.AuthContext,
	logger *slog.Logger,
	payload *gen.SubmitSetupStepPayload,
	before repo.GetIdentityProviderConnectionByOrganizationRow,
) (*gen.SubmitSetupStepResult, error) {
	canProvision := slices.Contains(before.Capabilities, capabilitySignInProvisioning)
	canProvisionClaims := slices.Contains(before.Capabilities, capabilityClaimsProvisioning)
	var submittedClientID, clientSecret string
	if canProvision {
		if len(payload.Values) != 0 {
			return nil, oops.E(oops.CodeBadRequest, nil, "automated sign_in setup does not accept values").LogError(ctx, logger)
		}
	} else {
		if len(payload.Values) != 2 {
			return nil, oops.E(oops.CodeBadRequest, nil, "manual sign_in setup requires client_id and client_secret values").LogError(ctx, logger)
		}
		for _, value := range payload.Values {
			if value == nil {
				return nil, oops.E(oops.CodeBadRequest, nil, "manual sign_in values must not contain null entries").LogError(ctx, logger)
			}
			switch value.Key {
			case setupValueClientID:
				if submittedClientID != "" {
					return nil, oops.E(oops.CodeBadRequest, nil, "manual sign_in client_id must be provided once").LogError(ctx, logger)
				}
				submittedClientID = strings.TrimSpace(value.Value)
			case setupValueClientSecret:
				if clientSecret != "" {
					return nil, oops.E(oops.CodeBadRequest, nil, "manual sign_in client_secret must be provided once").LogError(ctx, logger)
				}
				clientSecret = value.Value
			default:
				return nil, oops.E(oops.CodeBadRequest, nil, "manual sign_in setup accepts only client_id and client_secret").LogError(ctx, logger)
			}
		}
		if submittedClientID == "" {
			return nil, oops.E(oops.CodeBadRequest, nil, "client_id must not be empty").LogError(ctx, logger)
		}
		if strings.TrimSpace(clientSecret) == "" {
			return nil, oops.E(oops.CodeBadRequest, nil, "client_secret must not be empty").LogError(ctx, logger)
		}
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error locking sign-in configuration").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)
	if _, err := queries.LockOktaIdentityProviderSignIn(ctx, repo.LockOktaIdentityProviderSignInParams{
		OrganizationID:               authCtx.ActiveOrganizationID,
		IdentityProviderConnectionID: before.ID,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error locking sign-in configuration").LogError(ctx, logger)
	}
	before, err = queries.GetIdentityProviderConnectionByOrganization(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error loading locked sign-in configuration").LogError(ctx, logger)
	}
	if canProvision != slices.Contains(before.Capabilities, capabilitySignInProvisioning) {
		return nil, oops.E(oops.CodeConflict, nil, "Okta capabilities changed during sign-in setup").LogError(ctx, logger)
	}
	if canProvisionClaims != slices.Contains(before.Capabilities, capabilityClaimsProvisioning) {
		return nil, oops.E(oops.CodeConflict, nil, "Okta capabilities changed during sign-in setup").LogError(ctx, logger)
	}
	if !before.WorkosID.Valid || strings.TrimSpace(before.WorkosID.String) == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "organization is not linked to WorkOS").LogError(ctx, logger)
	}

	tokenScopes := requiredOktaReadScopes
	if canProvision {
		tokenScopes = oktaApplicationProvisioningScopes
	}
	token, err := s.acquireOktaManagementToken(ctx, authCtx.ActiveOrganizationID, before, tokenScopes)
	if err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "error authorizing Okta sign-in setup").LogError(ctx, logger)
	}
	tenantDomain := normalizeOktaDomain(before.TenantIdentifier)
	var application okta.Application
	if before.SignInApplicationID.Valid {
		application, err = s.okta.GetApplication(ctx, tenantDomain, token.AccessToken, before.SignInApplicationID.String)
		if err := dbtx.Commit(ctx); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error releasing sign-in configuration lock").LogError(ctx, logger)
		}
		if err != nil {
			return nil, oops.E(oops.CodeGatewayError, err, "error reading the existing Okta sign-in application").LogError(ctx, logger)
		}
		if application.ID != before.SignInApplicationID.String || application.ClientID == "" || application.Status != "ACTIVE" {
			return nil, oops.E(oops.CodeGatewayError, nil, "the existing Okta sign-in application is not active").LogError(ctx, logger)
		}
		if canProvision {
			if err := s.assignSignInApplicationToEveryone(ctx, authCtx.ActiveOrganizationID, before); err != nil {
				return nil, oops.E(oops.CodeGatewayError, err, "error assigning the Okta sign-in application to Everyone").LogError(ctx, logger)
			}
		}
		if canProvisionClaims {
			before, err = s.provisionSignInClaims(ctx, authCtx, logger, before)
			if err != nil {
				return nil, oops.E(oops.CodeGatewayError, err, "error provisioning the Okta sign-in claims").LogError(ctx, logger)
			}
		}
		if canProvision {
			step, buildErr := buildSignInSetupStep(before)
			if buildErr != nil {
				return nil, oops.E(oops.CodeUnexpected, buildErr, "error building identity provider setup").LogError(ctx, logger)
			}
			return &gen.SubmitSetupStepResult{Step: step, FieldOutcomes: []*gen.IdentityProviderFieldOutcome{}, NextStepKey: nil}, nil
		}
		if application.ClientID != submittedClientID {
			return nil, oops.E(oops.CodeBadRequest, nil, "client_id does not match the stored Okta sign-in application").LogError(ctx, logger)
		}
		return s.createWorkOSSignInConnection(ctx, authCtx, logger, before, application, clientSecret, false)
	}
	if canProvision {
		var found bool
		application, clientSecret, found, err = s.okta.FindActiveApplicationByLabel(ctx, tenantDomain, token.AccessToken, okta.SignInApplicationLabel)
		if err == nil && !found {
			secretBytes := make([]byte, 32)
			if _, err := rand.Read(secretBytes); err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "error creating Okta sign-in client secret").LogError(ctx, logger)
			}
			clientSecret = base64.RawURLEncoding.EncodeToString(secretBytes)
			application, err = s.okta.CreateOIDCApplication(ctx, tenantDomain, token.AccessToken, okta.CreateOIDCApplicationInput{
				RedirectURIs: []string{},
				ClientSecret: clientSecret,
			})
		}
	} else {
		application, err = s.okta.ResolveApplicationByClientID(ctx, tenantDomain, token.AccessToken, submittedClientID)
	}
	if err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "error finding the Okta sign-in application").LogError(ctx, logger)
	}
	if application.ID == "" || application.ClientID == "" {
		return nil, oops.E(oops.CodeGatewayError, nil, "Okta returned an incomplete sign-in application").LogError(ctx, logger)
	}
	evidence, err := json.Marshal(storedSignInEvidence{
		ClientID:               application.ClientID,
		RedirectURI:            "",
		GroupsClaimProvisioned: false,
		Outcome:                "",
		Detail:                 "",
		CheckedAt:              "",
		Reads:                  nil,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error encoding Okta sign-in application state").LogError(ctx, logger)
	}
	if err := queries.UpdateOktaIdentityProviderSignInApplication(ctx, repo.UpdateOktaIdentityProviderSignInApplicationParams{
		SignInApplicationID:          conv.ToPGText(application.ID),
		WorkosConnectionID:           conv.ToPGTextEmpty(""),
		SignInEvidence:               evidence,
		OrganizationID:               authCtx.ActiveOrganizationID,
		IdentityProviderConnectionID: before.ID,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving Okta sign-in application").LogError(ctx, logger)
	}
	after, err := queries.GetIdentityProviderConnectionByOrganization(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error loading saved Okta sign-in application").LogError(ctx, logger)
	}
	if err := s.audit.LogIdentityProviderConnectionUpdated(ctx, dbtx, audit.LogIdentityProviderConnectionUpdatedEvent{
		OrganizationID:                           authCtx.ActiveOrganizationID,
		Actor:                                    urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:                         authCtx.Email,
		ActorSlug:                                nil,
		IdentityProviderConnectionURN:            urn.NewIdentityProviderConnectionID(after.ID),
		TenantIdentifier:                         after.TenantIdentifier,
		IdentityProviderConnectionSnapshotBefore: identityProviderConnectionSnapshot(before, ""),
		IdentityProviderConnectionSnapshotAfter:  identityProviderConnectionSnapshot(after, ""),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error recording Okta sign-in application").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving Okta sign-in application").LogError(ctx, logger)
	}
	if canProvision {
		if err := s.assignSignInApplicationToEveryone(ctx, authCtx.ActiveOrganizationID, after); err != nil {
			return nil, oops.E(oops.CodeGatewayError, err, "error assigning the Okta sign-in application to Everyone").LogError(ctx, logger)
		}
	}
	if canProvisionClaims {
		after, err = s.provisionSignInClaims(ctx, authCtx, logger, after)
		if err != nil {
			return nil, oops.E(oops.CodeGatewayError, err, "error provisioning the Okta sign-in claims").LogError(ctx, logger)
		}
	}
	if canProvision && clientSecret == "" {
		step, buildErr := buildSignInSetupStep(after)
		if buildErr != nil {
			return nil, oops.E(oops.CodeUnexpected, buildErr, "error building identity provider setup").LogError(ctx, logger)
		}
		return &gen.SubmitSetupStepResult{Step: step, FieldOutcomes: []*gen.IdentityProviderFieldOutcome{}, NextStepKey: nil}, nil
	}
	return s.createWorkOSSignInConnection(ctx, authCtx, logger, after, application, clientSecret, canProvision)
}

func (s *Service) createWorkOSSignInConnection(
	ctx context.Context,
	authCtx *contextvalues.AuthContext,
	logger *slog.Logger,
	before repo.GetIdentityProviderConnectionByOrganizationRow,
	application okta.Application,
	clientSecret string,
	canProvision bool,
) (*gen.SubmitSetupStepResult, error) {
	discoveryEndpoint := "https://" + normalizeOktaDomain(before.TenantIdentifier) + "/.well-known/openid-configuration"
	if slices.Contains(before.Capabilities, capabilityClaimsProvisioning) {
		discoveryEndpoint = "https://" + normalizeOktaDomain(before.TenantIdentifier) + "/oauth2/default/.well-known/openid-configuration"
	}
	connection, connectionErr := s.workos.CreateOIDCConnection(ctx, workos.CreateOIDCConnectionInput{
		OrganizationID:    before.WorkosID.String,
		Name:              "Okta",
		DiscoveryEndpoint: discoveryEndpoint,
		ClientID:          application.ClientID,
		ClientSecret:      clientSecret,
	})
	if connectionErr != nil {
		step, err := buildSignInSetupStep(before)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error building identity provider setup").LogError(ctx, logger)
		}
		if workos.IsConnectionsWriteUnavailable(connectionErr) {
			return &gen.SubmitSetupStepResult{Step: step, FieldOutcomes: []*gen.IdentityProviderFieldOutcome{}, NextStepKey: nil}, nil
		}
		if fieldOutcome := workOSConnectionFieldOutcome(connectionErr, canProvision); fieldOutcome != nil {
			return &gen.SubmitSetupStepResult{Step: step, FieldOutcomes: []*gen.IdentityProviderFieldOutcome{fieldOutcome}, NextStepKey: nil}, nil
		}
		return nil, oops.E(oops.CodeGatewayError, connectionErr, "error creating the WorkOS sign-in connection").LogError(ctx, logger)
	}
	if connection.ID == "" || connection.OrganizationID != before.WorkosID.String || connection.ConnectionType != "GenericOIDC" {
		return nil, oops.E(oops.CodeGatewayError, nil, "WorkOS returned an incomplete sign-in connection").LogError(ctx, logger)
	}
	connection, err := s.workos.GetConnection(ctx, connection.ID)
	if err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "error reading the created WorkOS sign-in connection").LogError(ctx, logger)
	}
	if connection.ID == "" || connection.OrganizationID != before.WorkosID.String || connection.ConnectionType != "GenericOIDC" {
		return nil, oops.E(oops.CodeGatewayError, nil, "WorkOS returned an incomplete sign-in connection").LogError(ctx, logger)
	}
	redirectURI := strings.TrimSpace(connection.OIDCRedirectURI)
	if redirectURI == "" {
		redirectURI = strings.TrimSpace(connection.CallbackEndpoint)
	}
	if redirectURI == "" {
		return nil, oops.E(oops.CodeGatewayError, nil, "WorkOS did not report the OIDC redirect URI").LogError(ctx, logger)
	}
	if canProvision {
		token, err := s.acquireOktaManagementToken(ctx, authCtx.ActiveOrganizationID, before, oktaApplicationProvisioningScopes)
		if err != nil {
			return nil, oops.E(oops.CodeGatewayError, err, "error authorizing the Okta sign-in redirect URI update").LogError(ctx, logger)
		}
		if err := s.okta.EnsureOIDCApplicationRedirectURI(ctx, normalizeOktaDomain(before.TenantIdentifier), token.AccessToken, application.ID, redirectURI); err != nil {
			return nil, oops.E(oops.CodeGatewayError, err, "error updating the Okta sign-in redirect URI").LogError(ctx, logger)
		}
	}
	evidence, err := decodeStoredSignInEvidence(before.SignInEvidence)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error loading sign-in configuration").LogError(ctx, logger)
	}
	evidence.RedirectURI = redirectURI
	encodedEvidence, err := json.Marshal(evidence)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error encoding sign-in configuration").LogError(ctx, logger)
	}

	after, err := s.persistSignInUpdate(ctx, authCtx, logger, before, func(queries *repo.Queries) error {
		updated, err := queries.UpdateOktaIdentityProviderWorkOSConnection(ctx, repo.UpdateOktaIdentityProviderWorkOSConnectionParams{
			WorkosConnectionID:           conv.ToPGText(connection.ID),
			OrganizationID:               authCtx.ActiveOrganizationID,
			IdentityProviderConnectionID: before.ID,
			SignInApplicationID:          conv.ToPGText(application.ID),
		})
		if err != nil {
			return fmt.Errorf("update WorkOS sign-in connection: %w", err)
		}
		if updated != 1 {
			return errors.New("okta sign-in application changed while creating the WorkOS connection")
		}
		return queries.UpdateOktaIdentityProviderSignInAcknowledgement(ctx, repo.UpdateOktaIdentityProviderSignInAcknowledgementParams{
			GroupsSource:                 before.GroupsSource,
			GroupsClaimConfirmed:         before.GroupsClaimConfirmed,
			SignInEvidence:               encodedEvidence,
			OrganizationID:               authCtx.ActiveOrganizationID,
			IdentityProviderConnectionID: before.ID,
		})
	})
	if err != nil {
		return nil, err
	}
	step, err := buildSignInSetupStep(after)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error building identity provider setup").LogError(ctx, logger)
	}
	fieldOutcomes := []*gen.IdentityProviderFieldOutcome{}
	if !canProvision {
		fieldOutcomes = append(fieldOutcomes,
			&gen.IdentityProviderFieldOutcome{Key: setupValueClientID, Outcome: "accepted", Detail: "Okta sign-in Client ID saved."},
			&gen.IdentityProviderFieldOutcome{Key: setupValueClientSecret, Outcome: "accepted", Detail: "Client secret sent to WorkOS and not retained."},
		)
	}
	return &gen.SubmitSetupStepResult{Step: step, FieldOutcomes: fieldOutcomes, NextStepKey: nil}, nil
}

func workOSConnectionFieldOutcome(err error, canProvision bool) *gen.IdentityProviderFieldOutcome {
	apiErr, ok := errors.AsType[*workos.APIError](err)
	if !ok || apiErr.StatusCode != http.StatusBadRequest {
		return nil
	}
	var response struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if json.Unmarshal([]byte(apiErr.Body), &response) != nil || strings.TrimSpace(response.Code) == "" {
		return nil
	}
	detail := response.Code
	if strings.TrimSpace(response.Message) != "" {
		detail += ": " + response.Message
	}
	key := "workos_connection"
	if !canProvision {
		key = setupValueClientSecret
	}
	return &gen.IdentityProviderFieldOutcome{Key: key, Outcome: "rejected", Detail: detail}
}

func (s *Service) getActiveStoredSignInApplication(
	ctx context.Context,
	organizationID string,
	row repo.GetIdentityProviderConnectionByOrganizationRow,
) (okta.Application, error) {
	token, err := s.acquireOktaManagementToken(ctx, organizationID, row, requiredOktaReadScopes)
	if err != nil {
		return okta.Application{ID: "", Status: "", Label: "", ClientID: "", SignOnMode: "", SignOnURL: "", LogoURL: ""}, err
	}
	application, err := s.okta.GetApplication(ctx, normalizeOktaDomain(row.TenantIdentifier), token.AccessToken, row.SignInApplicationID.String)
	if err != nil {
		return okta.Application{ID: "", Status: "", Label: "", ClientID: "", SignOnMode: "", SignOnURL: "", LogoURL: ""}, fmt.Errorf("get Okta sign-in application: %w", err)
	}
	if application.ID != row.SignInApplicationID.String || application.ClientID == "" || application.Status != "ACTIVE" {
		return okta.Application{ID: "", Status: "", Label: "", ClientID: "", SignOnMode: "", SignOnURL: "", LogoURL: ""}, errors.New("the existing Okta sign-in application is not active")
	}
	return application, nil
}

func (s *Service) assignSignInApplicationToEveryone(ctx context.Context, organizationID string, row repo.GetIdentityProviderConnectionByOrganizationRow) error {
	token, err := s.acquireOktaManagementToken(ctx, organizationID, row, oktaApplicationProvisioningScopes)
	if err != nil {
		return err
	}
	tenantDomain := normalizeOktaDomain(row.TenantIdentifier)
	group, err := s.okta.FindEveryoneGroup(ctx, tenantDomain, token.AccessToken)
	if err != nil {
		return fmt.Errorf("find Okta Everyone group: %w", err)
	}
	if err := s.okta.AssignGroupToApplication(ctx, tenantDomain, token.AccessToken, row.SignInApplicationID.String, group.ID); err != nil {
		return fmt.Errorf("assign Okta sign-in application to Everyone: %w", err)
	}
	return nil
}

func (s *Service) provisionSignInClaims(
	ctx context.Context,
	authCtx *contextvalues.AuthContext,
	logger *slog.Logger,
	before repo.GetIdentityProviderConnectionByOrganizationRow,
) (repo.GetIdentityProviderConnectionByOrganizationRow, error) {
	evidence, err := decodeStoredSignInEvidence(before.SignInEvidence)
	if err != nil {
		return repo.GetIdentityProviderConnectionByOrganizationRow{}, err
	}
	if evidence.GroupsClaimProvisioned {
		return before, nil
	}
	token, err := s.acquireOktaManagementToken(ctx, authCtx.ActiveOrganizationID, before, oktaClaimsProvisioningScopes)
	if err != nil {
		return repo.GetIdentityProviderConnectionByOrganizationRow{}, err
	}
	tenantDomain := normalizeOktaDomain(before.TenantIdentifier)
	servers, err := s.okta.ListAuthorizationServers(ctx, tenantDomain, token.AccessToken)
	if err != nil {
		return repo.GetIdentityProviderConnectionByOrganizationRow{}, fmt.Errorf("list Okta authorization servers: %w", err)
	}
	authorizationServerID := ""
	for _, server := range servers {
		if server.Name == "default" && server.ID != "" {
			authorizationServerID = server.ID
			break
		}
	}
	if authorizationServerID == "" {
		return repo.GetIdentityProviderConnectionByOrganizationRow{}, errors.New("find Okta default authorization server: server not found")
	}
	if _, err := s.okta.EnsureSignInClaims(ctx, tenantDomain, token.AccessToken, authorizationServerID); err != nil {
		return repo.GetIdentityProviderConnectionByOrganizationRow{}, fmt.Errorf("ensure Okta sign-in claims: %w", err)
	}
	evidence.GroupsClaimProvisioned = true
	encodedEvidence, err := json.Marshal(evidence)
	if err != nil {
		return repo.GetIdentityProviderConnectionByOrganizationRow{}, fmt.Errorf("encode provisioned Okta sign-in claims: %w", err)
	}
	return s.persistSignInUpdate(ctx, authCtx, logger, before, func(queries *repo.Queries) error {
		return queries.UpdateOktaIdentityProviderSignInAcknowledgement(ctx, repo.UpdateOktaIdentityProviderSignInAcknowledgementParams{
			GroupsSource:                 conv.ToPGText("token"),
			GroupsClaimConfirmed:         true,
			SignInEvidence:               encodedEvidence,
			OrganizationID:               authCtx.ActiveOrganizationID,
			IdentityProviderConnectionID: before.ID,
		})
	})
}

func (s *Service) verifySignInSetupStep(
	ctx context.Context,
	authCtx *contextvalues.AuthContext,
	logger *slog.Logger,
) (*gen.IdentityProviderVerifyResult, error) {
	before, err := repo.New(s.db).GetIdentityProviderConnectionByOrganization(ctx, authCtx.ActiveOrganizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, nil, "identity provider connection not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error loading identity provider connection").LogError(ctx, logger)
	}
	if !before.SignInApplicationID.Valid {
		return nil, oops.E(oops.CodeBadRequest, nil, "create the Okta sign-in application before verification").LogError(ctx, logger)
	}

	prior, err := decodeStoredSignInEvidence(before.SignInEvidence)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error loading prior sign-in verification").LogError(ctx, logger)
	}
	checkedAt := time.Now().UTC()
	reads := make([]*gen.IdentityProviderCapabilityRead, 0, 2)

	var application okta.Application
	token, tokenErr := s.acquireOktaManagementToken(ctx, authCtx.ActiveOrganizationID, before, requiredOktaReadScopes)
	var applicationErr error
	if tokenErr != nil {
		applicationErr = tokenErr
	} else {
		application, applicationErr = s.okta.GetApplication(ctx, normalizeOktaDomain(before.TenantIdentifier), token.AccessToken, before.SignInApplicationID.String)
	}
	applicationOK := applicationErr == nil && application.Status == "ACTIVE" && (prior.ClientID == "" || prior.ClientID == application.ClientID)
	applicationDetail := "Okta sign-in application is active."
	if applicationErr != nil {
		applicationDetail = "Unable to read the Okta sign-in application."
	} else if application.Status != "ACTIVE" {
		applicationDetail = "Okta sign-in application is not active."
	} else if prior.ClientID != "" && prior.ClientID != application.ClientID {
		applicationDetail = "Okta sign-in application Client ID does not match the configured application."
	}
	reads = append(reads, &gen.IdentityProviderCapabilityRead{
		Capability: "sign_in",
		Resource:   "sign_in_application",
		OK:         applicationOK,
		Count:      nil,
		Detail:     &applicationDetail,
	})

	portalFallback := !before.WorkosConnectionID.Valid || strings.TrimSpace(before.WorkosConnectionID.String) == ""
	connectionID := ""
	var connection workos.Connection
	var connectionErr error
	if !portalFallback {
		connectionID = before.WorkosConnectionID.String
		connection, connectionErr = s.workos.GetConnection(ctx, connectionID)
	} else if before.WorkosID.Valid && strings.TrimSpace(before.WorkosID.String) != "" {
		var connections []workos.Connection
		connections, connectionErr = s.workos.ListConnections(ctx, before.WorkosID.String)
		if connectionErr == nil {
			for _, candidate := range connections {
				if candidate.ConnectionType != "GenericOIDC" {
					continue
				}
				connectionID = candidate.ID
				if candidate.State == "active" {
					break
				}
			}
			if connectionID != "" {
				connection, connectionErr = s.workos.GetConnection(ctx, connectionID)
			}
		}
	} else {
		connectionErr = errors.New("organization is not linked to WorkOS")
	}
	connectionOK := connectionErr == nil && connection.ID == connectionID && connection.State == "active" && connection.ConnectionType == "GenericOIDC" && before.WorkosID.Valid && connection.OrganizationID == before.WorkosID.String
	connectionDetail := "WorkOS OIDC connection is active."
	if connectionErr != nil {
		connectionDetail = "Unable to read the WorkOS sign-in connection."
	} else if connectionID == "" {
		connectionDetail = "WorkOS does not have an active OIDC connection for this organization."
	} else if connection.State == "validating" {
		connectionDetail = "WorkOS has the sign-in configuration and is waiting for the first successful sign-in."
	} else if connection.State != "active" {
		connectionDetail = "WorkOS OIDC connection state is " + connection.State + "."
	} else if connection.ConnectionType != "GenericOIDC" || !before.WorkosID.Valid || connection.OrganizationID != before.WorkosID.String {
		connectionDetail = "WorkOS sign-in connection does not match the configured organization and type."
	}
	reads = append(reads, &gen.IdentityProviderCapabilityRead{
		Capability: "sign_in",
		Resource:   "sign_in_connection",
		OK:         connectionOK,
		Count:      nil,
		Detail:     &connectionDetail,
	})

	outcome, detail := signInVerificationOutcome(application, applicationErr, applicationOK, connection, connectionErr, connectionOK, portalFallback)
	result := &gen.IdentityProviderVerifyResult{
		Outcome:       outcome,
		Detail:        detail,
		Capabilities:  append([]string(nil), before.Capabilities...),
		GrantedScopes: append([]string(nil), before.GrantedScopes...),
		Evidence: &gen.IdentityProviderVerifyEvidence{
			CheckedAt: checkedAt.Format(time.RFC3339Nano),
			Reads:     reads,
		},
	}
	storedReads := make([]storedVerificationRead, len(reads))
	for i, read := range reads {
		storedReads[i] = storedVerificationRead{Capability: read.Capability, Resource: read.Resource, OK: read.OK, Count: read.Count, Detail: read.Detail}
	}
	evidence, err := json.Marshal(storedSignInEvidence{
		ClientID:               prior.ClientID,
		RedirectURI:            prior.RedirectURI,
		GroupsClaimProvisioned: prior.GroupsClaimProvisioned,
		Outcome:                result.Outcome,
		Detail:                 result.Detail,
		CheckedAt:              result.Evidence.CheckedAt,
		Reads:                  storedReads,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error encoding sign-in verification").LogError(ctx, logger)
	}
	signInState := "failed"
	switch outcome {
	case "passed":
		signInState = "passed"
	case "pending_validation":
		signInState = "application_created"
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving sign-in verification").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	updated, err := repo.New(dbtx).UpdateOktaIdentityProviderSignInVerification(ctx, repo.UpdateOktaIdentityProviderSignInVerificationParams{
		WorkosConnectionID:           conv.ToPGTextEmpty(connectionID),
		SignInState:                  conv.ToPGText(signInState),
		SignInEvidence:               evidence,
		OrganizationID:               authCtx.ActiveOrganizationID,
		IdentityProviderConnectionID: before.ID,
		SignInApplicationID:          before.SignInApplicationID,
		PreviousSignInEvidence:       before.SignInEvidence,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving sign-in verification").LogError(ctx, logger)
	}
	if updated != 1 {
		return nil, oops.E(oops.CodeConflict, nil, "identity provider sign-in configuration changed during verification").LogError(ctx, logger)
	}
	after, err := repo.New(dbtx).GetIdentityProviderConnectionByOrganization(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error loading verified sign-in configuration").LogError(ctx, logger)
	}
	if err := s.audit.LogIdentityProviderConnectionVerified(ctx, dbtx, audit.LogIdentityProviderConnectionVerifiedEvent{
		OrganizationID:                           authCtx.ActiveOrganizationID,
		Actor:                                    urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:                         authCtx.Email,
		ActorSlug:                                nil,
		IdentityProviderConnectionURN:            urn.NewIdentityProviderConnectionID(after.ID),
		TenantIdentifier:                         after.TenantIdentifier,
		IdentityProviderConnectionSnapshotBefore: identityProviderConnectionSnapshot(before, prior.Outcome),
		IdentityProviderConnectionSnapshotAfter:  identityProviderConnectionSnapshot(after, outcome),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error recording sign-in verification").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving sign-in verification").LogError(ctx, logger)
	}
	return result, nil
}

func signInVerificationOutcome(
	application okta.Application,
	applicationErr error,
	applicationOK bool,
	connection workos.Connection,
	connectionErr error,
	connectionOK bool,
	portalFallback bool,
) (string, string) {
	if applicationErr != nil {
		if apiErr, ok := errors.AsType[*okta.APIError](applicationErr); ok {
			switch apiErr.StatusCode {
			case http.StatusForbidden:
				return "capability_missing", "Okta did not permit reading the sign-in application."
			case http.StatusNotFound:
				return "mismatched_value", "The Okta sign-in application no longer exists."
			case http.StatusRequestTimeout, http.StatusTooManyRequests:
				return "unreachable", "Okta temporarily could not complete the sign-in application read."
			default:
				if apiErr.StatusCode >= http.StatusBadRequest && apiErr.StatusCode < http.StatusInternalServerError {
					return "refused", "Okta refused the sign-in application read."
				}
			}
		}
		return "unreachable", "Unable to reach Okta while reading the sign-in application."
	}
	if connectionErr != nil {
		if apiErr, ok := errors.AsType[*workos.APIError](connectionErr); ok {
			if apiErr.StatusCode == http.StatusNotFound {
				return "mismatched_value", "The WorkOS sign-in connection no longer exists."
			}
			if apiErr.StatusCode == http.StatusRequestTimeout || apiErr.StatusCode == http.StatusTooManyRequests {
				return "unreachable", "WorkOS temporarily could not complete the sign-in connection read."
			}
			if apiErr.StatusCode >= http.StatusBadRequest && apiErr.StatusCode < http.StatusInternalServerError {
				return "refused", "WorkOS refused the sign-in connection read."
			}
		}
		return "unreachable", "Unable to reach WorkOS while reading the sign-in connection."
	}
	if !applicationOK {
		if application.Status != "ACTIVE" {
			return "mismatched_value", "The Okta sign-in application is not active."
		}
		return "mismatched_value", "The Okta sign-in application Client ID does not match."
	}
	if !connectionOK {
		if !portalFallback && connection.State != "" && connection.State != "active" {
			switch connection.State {
			case "validating":
				return "pending_validation", "Speakeasy has configured sign-in. It becomes active after the first successful sign-in through Okta; run a test sign-in from the sign-in provider or sign in to Speakeasy with Okta, then check again."
			case "draft", "inactive", "requires_type":
				return "refused", "WorkOS reported the sign-in connection state as " + connection.State + "."
			}
		}
		if !portalFallback {
			return "mismatched_value", "The WorkOS sign-in connection does not match the configured organization and type."
		}
		return "mismatched_value", "Finish the OIDC connection in WorkOS Admin Portal, then verify again."
	}
	return "passed", "Okta sign-in application and WorkOS OIDC connection verified."
}

func (s *Service) acquireOktaManagementToken(
	ctx context.Context,
	organizationID string,
	row repo.GetIdentityProviderConnectionByOrganizationRow,
	scopes []string,
) (okta.Token, error) {
	if !row.ClientID.Valid || !row.SigningKeyID.Valid {
		return okta.Token{}, errors.New("okta API Services app is not configured")
	}
	signingKey, err := repo.New(s.db).GetConfiguredIdentityProviderSigningKey(ctx, repo.GetConfiguredIdentityProviderSigningKeyParams{
		OrganizationID:               organizationID,
		IdentityProviderConnectionID: row.ID,
		SigningKeyID:                 row.SigningKeyID.UUID,
	})
	if err != nil {
		return okta.Token{}, fmt.Errorf("load identity provider signing key: %w", err)
	}
	privateKey, err := s.decryptSigningKey(signingKey.PrivateKeyEncrypted)
	if err != nil {
		return okta.Token{}, err
	}
	token, err := s.okta.AcquireToken(ctx, okta.TokenRequest{
		ConnectionID: row.ID,
		TenantDomain: normalizeOktaDomain(row.TenantIdentifier),
		ClientID:     row.ClientID.String,
		KeyID:        signingKey.Kid,
		PrivateKey:   privateKey,
		Scopes:       append([]string(nil), scopes...),
	})
	if err != nil {
		return okta.Token{}, fmt.Errorf("acquire Okta management token: %w", err)
	}
	return token, nil
}

func (s *Service) persistSignInUpdate(
	ctx context.Context,
	authCtx *contextvalues.AuthContext,
	logger *slog.Logger,
	before repo.GetIdentityProviderConnectionByOrganizationRow,
	update func(*repo.Queries) error,
) (repo.GetIdentityProviderConnectionByOrganizationRow, error) {
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return repo.GetIdentityProviderConnectionByOrganizationRow{}, oops.E(oops.CodeUnexpected, err, "error updating sign-in configuration").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)
	if err := update(queries); err != nil {
		return repo.GetIdentityProviderConnectionByOrganizationRow{}, oops.E(oops.CodeUnexpected, err, "error saving sign-in configuration").LogError(ctx, logger)
	}
	after, err := queries.GetIdentityProviderConnectionByOrganization(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return repo.GetIdentityProviderConnectionByOrganizationRow{}, oops.E(oops.CodeUnexpected, err, "error loading updated sign-in configuration").LogError(ctx, logger)
	}
	if err := s.audit.LogIdentityProviderConnectionUpdated(ctx, dbtx, audit.LogIdentityProviderConnectionUpdatedEvent{
		OrganizationID:                           authCtx.ActiveOrganizationID,
		Actor:                                    urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:                         authCtx.Email,
		ActorSlug:                                nil,
		IdentityProviderConnectionURN:            urn.NewIdentityProviderConnectionID(after.ID),
		TenantIdentifier:                         after.TenantIdentifier,
		IdentityProviderConnectionSnapshotBefore: identityProviderConnectionSnapshot(before, ""),
		IdentityProviderConnectionSnapshotAfter:  identityProviderConnectionSnapshot(after, ""),
	}); err != nil {
		return repo.GetIdentityProviderConnectionByOrganizationRow{}, oops.E(oops.CodeUnexpected, err, "error recording sign-in configuration update").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return repo.GetIdentityProviderConnectionByOrganizationRow{}, oops.E(oops.CodeUnexpected, err, "error saving sign-in configuration").LogError(ctx, logger)
	}
	return after, nil
}

func buildSignInSetupStep(row repo.GetIdentityProviderConnectionByOrganizationRow) (*gen.IdentityProviderSetupStep, error) {
	evidence, err := decodeStoredSignInEvidence(row.SignInEvidence)
	if err != nil {
		return nil, err
	}
	step := &gen.IdentityProviderSetupStep{
		Key:            setupStepSignIn,
		Title:          "Configure Okta sign-in",
		Where:          "our_page",
		Instructions:   []string{"Verify the Okta connection before creating the sign-in application."},
		DeepLink:       nil,
		PrintedValues:  []*gen.IdentityProviderPrintedValue{},
		ExpectedValues: []*gen.IdentityProviderExpectedValue{},
		Claims:         signInClaims(evidence.GroupsClaimProvisioned),
		Repair:         nil,
		PortalIntent:   nil,
		State:          signInStepState(conv.FromPGText[string](row.SignInState)),
		LastOutcome:    signInLastOutcome(evidence, row.Capabilities, row.GrantedScopes),
	}
	if !row.SignInApplicationID.Valid {
		if row.Status == "active" && slices.Contains(row.Capabilities, capabilitySignInProvisioning) {
			step.Instructions = []string{"Create the Speakeasy OIDC sign-in application in Okta and assign it to Everyone."}
			return step, nil
		}
		if row.Status == "active" {
			step.Where = "their_console"
			step.Instructions = []string{
				"Create an OIDC Web Application named Speakeasy sign-in in the Okta Admin Console.",
				"Use Authorization Code and Refresh Token grants, require PKCE, and assign the app to Everyone.",
				"Enter the application's Client ID and client secret in Speakeasy. The secret is sent to WorkOS and not retained.",
			}
			step.DeepLink = oktaAdminAppsURL(row.TenantIdentifier)
			step.PrintedValues = signInPrintedValues(row.TenantIdentifier, "", false, "")
			step.ExpectedValues = []*gen.IdentityProviderExpectedValue{
				buildExpectedSetupValue(setupValueClientID, "Client ID", false, nil),
				buildExpectedSetupValue(setupValueClientSecret, "Client secret", true, nil),
			}
		}
		return step, nil
	}

	step.Where = "their_console"
	workOSConnectionMissing := !row.WorkosConnectionID.Valid || strings.TrimSpace(row.WorkosConnectionID.String) == ""
	canProvisionClaims := slices.Contains(row.Capabilities, capabilityClaimsProvisioning)
	if workOSConnectionMissing && !slices.Contains(row.Capabilities, capabilitySignInProvisioning) {
		step.Instructions = []string{
			"Open WorkOS Admin Portal and choose the OpenID Connect connection.",
			"Enter the Okta application's Client ID and client secret again, or copy them directly into WorkOS Admin Portal.",
			"The client secret is sent to WorkOS and not retained by Speakeasy.",
		}
		step.DeepLink = oktaAdminApplicationURL(row.TenantIdentifier, row.SignInApplicationID.String, "general")
		step.PrintedValues = signInPrintedValues(row.TenantIdentifier, evidence.ClientID, canProvisionClaims, evidence.RedirectURI)
		step.ExpectedValues = []*gen.IdentityProviderExpectedValue{
			buildExpectedSetupValue(setupValueClientID, "Client ID", false, conv.PtrEmpty(evidence.ClientID)),
			buildExpectedSetupValue(setupValueClientSecret, "Client secret", true, nil),
		}
		step.PortalIntent = new("sso")
		return step, nil
	}
	if canProvisionClaims {
		step.Where = "our_page"
		step.Instructions = []string{
			"Speakeasy created the Okta sign-in application, identity claims, and WorkOS OIDC connection.",
		}
		step.PrintedValues = signInPrintedValues(row.TenantIdentifier, evidence.ClientID, true, evidence.RedirectURI)
		if workOSConnectionMissing {
			step.Where = "their_console"
			step.Instructions = []string{
				"Open WorkOS Admin Portal and choose the OpenID Connect connection.",
				"Copy the client secret from the Okta application directly into WorkOS Admin Portal. It is not retained by Speakeasy.",
				"Use the Client ID, issuer, and discovery URL shown below, then activate the connection.",
			}
			step.DeepLink = oktaAdminApplicationURL(row.TenantIdentifier, row.SignInApplicationID.String, "general")
			step.PortalIntent = new("sso")
		}
		return step, nil
	}
	step.Instructions = []string{
		"Speakeasy created the Okta sign-in application and WorkOS OIDC connection.",
		"Configure the groups claim in Okta, then confirm whether sign-in tokens or the directory will supply groups.",
	}
	step.DeepLink = oktaAdminApplicationURL(row.TenantIdentifier, row.SignInApplicationID.String, "sign-on")
	step.PrintedValues = signInPrintedValues(row.TenantIdentifier, evidence.ClientID, false, evidence.RedirectURI)
	step.ExpectedValues = []*gen.IdentityProviderExpectedValue{
		buildExpectedSetupValue(setupValueGroupsClaimConfirmed, "Groups claim confirmed", false, conv.PtrEmpty(fmt.Sprintf("%t", row.GroupsClaimConfirmed))),
		buildExpectedSetupValue(setupValueGroupsSource, "Groups source", false, conv.FromPGText[string](row.GroupsSource)),
	}
	step.Repair = &gen.IdentityProviderRepair{
		Title: "Repair the groups claim",
		Instructions: []string{
			"Open the Sign On tab, then find OpenID Connect ID Token.",
			"Set Groups claim type to Filter.",
			"Set the claim name to groups and Matches regex to .*.",
		},
		DeepLink:          oktaAdminApplicationURL(row.TenantIdentifier, row.SignInApplicationID.String, "sign-on"),
		FallbackAvailable: true,
	}
	if workOSConnectionMissing {
		step.Instructions = []string{
			"Open WorkOS Admin Portal and choose the OpenID Connect connection.",
			"Copy the client secret from the Okta application directly into WorkOS Admin Portal. It is not retained by Speakeasy.",
			"Use the Client ID, issuer, and discovery URL shown below, then activate the connection.",
		}
		step.DeepLink = oktaAdminApplicationURL(row.TenantIdentifier, row.SignInApplicationID.String, "general")
		step.PortalIntent = new("sso")
	}
	return step, nil
}

func signInPrintedValues(tenantIdentifier, clientID string, customAuthorizationServer bool, redirectURI string) []*gen.IdentityProviderPrintedValue {
	values := make([]*gen.IdentityProviderPrintedValue, 0, 4)
	if clientID != "" {
		values = append(values, &gen.IdentityProviderPrintedValue{Label: "Client ID", Value: clientID, Copyable: true, Secret: false})
	}
	issuer := "https://" + tenantIdentifier
	if customAuthorizationServer {
		issuer += "/oauth2/default"
	}
	values = append(values,
		&gen.IdentityProviderPrintedValue{Label: "Issuer", Value: issuer, Copyable: true, Secret: false},
		&gen.IdentityProviderPrintedValue{Label: "Discovery URL", Value: issuer + "/.well-known/openid-configuration", Copyable: true, Secret: false},
	)
	if redirectURI != "" {
		values = append(values, &gen.IdentityProviderPrintedValue{Label: "Sign-in redirect URI", Value: redirectURI, Copyable: true, Secret: false})
	}
	return values
}

func signInClaims(groupsProvisioned bool) []*gen.IdentityProviderClaim {
	return []*gen.IdentityProviderClaim{
		{Name: "email", Purpose: "identity", CarriesAccess: false, Provisioned: false},
		{Name: "first_name", Purpose: "display", CarriesAccess: false, Provisioned: groupsProvisioned},
		{Name: "last_name", Purpose: "display", CarriesAccess: false, Provisioned: groupsProvisioned},
		{Name: "groups", Purpose: "used by access rules", CarriesAccess: true, Provisioned: groupsProvisioned},
	}
}

func decodeStoredSignInEvidence(raw []byte) (storedSignInEvidence, error) {
	if len(raw) == 0 {
		return storedSignInEvidence{ClientID: "", RedirectURI: "", GroupsClaimProvisioned: false, Outcome: "", Detail: "", CheckedAt: "", Reads: nil}, nil
	}
	var evidence storedSignInEvidence
	if err := json.Unmarshal(raw, &evidence); err != nil {
		return storedSignInEvidence{ClientID: "", RedirectURI: "", GroupsClaimProvisioned: false, Outcome: "", Detail: "", CheckedAt: "", Reads: nil}, fmt.Errorf("decode sign-in evidence: %w", err)
	}
	return evidence, nil
}

func signInLastOutcome(evidence storedSignInEvidence, capabilities, grantedScopes []string) *gen.IdentityProviderVerifyResult {
	if !validSignInVerifyOutcome(evidence.Outcome) {
		return nil
	}
	if _, err := time.Parse(time.RFC3339, evidence.CheckedAt); err != nil {
		return nil
	}
	reads := make([]*gen.IdentityProviderCapabilityRead, 0, len(evidence.Reads))
	for _, read := range evidence.Reads {
		if !validSignInCapabilityResource(read.Resource) {
			continue
		}
		reads = append(reads, &gen.IdentityProviderCapabilityRead{
			Capability: read.Capability,
			Resource:   read.Resource,
			OK:         read.OK,
			Count:      read.Count,
			Detail:     read.Detail,
		})
	}
	return &gen.IdentityProviderVerifyResult{
		Outcome:       evidence.Outcome,
		Detail:        evidence.Detail,
		Capabilities:  append([]string(nil), capabilities...),
		GrantedScopes: append([]string(nil), grantedScopes...),
		Evidence:      &gen.IdentityProviderVerifyEvidence{CheckedAt: evidence.CheckedAt, Reads: reads},
	}
}

func validSignInVerifyOutcome(outcome string) bool {
	switch outcome {
	case "passed", "pending_validation", "unreachable", "refused", "mismatched_value", "capability_missing":
		return true
	default:
		return false
	}
}

func validSignInCapabilityResource(resource string) bool {
	switch resource {
	case "groups", "users", "apps", "sign_in_application", "sign_in_connection":
		return true
	default:
		return false
	}
}

func signInStepState(state *string) string {
	value := conv.PtrValOr(state, "")
	switch value {
	case "application_created":
		return "awaiting_verification"
	case "passed", "failed":
		return value
	default:
		return "not_started"
	}
}

func oktaAdminApplicationURL(tenantIdentifier, applicationID, tab string) *string {
	base := oktaAdminAppsURL(tenantIdentifier)
	if base == nil || applicationID == "" {
		return nil
	}
	parsed, err := url.Parse(*base)
	if err != nil {
		return nil
	}
	parsed.Path = "/admin/app/oidc_client/instance/" + url.PathEscape(applicationID) + "/"
	parsed.Fragment = "tab-" + tab
	return conv.PtrEmpty(parsed.String())
}
