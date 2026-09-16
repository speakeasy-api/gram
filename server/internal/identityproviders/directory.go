package identityproviders

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

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

func (s *Service) submitDirectorySetupStep(
	ctx context.Context,
	authCtx *contextvalues.AuthContext,
	logger *slog.Logger,
	payload *gen.SubmitSetupStepPayload,
) (*gen.SubmitSetupStepResult, error) {
	if len(payload.Values) != 0 {
		return nil, oops.E(oops.CodeBadRequest, nil, "directory setup does not accept values").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error locking directory configuration").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)
	before, err := queries.GetIdentityProviderConnectionByOrganization(ctx, authCtx.ActiveOrganizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, nil, "identity provider connection not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error loading directory configuration").LogError(ctx, logger)
	}
	if _, err := queries.LockOktaIdentityProviderDirectory(ctx, repo.LockOktaIdentityProviderDirectoryParams{
		OrganizationID:               authCtx.ActiveOrganizationID,
		IdentityProviderConnectionID: before.ID,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error locking directory configuration").LogError(ctx, logger)
	}
	before, err = queries.GetIdentityProviderConnectionByOrganization(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error loading locked directory configuration").LogError(ctx, logger)
	}
	if before.Status != "active" {
		return nil, oops.E(oops.CodeBadRequest, nil, "verify the Okta connection before configuring directory sync").LogError(ctx, logger)
	}
	if !before.DirectoryScimBaseUrl.Valid || !before.DirectoryScimTokenEncrypted.Valid {
		return nil, oops.E(oops.CodeBadRequest, nil, "directory handoff is not ready").LogError(ctx, logger)
	}
	if !slices.Contains(before.Capabilities, capabilityGroupAssignment) {
		return nil, oops.E(oops.CodeBadRequest, nil, "verify the okta.groups.manage scope before configuring directory sync").LogError(ctx, logger)
	}

	token, err := s.acquireOktaManagementToken(ctx, authCtx.ActiveOrganizationID, before, oktaDirectoryProvisioningScopes)
	if err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "error authorizing Okta directory setup").LogError(ctx, logger)
	}
	tenantDomain := normalizeOktaDomain(before.TenantIdentifier)
	applicationID := conv.FromPGTextOrEmpty[string](before.DirectoryApplicationID)
	if applicationID == "" {
		application, err := s.okta.CreateDirectoryApplication(ctx, tenantDomain, token.AccessToken)
		if err != nil {
			return nil, oops.E(oops.CodeGatewayError, err, "error creating the Okta directory application").LogError(ctx, logger)
		}
		if application.ID == "" || application.Status != "ACTIVE" {
			return nil, oops.E(oops.CodeGatewayError, nil, "Okta returned an incomplete directory application").LogError(ctx, logger)
		}
		applicationID = application.ID
	} else {
		application, err := s.okta.GetApplication(ctx, tenantDomain, token.AccessToken, applicationID)
		if err != nil {
			return nil, oops.E(oops.CodeGatewayError, err, "error reading the existing Okta directory application").LogError(ctx, logger)
		}
		if application.ID != applicationID || application.Status != "ACTIVE" {
			return nil, oops.E(oops.CodeGatewayError, nil, "the existing Okta directory application is not active").LogError(ctx, logger)
		}
	}

	seenCursors := make(map[string]struct{})
	afterCursor := ""
	for {
		page, err := s.okta.ListGroups(ctx, tenantDomain, token.AccessToken, okta.PageRequest{Limit: 200, After: afterCursor})
		if err != nil {
			return nil, oops.E(oops.CodeGatewayError, err, "error listing Okta groups for directory setup").LogError(ctx, logger)
		}
		for _, raw := range page.Items {
			group, err := okta.DecodeGroup(raw)
			if err != nil {
				return nil, oops.E(oops.CodeGatewayError, err, "error reading an Okta group for directory setup").LogError(ctx, logger)
			}
			if group.ID == "" || group.Type == "BUILT_IN" || strings.EqualFold(group.Name, "Everyone") {
				continue
			}
			if err := s.okta.AssignGroupToApplication(ctx, tenantDomain, token.AccessToken, applicationID, group.ID); err != nil {
				return nil, oops.E(oops.CodeGatewayError, err, "error assigning an Okta group to the directory application").LogError(ctx, logger)
			}
		}
		if page.NextCursor == "" {
			break
		}
		if _, exists := seenCursors[page.NextCursor]; exists {
			return nil, oops.E(oops.CodeGatewayError, nil, "Okta repeated a group pagination cursor").LogError(ctx, logger)
		}
		seenCursors[page.NextCursor] = struct{}{}
		afterCursor = page.NextCursor
	}

	if err := queries.UpdateOktaIdentityProviderDirectoryApplication(ctx, repo.UpdateOktaIdentityProviderDirectoryApplicationParams{
		DirectoryApplicationID:       conv.ToPGText(applicationID),
		OrganizationID:               authCtx.ActiveOrganizationID,
		IdentityProviderConnectionID: before.ID,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving the Okta directory application").LogError(ctx, logger)
	}
	after, err := queries.GetIdentityProviderConnectionByOrganization(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error loading the saved directory configuration").LogError(ctx, logger)
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
		return nil, oops.E(oops.CodeUnexpected, err, "error recording directory configuration").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving directory configuration").LogError(ctx, logger)
	}

	step, err := s.buildDirectorySetupStep(after)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error building directory setup").LogError(ctx, logger)
	}
	return &gen.SubmitSetupStepResult{Step: step, FieldOutcomes: []*gen.IdentityProviderFieldOutcome{}, NextStepKey: nil}, nil
}

