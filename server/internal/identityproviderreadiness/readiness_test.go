package identityproviderreadiness_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/identityproviderreadiness"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

type workOSDouble struct {
	domains              *workos.OrganizationDomainPolicy
	domainErr            error
	directories          []workos.Directory
	directoryErr         error
	connectionsAvailable bool
	connectionsErr       error
	domainCalls          int
	directoryCalls       int
}

func (d *workOSDouble) GetOrganizationDomainPolicy(context.Context, string) (*workos.OrganizationDomainPolicy, error) {
	d.domainCalls++
	return d.domains, d.domainErr
}

func (d *workOSDouble) ListDirectories(context.Context, string) ([]workos.Directory, error) {
	d.directoryCalls++
	return d.directories, d.directoryErr
}

func (d *workOSDouble) ConnectionsAPIAvailable(context.Context) (bool, error) {
	return d.connectionsAvailable, d.connectionsErr
}

type featureDouble struct {
	values map[productfeatures.Feature]bool
	errors map[productfeatures.Feature]error
}

func (d featureDouble) IsFeatureEnabled(_ context.Context, _ string, feature productfeatures.Feature) (bool, error) {
	return d.values[feature], d.errors[feature]
}

type handoffDouble struct {
	stored bool
	err    error
}

func (d handoffDouble) HasDirectoryHandoff(context.Context, string) (bool, error) {
	return d.stored, d.err
}

func readyDependencies() (*workOSDouble, featureDouble, handoffDouble) {
	featureValues := map[productfeatures.Feature]bool{}
	featureValues[productfeatures.FeatureSSO] = true
	featureValues[productfeatures.FeatureSCIM] = true
	return &workOSDouble{
		domains: &workos.OrganizationDomainPolicy{Domains: []workos.OrganizationDomain{{
			Domain: "example.test", State: workos.OrganizationDomainStateVerified,
		}}},
		domainErr:            nil,
		directories:          []workos.Directory{{ID: "directory", OrganizationID: "workos-org", Type: "okta scim v2.0", Name: "Okta", State: "linked", CreatedAt: "", UpdatedAt: ""}},
		directoryErr:         nil,
		connectionsAvailable: true,
		connectionsErr:       nil,
	}, featureDouble{
		values: featureValues,
		errors: map[productfeatures.Feature]error{},
	}, handoffDouble{stored: true, err: nil}
}

func TestEvaluateEligibleWhenEveryCheckPasses(t *testing.T) {
	t.Parallel()

	workosClient, features, handoff := readyDependencies()
	result := identityproviderreadiness.New(workosClient, features, handoff).Evaluate(t.Context(), "organization", "workos-org")

	require.True(t, result.Eligible)
	require.Equal(t, "okta", result.Provider)
	require.NotEmpty(t, result.CheckedAt)
	require.Equal(t, []string{
		"workos_organization_linked",
		"workos_domain_verified",
		"directory_handoff_stored",
		"workos_directory_created",
		"connections_api_available",
		"sso_feature_enabled",
		"scim_feature_enabled",
	}, readinessKeys(result.Checks))
	for _, check := range result.Checks {
		require.True(t, check.OK)
		require.Equal(t, result.CheckedAt, check.CheckedAt)
	}
}

func TestEvaluateReportsMissingWorkOSLinkWithoutDependentCalls(t *testing.T) {
	t.Parallel()

	workosClient, features, handoff := readyDependencies()
	result := identityproviderreadiness.New(workosClient, features, handoff).Evaluate(t.Context(), "organization", "")

	require.False(t, result.Eligible)
	require.False(t, readinessCheck(result.Checks, "workos_organization_linked").OK)
	require.False(t, readinessCheck(result.Checks, "workos_domain_verified").OK)
	require.False(t, readinessCheck(result.Checks, "workos_directory_created").OK)
	require.Zero(t, workosClient.domainCalls)
	require.Zero(t, workosClient.directoryCalls)
}

func TestEvaluateReportsUnverifiedDomain(t *testing.T) {
	t.Parallel()

	workosClient, features, handoff := readyDependencies()
	workosClient.domains = &workos.OrganizationDomainPolicy{Domains: []workos.OrganizationDomain{{Domain: "example.test", State: workos.OrganizationDomainStatePending}}}
	result := identityproviderreadiness.New(workosClient, features, handoff).Evaluate(t.Context(), "organization", "workos-org")

	check := readinessCheck(result.Checks, "workos_domain_verified")
	require.False(t, check.OK)
	require.Equal(t, "WorkOS organization has no verified domain.", check.Detail)
}

