package identityproviderreadiness

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/gen/types"
	identityproviderrepo "github.com/speakeasy-api/gram/server/internal/identityproviders/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

var ErrDirectoryHandoffUnavailable = errors.New("directory handoff is not available on this build")

type Checker interface {
	Evaluate(context.Context, string, string) *types.IdentityProviderReadiness
}

type WorkOSClient interface {
	GetOrganizationDomainPolicy(context.Context, string) (*workos.OrganizationDomainPolicy, error)
	ListDirectories(context.Context, string) ([]workos.Directory, error)
	ConnectionsAPIAvailable(context.Context) (bool, error)
}

type FeatureChecker interface {
	IsFeatureEnabled(context.Context, string, productfeatures.Feature) (bool, error)
}

type DirectoryHandoffChecker interface {
	HasDirectoryHandoff(context.Context, string) (bool, error)
}

type DatabaseDirectoryHandoffChecker struct {
	queries DirectoryHandoffChecker
}

func NewDatabaseDirectoryHandoffChecker(db identityproviderrepo.DBTX) *DatabaseDirectoryHandoffChecker {
	return &DatabaseDirectoryHandoffChecker{queries: identityproviderrepo.New(db)}
}

func (c *DatabaseDirectoryHandoffChecker) HasDirectoryHandoff(ctx context.Context, organizationID string) (bool, error) {
	stored, err := c.queries.HasDirectoryHandoff(ctx, organizationID)
	if err != nil {
		return false, fmt.Errorf("check directory handoff: %w", err)
	}
	return stored, nil
}

type UnavailableDirectoryHandoffChecker struct{}

func (UnavailableDirectoryHandoffChecker) HasDirectoryHandoff(context.Context, string) (bool, error) {
	return false, ErrDirectoryHandoffUnavailable
}

type UnavailableWorkOSClient struct{}

func (UnavailableWorkOSClient) GetOrganizationDomainPolicy(context.Context, string) (*workos.OrganizationDomainPolicy, error) {
	return nil, errors.New("WorkOS is unavailable")
}

func (UnavailableWorkOSClient) ListDirectories(context.Context, string) ([]workos.Directory, error) {
	return nil, errors.New("WorkOS is unavailable")
}

func (UnavailableWorkOSClient) ConnectionsAPIAvailable(context.Context) (bool, error) {
	return false, errors.New("WorkOS is unavailable")
}

type Readiness struct {
	workos   WorkOSClient
	features FeatureChecker
	handoff  DirectoryHandoffChecker
}

func New(workosClient WorkOSClient, features FeatureChecker, handoff DirectoryHandoffChecker) *Readiness {
	return &Readiness{workos: workosClient, features: features, handoff: handoff}
}

func (r *Readiness) Evaluate(ctx context.Context, organizationID, workosOrganizationID string) *types.IdentityProviderReadiness {
	checkedAt := time.Now().UTC().Format(time.RFC3339)
	checks := make([]*types.IdentityProviderReadinessCheck, 0, 7)
	add := func(key string, ok bool, detail, remedy, owner string) {
		checks = append(checks, &types.IdentityProviderReadinessCheck{
			Key: key, OK: ok, Detail: detail, Remedy: remedy, Owner: owner, CheckedAt: checkedAt,
		})
	}

	workosOrganizationID = strings.TrimSpace(workosOrganizationID)
	linked := workosOrganizationID != ""
	linkedDetail := "WorkOS organization is linked."
	if !linked {
		linkedDetail = "WorkOS organization is not linked."
	}
	add("workos_organization_linked", linked, linkedDetail, "Link the organization to WorkOS in the platform administration workflow.", "platform_admin")

	domainVerified := false
	domainDetail := "WorkOS organization has no verified domain."
	if !linked {
		domainDetail = "WorkOS organization is not linked."
	} else if policy, err := r.workos.GetOrganizationDomainPolicy(ctx, workosOrganizationID); err != nil {
		domainDetail = "WorkOS domain verification could not be checked."
	} else {
		for _, domain := range policy.Domains {
			if domain.State == workos.OrganizationDomainStateVerified || domain.State == workos.OrganizationDomainStateLegacyVerified {
				domainVerified = true
				domainDetail = "WorkOS organization has a verified domain."
				break
			}
		}
	}
	add("workos_domain_verified", domainVerified, domainDetail, "Verify an organization domain in WorkOS.", "platform_admin")

	handoffStored, handoffErr := r.handoff.HasDirectoryHandoff(ctx, organizationID)
	handoffDetail := "Directory handoff credentials are stored."
	if errors.Is(handoffErr, ErrDirectoryHandoffUnavailable) {
		handoffDetail = "not available on this build"
	} else if handoffErr != nil {
		handoffStored = false
		handoffDetail = "Directory handoff storage could not be checked."
	} else if !handoffStored {
		handoffDetail = "Directory handoff credentials are not stored."
	}
	add("directory_handoff_stored", handoffStored, handoffDetail, "Store the directory endpoint and token in the platform administration handoff.", "platform_admin")

	directoryCreated := false
	directoryDetail := "WorkOS organization has no directory."
	if !linked {
		directoryDetail = "WorkOS organization is not linked."
	} else if directories, err := r.workos.ListDirectories(ctx, workosOrganizationID); err != nil {
		directoryDetail = "WorkOS directory creation could not be checked."
	} else if len(directories) > 0 {
		directoryCreated = true
		directoryDetail = "WorkOS organization has a directory."
	}
	add("workos_directory_created", directoryCreated, directoryDetail, "Create the organization's directory in WorkOS.", "platform_admin")

	connectionsAvailable, connectionsErr := r.workos.ConnectionsAPIAvailable(ctx)
	connectionsDetail := "WorkOS Connections API is available."
	if connectionsErr != nil {
		connectionsAvailable = false
		connectionsDetail = "WorkOS Connections API availability could not be checked."
	} else if !connectionsAvailable {
		connectionsDetail = "WorkOS Connections API is not available."
	}
	add("connections_api_available", connectionsAvailable, connectionsDetail, "Ask WorkOS to enable Connections API migration capabilities for this environment.", "speakeasy")

	ssoEnabled, ssoErr := r.features.IsFeatureEnabled(ctx, organizationID, productfeatures.FeatureSSO)
	ssoDetail := "SSO is enabled for this organization."
	if ssoErr != nil {
		ssoEnabled = false
		ssoDetail = "SSO feature availability could not be checked."
	} else if !ssoEnabled {
		ssoDetail = "SSO is not enabled for this organization."
	}
	add("sso_feature_enabled", ssoEnabled, ssoDetail, "Enable the SSO product feature for this organization.", "speakeasy")

	scimEnabled, scimErr := r.features.IsFeatureEnabled(ctx, organizationID, productfeatures.FeatureSCIM)
	scimDetail := "SCIM is enabled for this organization."
	if scimErr != nil {
		scimEnabled = false
		scimDetail = "SCIM feature availability could not be checked."
	} else if !scimEnabled {
		scimDetail = "SCIM is not enabled for this organization."
	}
	add("scim_feature_enabled", scimEnabled, scimDetail, "Enable the SCIM product feature for this organization.", "speakeasy")

	eligible := true
	for _, check := range checks {
		eligible = eligible && check.OK
	}
	return &types.IdentityProviderReadiness{Provider: "okta", Eligible: eligible, Checks: checks, CheckedAt: checkedAt}
}