func (s *Service) verifyDirectorySetupStep(
	ctx context.Context,
	authCtx *contextvalues.AuthContext,
	logger *slog.Logger,
) (*gen.IdentityProviderVerifyResult, error) {
	before, err := repo.New(s.db).GetIdentityProviderConnectionByOrganization(ctx, authCtx.ActiveOrganizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, nil, "identity provider connection not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error loading directory configuration").LogError(ctx, logger)
	}
	applicationID := conv.FromPGTextOrEmpty[string](before.DirectoryApplicationID)
	if applicationID == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "set up the Okta directory application before verification").LogError(ctx, logger)
	}
	if !before.WorkosID.Valid || strings.TrimSpace(before.WorkosID.String) == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "organization is not linked to the sign-in provider").LogError(ctx, logger)
	}

	token, err := s.acquireOktaManagementToken(ctx, authCtx.ActiveOrganizationID, before, requiredOktaReadScopes)
	if err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "error authorizing Okta directory verification").LogError(ctx, logger)
	}
	checkedAt := time.Now().UTC()
	tenantDomain := normalizeOktaDomain(before.TenantIdentifier)
	applicationRead := &gen.IdentityProviderCapabilityRead{Capability: capabilityGroupAssignment, Resource: "directory_application", OK: false, Count: nil, Detail: nil}
	connectionRead := &gen.IdentityProviderCapabilityRead{Capability: capabilityGroupAssignment, Resource: "directory_connection", OK: false, Count: nil, Detail: nil}
	groupsRead := &gen.IdentityProviderCapabilityRead{Capability: capabilityDirectoryRead, Resource: "directory_groups", OK: false, Count: nil, Detail: nil}
	usersRead := &gen.IdentityProviderCapabilityRead{Capability: capabilityDirectoryRead, Resource: "directory_users", OK: false, Count: nil, Detail: nil}

	applicationActive := false
	application, applicationErr := s.okta.GetApplication(ctx, tenantDomain, token.AccessToken, applicationID)
	if applicationErr != nil {
		detail := "Unable to read the Okta directory application."
		applicationRead.Detail = &detail
	} else {
		applicationActive = application.ID == applicationID && application.Status == "ACTIVE"
		applicationRead.OK = applicationActive
		detail := "Okta directory application status: " + conv.Default(application.Status, "unknown") + "."
		applicationRead.Detail = &detail
	}

	if applicationErr == nil {
		connection, connectionErr := s.okta.GetProvisioningConnection(ctx, tenantDomain, token.AccessToken, applicationID)
		if connectionErr != nil {
			detail := "Unable to read the Okta provisioning connection."
			connectionRead.Detail = &detail
		} else {
			connectionRead.OK = true
			detail := "Okta provisioning connection status: " + conv.Default(connection.Status, "unknown") + "."
			connectionRead.Detail = &detail
		}
	}

	directories, directoriesErr := s.workos.ListDirectories(ctx, before.WorkosID.String)
	directory := selectWorkOSDirectory(directories, conv.FromPGTextOrEmpty[string](before.DirectoryWorkosID))
	var groupCount, userCount *int
	if directoriesErr != nil {
		detail := conv.PtrValOr(connectionRead.Detail, "") + " Unable to read the sign-in provider directory."
		connectionRead.OK = false
		connectionRead.Detail = &detail
	} else if directory == nil {
		detail := conv.PtrValOr(connectionRead.Detail, "") + " The sign-in provider has not linked a directory yet."
		connectionRead.Detail = &detail
	} else {
		detail := conv.PtrValOr(connectionRead.Detail, "") + " The sign-in provider directory state: " + directory.State + "."
		connectionRead.Detail = &detail
		groups, groupsErr := s.workos.ListDirectoryGroups(ctx, directory.ID)
		if groupsErr != nil {
			detail := "Unable to read the sign-in provider directory groups."
			groupsRead.Detail = &detail
		} else {
			count := len(groups)
			groupCount = &count
			groupsRead.OK = true
			groupsRead.Count = &count
			detail := fmt.Sprintf("The sign-in provider received %d directory group(s).", count)
			groupsRead.Detail = &detail
		}
		users, usersErr := s.workos.ListDirectoryUsers(ctx, directory.ID)
		if usersErr != nil {
			detail := "Unable to read the sign-in provider directory users."
			usersRead.Detail = &detail
		} else {
			count := len(users)
			userCount = &count
			usersRead.OK = true
			usersRead.Count = &count
			detail := fmt.Sprintf("The sign-in provider received %d directory user(s).", count)
			usersRead.Detail = &detail
		}
	}

	outcome := "pending_validation"
	detail := "Finish the Provisioning tab in Okta or wait for the first directory group push, then check again."
	directoryState := "pending_validation"
	if applicationErr != nil {
		outcome = "unreachable"
		detail = "Unable to read the Okta directory application."
		directoryState = "failed"
	} else if !applicationActive {
		outcome = "mismatched_value"
		detail = "The Okta directory application is not active."
		directoryState = "failed"
	} else if directory != nil && directory.State == "linked" && groupCount != nil && *groupCount > 0 {
		outcome = "passed"
		detail = "Okta directory sync is linked and the sign-in provider has received directory groups."
		directoryState = "passed"
	}
	result := &gen.IdentityProviderVerifyResult{
		Outcome:       outcome,
		Detail:        detail,
		Capabilities:  append([]string(nil), before.Capabilities...),
		GrantedScopes: append([]string(nil), before.GrantedScopes...),
		Evidence: &gen.IdentityProviderVerifyEvidence{
			CheckedAt: checkedAt.Format(time.RFC3339Nano),
			Reads:     []*gen.IdentityProviderCapabilityRead{applicationRead, connectionRead, groupsRead, usersRead},
		},
	}
	encoded, err := marshalStoredVerification(result)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error encoding directory verification").LogError(ctx, logger)
	}
	directoryGroupCount, err := optionalInt4(groupCount)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving directory group count").LogError(ctx, logger)
	}
	directoryUserCount, err := optionalInt4(userCount)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving directory user count").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving directory verification").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)
	updated, err := queries.UpdateOktaIdentityProviderDirectoryVerification(ctx, repo.UpdateOktaIdentityProviderDirectoryVerificationParams{
		DirectoryState:               conv.ToPGText(directoryState),
		DirectoryGroupCount:          directoryGroupCount,
		DirectoryUserCount:           directoryUserCount,
		DirectoryEvidence:            encoded,
		OrganizationID:               authCtx.ActiveOrganizationID,
		IdentityProviderConnectionID: before.ID,
		DirectoryApplicationID:       before.DirectoryApplicationID,
		PreviousDirectoryEvidence:    before.DirectoryEvidence,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving directory verification").LogError(ctx, logger)
	}
	if updated != 1 {
		return nil, oops.E(oops.CodeConflict, nil, "directory configuration changed during verification").LogError(ctx, logger)
	}
	if directory != nil && !before.DirectoryWorkosID.Valid {
		if err := queries.CacheOrganizationDirectoryWorkOSID(ctx, repo.CacheOrganizationDirectoryWorkOSIDParams{
			DirectoryWorkosID: conv.ToPGText(directory.ID),
			OrganizationID:    authCtx.ActiveOrganizationID,
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error saving the sign-in provider directory identifier").LogError(ctx, logger)
		}
	}
	after, err := queries.GetIdentityProviderConnectionByOrganization(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error loading verified directory configuration").LogError(ctx, logger)
	}
	if err := s.audit.LogIdentityProviderConnectionVerified(ctx, dbtx, audit.LogIdentityProviderConnectionVerifiedEvent{
		OrganizationID:                           authCtx.ActiveOrganizationID,
		Actor:                                    urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:                         authCtx.Email,
		ActorSlug:                                nil,
		IdentityProviderConnectionURN:            urn.NewIdentityProviderConnectionID(after.ID),
		TenantIdentifier:                         after.TenantIdentifier,
		IdentityProviderConnectionSnapshotBefore: identityProviderConnectionSnapshot(before, ""),
		IdentityProviderConnectionSnapshotAfter:  identityProviderConnectionSnapshot(after, ""),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error recording directory verification").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving directory verification").LogError(ctx, logger)
	}
	return result, nil
}

func (s *Service) buildDirectorySetupStep(row repo.GetIdentityProviderConnectionByOrganizationRow) (*gen.IdentityProviderSetupStep, error) {
	step := &gen.IdentityProviderSetupStep{
		Key:            setupStepDirectory,
		Title:          "Set up directory sync",
		Where:          "their_console",
		Instructions:   []string{"Verify the Okta connection before configuring directory sync."},
		DeepLink:       nil,
		PrintedValues:  []*gen.IdentityProviderPrintedValue{},
		ExpectedValues: []*gen.IdentityProviderExpectedValue{},
		Claims:         nil,
		Repair:         nil,
		PortalIntent:   nil,
		State:          directoryStepState(conv.FromPGText[string](row.DirectoryState)),
		LastOutcome:    decodeDirectoryLastOutcome(row.DirectoryEvidence),
	}
	if !row.DirectoryScimBaseUrl.Valid || !row.DirectoryScimTokenEncrypted.Valid {
		step.Instructions = []string{"Open the setup portal and configure directory sync for this organization."}
		step.PortalIntent = new("dsync")
		return step, nil
	}
	if row.Status != "active" {
		return step, nil
	}
	applicationID := conv.FromPGTextOrEmpty[string](row.DirectoryApplicationID)
	if applicationID == "" {
		step.Instructions = []string{"Create the Speakeasy directory application in Okta and assign the organization's groups."}
		return step, nil
	}

	// This is the only boundary that decrypts the handoff token. Callers must
	// return it solely as a secret printed value to an authorized org admin.
	token, err := s.encryption.Decrypt(row.DirectoryScimTokenEncrypted.String)
	if err != nil {
		return nil, fmt.Errorf("decrypt directory handoff token: %w", err)
	}
	deepLink := oktaDirectoryProvisioningURL(row.TenantIdentifier, applicationID)
	step.Instructions = []string{
		"Open the Provisioning tab, select Configure API Integration, and enable API integration.",
		"Paste the SCIM base URL and bearer token, test the API credentials, and save.",
		"Under To App, enable Create Users, Update User Attributes, and Deactivate Users.",
		"Under Push Groups, push the groups assigned to the application.",
	}
	step.DeepLink = deepLink
	step.PrintedValues = []*gen.IdentityProviderPrintedValue{
		{Label: "SCIM base URL", Value: row.DirectoryScimBaseUrl.String, Copyable: true, Secret: false},
		{Label: "Bearer token", Value: token, Copyable: true, Secret: true},
	}
	if deepLink != nil {
		step.PrintedValues = append(step.PrintedValues, &gen.IdentityProviderPrintedValue{Label: "Provisioning tab", Value: *deepLink, Copyable: false, Secret: false})
	}
	return step, nil
}

func decodeDirectoryLastOutcome(encoded []byte) *gen.IdentityProviderVerifyResult {
	if len(encoded) == 0 {
		return nil
	}
	var stored storedVerification
	if err := json.Unmarshal(encoded, &stored); err != nil || stored.Outcome == "" || stored.CheckedAt == "" {
		return nil
	}
	reads := make([]*gen.IdentityProviderCapabilityRead, 0, len(stored.Reads))
	for _, read := range stored.Reads {
		reads = append(reads, &gen.IdentityProviderCapabilityRead{Capability: read.Capability, Resource: read.Resource, OK: read.OK, Count: read.Count, Detail: read.Detail})
	}
	return &gen.IdentityProviderVerifyResult{
		Outcome:       stored.Outcome,
		Detail:        stored.Detail,
		Capabilities:  stored.Capabilities,
		GrantedScopes: stored.GrantedScopes,
		Evidence:      &gen.IdentityProviderVerifyEvidence{CheckedAt: stored.CheckedAt, Reads: reads},
	}
}

func selectWorkOSDirectory(directories []workos.Directory, storedID string) *workos.Directory {
	if storedID != "" {
		for i := range directories {
			if directories[i].ID == storedID {
				return &directories[i]
			}
		}
		return nil
	}
	if len(directories) == 1 {
		return &directories[0]
	}
	var linked *workos.Directory
	for i := range directories {
		if directories[i].State != "linked" {
			continue
		}
		if linked != nil {
			return nil
		}
		linked = &directories[i]
	}
	return linked
}

func directoryStepState(state *string) string {
	switch conv.PtrValOr(state, "") {
	case "configured", "pending_validation":
		return "awaiting_verification"
	case "passed", "failed":
		return conv.PtrValOr(state, "")
	default:
		return "not_started"
	}
}

func oktaDirectoryProvisioningURL(tenantIdentifier, applicationID string) *string {
	base := oktaAdminAppsURL(tenantIdentifier)
	if base == nil || applicationID == "" {
		return nil
	}
	parsed, err := url.Parse(*base)
	if err != nil {
		return nil
	}
	parsed.Path = "/admin/app/" + okta.DirectoryApplicationName + "/instance/" + url.PathEscape(applicationID) + "/"
	parsed.Fragment = "tab-provisioning"
	return conv.PtrEmpty(parsed.String())
}

func optionalInt4(value *int) (pgtype.Int4, error) {
	if value == nil {
		return pgtype.Int4{Int32: 0, Valid: false}, nil
	}
	if *value < 0 || *value > math.MaxInt32 {
		return pgtype.Int4{}, fmt.Errorf("directory count %d is outside the PostgreSQL integer range", *value)
	}
	return pgtype.Int4{Int32: int32(*value), Valid: true}, nil
}