func TestEvaluateReportsUnavailableDirectoryHandoff(t *testing.T) {
	t.Parallel()

	workosClient, features, _ := readyDependencies()
	result := identityproviderreadiness.New(workosClient, features, identityproviderreadiness.UnavailableDirectoryHandoffChecker{}).Evaluate(t.Context(), "organization", "workos-org")

	check := readinessCheck(result.Checks, "directory_handoff_stored")
	require.False(t, check.OK)
	require.Equal(t, "not available on this build", check.Detail)
}

func TestEvaluateReportsMissingWorkOSDirectory(t *testing.T) {
	t.Parallel()

	workosClient, features, handoff := readyDependencies()
	workosClient.directories = nil
	result := identityproviderreadiness.New(workosClient, features, handoff).Evaluate(t.Context(), "organization", "workos-org")

	check := readinessCheck(result.Checks, "workos_directory_created")
	require.False(t, check.OK)
	require.Equal(t, "WorkOS organization has no directory.", check.Detail)
}

func TestEvaluateReportsUnavailableConnectionsAPI(t *testing.T) {
	t.Parallel()

	workosClient, features, handoff := readyDependencies()
	workosClient.connectionsAvailable = false
	result := identityproviderreadiness.New(workosClient, features, handoff).Evaluate(t.Context(), "organization", "workos-org")

	check := readinessCheck(result.Checks, "connections_api_available")
	require.False(t, check.OK)
	require.Equal(t, "WorkOS Connections API is not available.", check.Detail)
}

func TestEvaluateReportsDisabledProductFeaturesIndependently(t *testing.T) {
	t.Parallel()

	workosClient, features, handoff := readyDependencies()
	features.values[productfeatures.FeatureSSO] = false
	features.values[productfeatures.FeatureSCIM] = false
	result := identityproviderreadiness.New(workosClient, features, handoff).Evaluate(t.Context(), "organization", "workos-org")

	require.False(t, readinessCheck(result.Checks, "sso_feature_enabled").OK)
	require.False(t, readinessCheck(result.Checks, "scim_feature_enabled").OK)
}

func TestEvaluateDegradesWorkOSFailuresToChecks(t *testing.T) {
	t.Parallel()

	workosClient, features, handoff := readyDependencies()
	workosClient.domainErr = errors.New("domain unavailable")
	workosClient.directoryErr = errors.New("directory unavailable")
	workosClient.connectionsErr = errors.New("connections unavailable")
	result := identityproviderreadiness.New(workosClient, features, handoff).Evaluate(t.Context(), "organization", "workos-org")

	require.False(t, result.Eligible)
	require.Contains(t, readinessCheck(result.Checks, "workos_domain_verified").Detail, "could not be checked")
	require.Contains(t, readinessCheck(result.Checks, "workos_directory_created").Detail, "could not be checked")
	require.Contains(t, readinessCheck(result.Checks, "connections_api_available").Detail, "could not be checked")
}

func TestEvaluateDegradesFeatureFailuresToChecks(t *testing.T) {
	t.Parallel()

	workosClient, features, handoff := readyDependencies()
	features.errors[productfeatures.FeatureSSO] = errors.New("SSO unavailable")
	features.errors[productfeatures.FeatureSCIM] = errors.New("SCIM unavailable")
	result := identityproviderreadiness.New(workosClient, features, handoff).Evaluate(t.Context(), "organization", "workos-org")

	require.False(t, result.Eligible)
	require.Contains(t, readinessCheck(result.Checks, "sso_feature_enabled").Detail, "could not be checked")
	require.Contains(t, readinessCheck(result.Checks, "scim_feature_enabled").Detail, "could not be checked")
}

func readinessCheck(checks []*types.IdentityProviderReadinessCheck, key string) *types.IdentityProviderReadinessCheck {
	for _, check := range checks {
		if check.Key == key {
			return check
		}
	}
	return nil
}

func readinessKeys(checks []*types.IdentityProviderReadinessCheck) []string {
	keys := make([]string, 0, len(checks))
	for _, check := range checks {
		keys = append(keys, check.Key)
	}
	return keys
}
