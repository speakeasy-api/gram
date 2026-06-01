package activities_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	customdomainsRepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// stubProvisionerFactory wraps a single provisioner for injection in tests.
type stubProvisionerFactory struct {
	provisioner k8s.CustomDomainProvisioner
}

func (f *stubProvisionerFactory) Provisioner(_ k8s.ProvisionerKind) k8s.CustomDomainProvisioner {
	return f.provisioner
}

// --- Delete path (no DB required) ---

func TestCustomDomainIngress_Delete_EmptyResourceName_Errors(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	stub := k8s.NewStubProvisioner(k8s.ProvisionerKindIngress, logger)
	act := activities.NewCustomDomainIngress(logger, nil, &stubProvisionerFactory{provisioner: stub}, k8s.ProvisionerKindIngress)

	err := act.Do(ctx, activities.CustomDomainIngressArgs{
		OrgID:        "org-1",
		Domain:       "test.example.com",
		Action:       activities.CustomDomainIngressActionDelete,
		IngressName:  "",
		ResourceName: "",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "resource name is empty")
}

func TestCustomDomainIngress_Delete_ResourceNameTakesPriority(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	stub := k8s.NewStubProvisioner(k8s.ProvisionerKindIngress, logger)
	act := activities.NewCustomDomainIngress(logger, nil, &stubProvisionerFactory{provisioner: stub}, k8s.ProvisionerKindIngress)

	err := act.Do(ctx, activities.CustomDomainIngressArgs{
		OrgID:        "org-1",
		Domain:       "test.example.com",
		Action:       activities.CustomDomainIngressActionDelete,
		IngressName:  "old-ingress",
		ResourceName: "preferred-resource",
	})
	require.NoError(t, err)

	calls := stub.Calls()
	require.Len(t, calls, 1)
	require.Equal(t, "Delete", calls[0].Method)
	require.Equal(t, "preferred-resource", calls[0].ResourceName)
}

func TestCustomDomainIngress_Delete_IngressNameFallback(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	stub := k8s.NewStubProvisioner(k8s.ProvisionerKindIngress, logger)
	act := activities.NewCustomDomainIngress(logger, nil, &stubProvisionerFactory{provisioner: stub}, k8s.ProvisionerKindIngress)

	err := act.Do(ctx, activities.CustomDomainIngressArgs{
		OrgID:       "org-1",
		Domain:      "test.example.com",
		Action:      activities.CustomDomainIngressActionDelete,
		IngressName: "legacy-ingress",
	})
	require.NoError(t, err)

	calls := stub.Calls()
	require.Len(t, calls, 1)
	require.Equal(t, "Delete", calls[0].Method)
	require.Equal(t, "legacy-ingress", calls[0].ResourceName)
}

// --- Setup path (requires provisioner_kind DB column from migration PR) ---

func TestCustomDomainIngress_Setup_Ingress_UpdatesDB(t *testing.T) {
	t.Parallel()

	const orgID = "org-ingress-setup"
	const domain = "ingress-setup.example.com"
	ctx := t.Context()
	logger := testenv.NewLogger(t)

	conn, err := infra.CloneTestDatabase(t, "ingress_setup_test")
	require.NoError(t, err)

	_, err = orgRepo.New(conn).UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        orgID,
		Slug:        orgID,
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	_, err = customdomainsRepo.New(conn).CreateCustomDomain(ctx, customdomainsRepo.CreateCustomDomainParams{
		OrganizationID:  orgID,
		Domain:          domain,
		ProvisionerKind: "ingress",
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)

	stub := k8s.NewStubProvisioner(k8s.ProvisionerKindIngress, logger)
	act := activities.NewCustomDomainIngress(logger, conn, &stubProvisionerFactory{provisioner: stub}, k8s.ProvisionerKindIngress, activities.WithSetupSleep(0))

	err = act.Do(ctx, activities.CustomDomainIngressArgs{
		OrgID:           orgID,
		Domain:          domain,
		Action:          activities.CustomDomainIngressActionSetup,
		ProvisionerKind: k8s.ProvisionerKindIngress,
	})
	require.NoError(t, err)

	// Setup → Get, in that order
	calls := stub.Calls()
	require.Len(t, calls, 2)
	require.Equal(t, "Setup", calls[0].Method)
	require.Equal(t, domain, calls[0].Domain)
	require.Equal(t, "Get", calls[1].Method)

	row, err := customdomainsRepo.New(conn).GetCustomDomainByDomain(ctx, domain)
	require.NoError(t, err)
	require.True(t, row.Activated)
	require.True(t, row.Verified)
	require.Equal(t, "ingress", row.ProvisionerKind)
	require.True(t, row.IngressName.Valid, "IngressName must be set after setup")
}

func TestCustomDomainIngress_Setup_Gateway_WritesNullCertSecret(t *testing.T) {
	t.Parallel()

	const orgID = "org-gateway-setup"
	const domain = "gateway-setup.example.com"
	ctx := t.Context()
	logger := testenv.NewLogger(t)

	conn, err := infra.CloneTestDatabase(t, "gateway_setup_test")
	require.NoError(t, err)

	_, err = orgRepo.New(conn).UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        orgID,
		Slug:        orgID,
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	_, err = customdomainsRepo.New(conn).CreateCustomDomain(ctx, customdomainsRepo.CreateCustomDomainParams{
		OrganizationID:  orgID,
		Domain:          domain,
		ProvisionerKind: "gateway",
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)

	// GatewayProvisioner returns empty SecretName — stub mirrors this behaviour.
	stub := k8s.NewStubProvisioner(k8s.ProvisionerKindGateway, logger)
	act := activities.NewCustomDomainIngress(logger, conn, &stubProvisionerFactory{provisioner: stub}, k8s.ProvisionerKindGateway, activities.WithSetupSleep(0))

	err = act.Do(ctx, activities.CustomDomainIngressArgs{
		OrgID:           orgID,
		Domain:          domain,
		Action:          activities.CustomDomainIngressActionSetup,
		ProvisionerKind: k8s.ProvisionerKindGateway,
	})
	require.NoError(t, err)

	row, err := customdomainsRepo.New(conn).GetCustomDomainByDomain(ctx, domain)
	require.NoError(t, err)
	require.True(t, row.Activated)
	require.Equal(t, "gateway", row.ProvisionerKind)
	// Gateway owns TLS at the parent level — HTTPRoute never has a cert secret.
	require.False(t, row.CertSecretName.Valid, "CertSecretName must be NULL for gateway kind")
}

func TestCustomDomainIngress_Setup_KindResolution_DefaultsToIngress(t *testing.T) {
	t.Parallel()

	const orgID = "org-default-kind"
	const domain = "default-kind.example.com"
	ctx := t.Context()
	logger := testenv.NewLogger(t)

	conn, err := infra.CloneTestDatabase(t, "default_kind_test")
	require.NoError(t, err)

	_, err = orgRepo.New(conn).UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        orgID,
		Slug:        orgID,
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	_, err = customdomainsRepo.New(conn).CreateCustomDomain(ctx, customdomainsRepo.CreateCustomDomainParams{
		OrganizationID:  orgID,
		Domain:          domain,
		ProvisionerKind: "ingress",
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)

	stub := k8s.NewStubProvisioner(k8s.ProvisionerKindIngress, logger)
	// No default provisioner set and no kind in args — must resolve to ingress.
	act := activities.NewCustomDomainIngress(logger, conn, &stubProvisionerFactory{provisioner: stub}, "", activities.WithSetupSleep(0))

	err = act.Do(ctx, activities.CustomDomainIngressArgs{
		OrgID:  orgID,
		Domain: domain,
		Action: activities.CustomDomainIngressActionSetup,
	})
	require.NoError(t, err)

	row, err := customdomainsRepo.New(conn).GetCustomDomainByDomain(ctx, domain)
	require.NoError(t, err)
	require.Equal(t, "ingress", row.ProvisionerKind)
}

func TestCustomDomainIngress_Setup_WrongOrg_Errors(t *testing.T) {
	t.Parallel()

	const ownerOrg = "org-owner-ingress"
	const domain = "owner-ingress.example.com"
	ctx := t.Context()
	logger := testenv.NewLogger(t)

	conn, err := infra.CloneTestDatabase(t, "wrong_org_ingress_test")
	require.NoError(t, err)

	_, err = orgRepo.New(conn).UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:          ownerOrg,
		Name:        ownerOrg,
		Slug:        ownerOrg,
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	_, err = customdomainsRepo.New(conn).CreateCustomDomain(ctx, customdomainsRepo.CreateCustomDomainParams{
		OrganizationID:  ownerOrg,
		Domain:          domain,
		ProvisionerKind: "ingress",
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)

	stub := k8s.NewStubProvisioner(k8s.ProvisionerKindIngress, logger)
	act := activities.NewCustomDomainIngress(logger, conn, &stubProvisionerFactory{provisioner: stub}, k8s.ProvisionerKindIngress, activities.WithSetupSleep(0))

	err = act.Do(ctx, activities.CustomDomainIngressArgs{
		OrgID:  "org-intruder",
		Domain: domain,
		Action: activities.CustomDomainIngressActionSetup,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "custom domain does not belong to organization")
}
