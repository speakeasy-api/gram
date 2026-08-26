package activities_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
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

type blockingProvisioner struct {
	applyStarted chan struct{}
	releaseApply chan struct{}
	calls        []k8s.StubCall
}

func newBlockingProvisioner() *blockingProvisioner {
	return &blockingProvisioner{
		applyStarted: make(chan struct{}, 1),
		releaseApply: make(chan struct{}),
		calls:        nil,
	}
}

func (p *blockingProvisioner) Kind() k8s.ProvisionerKind {
	return k8s.ProvisionerKindIngress
}

func (p *blockingProvisioner) Apply(ctx context.Context, config k8s.RouteConfig) (k8s.SetupResult, error) {
	p.calls = append(p.calls, k8s.StubCall{
		Method:       "Apply",
		Domain:       config.Domain,
		ResourceName: "",
		SecretName:   "",
		IPAllowlist:  config.IPAllowlist,
		RootTarget:   config.RootTarget,
	})
	p.applyStarted <- struct{}{}
	select {
	case <-ctx.Done():
		return k8s.SetupResult{}, fmt.Errorf("wait for apply release: %w", ctx.Err())
	case <-p.releaseApply:
		return k8s.SetupResult{ResourceName: "race-resource", SecretName: "race-secret"}, nil
	}
}

func (p *blockingProvisioner) Get(_ context.Context, resourceName string) error {
	p.calls = append(p.calls, k8s.StubCall{Method: "Get", ResourceName: resourceName})
	return nil
}

func (p *blockingProvisioner) Delete(_ context.Context, resourceName, secretName string) error {
	p.calls = append(p.calls, k8s.StubCall{Method: "Delete", ResourceName: resourceName, SecretName: secretName})
	return nil
}

func (p *blockingProvisioner) Calls() []k8s.StubCall {
	return p.calls
}

// --- Delete path (no DB required) ---

func TestCustomDomainIngress_Delete_EmptyResourceName_Errors(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	stub := k8s.NewStubProvisioner(k8s.ProvisionerKindIngress, logger)
	act := activities.NewCustomDomainIngress(logger, nil, &stubProvisionerFactory{provisioner: stub})

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
	act := activities.NewCustomDomainIngress(logger, nil, &stubProvisionerFactory{provisioner: stub})

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
	act := activities.NewCustomDomainIngress(logger, nil, &stubProvisionerFactory{provisioner: stub})

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

	setupRow, err := customdomainsRepo.New(conn).CreateCustomDomain(ctx, customdomainsRepo.CreateCustomDomainParams{
		OrganizationID:  orgID,
		Domain:          domain,
		ProvisionerKind: "ingress",
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)
	// Reconcile only activates verified rows; setup runs after the TXT proof.
	_, err = customdomainsRepo.New(conn).SetCustomDomainVerified(ctx, setupRow.ID)
	require.NoError(t, err)

	stub := k8s.NewStubProvisioner(k8s.ProvisionerKindIngress, logger)
	act := activities.NewCustomDomainIngress(logger, conn, &stubProvisionerFactory{provisioner: stub}, activities.WithSetupSleep(0))

	err = act.Do(ctx, activities.CustomDomainIngressArgs{
		OrgID:           orgID,
		Domain:          domain,
		Action:          activities.CustomDomainIngressActionSetup,
		ProvisionerKind: k8s.ProvisionerKindIngress,
	})
	require.NoError(t, err)

	// Apply → Get, in that order
	calls := stub.Calls()
	require.Len(t, calls, 2)
	require.Equal(t, "Apply", calls[0].Method)
	require.Equal(t, domain, calls[0].Domain)
	require.Equal(t, "Get", calls[1].Method)

	row, err := customdomainsRepo.New(conn).GetCustomDomainByDomain(ctx, domain)
	require.NoError(t, err)
	require.True(t, row.Activated)
	require.True(t, row.Verified)
	require.Equal(t, "ingress", row.ProvisionerKind)
	require.True(t, row.IngressName.Valid, "IngressName must be set after setup")
}

func TestCustomDomainIngress_Reapply_AppliesAllowlist(t *testing.T) {
	t.Parallel()

	const orgID = "org-reapply"
	const domain = "reapply.example.com"
	ctx := t.Context()
	logger := testenv.NewLogger(t)

	conn, err := infra.CloneTestDatabase(t, "reapply_test")
	require.NoError(t, err)

	_, err = orgRepo.New(conn).UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        orgID,
		Slug:        orgID,
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	created, err := customdomainsRepo.New(conn).CreateCustomDomain(ctx, customdomainsRepo.CreateCustomDomainParams{
		OrganizationID:  orgID,
		Domain:          domain,
		ProvisionerKind: "ingress",
		IpAllowlist:     []string{"1.2.3.4", "10.0.0.0/8"},
	})
	require.NoError(t, err)
	_, err = customdomainsRepo.New(conn).UpdateCustomDomain(ctx, customdomainsRepo.UpdateCustomDomainParams{
		ID:              created.ID,
		Verified:        true,
		Activated:       true,
		IngressName:     pgtype.Text{String: "reapply-example-com", Valid: true},
		CertSecretName:  pgtype.Text{String: "reapply-example-com-tls", Valid: true},
		ProvisionerKind: "ingress",
	})
	require.NoError(t, err)

	stub := k8s.NewStubProvisioner(k8s.ProvisionerKindIngress, logger)
	act := activities.NewCustomDomainIngress(logger, conn, &stubProvisionerFactory{provisioner: stub}, activities.WithSetupSleep(0))

	err = act.Do(ctx, activities.CustomDomainIngressArgs{
		OrgID:           orgID,
		Domain:          domain,
		Action:          activities.CustomDomainIngressActionReapply,
		ProvisionerKind: k8s.ProvisionerKindIngress,
		IPAllowlist:     []string{"1.2.3.4", "10.0.0.0/8"},
	})
	require.NoError(t, err)

	// Reapply runs a single idempotent Apply with desired state from the DB —
	// no convergence Get and no activation write for an already-active domain.
	calls := stub.Calls()
	require.Len(t, calls, 1)
	require.Equal(t, "Apply", calls[0].Method)
	require.Equal(t, domain, calls[0].Domain)
	require.Equal(t, []string{"1.2.3.4", "10.0.0.0/8"}, calls[0].IPAllowlist)
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
	act := activities.NewCustomDomainIngress(logger, conn, &stubProvisionerFactory{provisioner: stub}, activities.WithSetupSleep(0))

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
	act := activities.NewCustomDomainIngress(logger, conn, &stubProvisionerFactory{provisioner: stub}, activities.WithSetupSleep(0))

	err = act.Do(ctx, activities.CustomDomainIngressArgs{
		OrgID:  "org-intruder",
		Domain: domain,
		Action: activities.CustomDomainIngressActionSetup,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "custom domain does not belong to organization")
}

func TestReconcileCustomDomain_DeleteDuringApplyRemovesAppliedResource(t *testing.T) {
	t.Parallel()

	const orgID = "org-delete-during-apply"
	const domain = "delete-during-apply.example.com"
	ctx := t.Context()
	logger := testenv.NewLogger(t)
	conn, err := infra.CloneTestDatabase(t, "delete_during_apply_test")
	require.NoError(t, err)
	_, err = orgRepo.New(conn).UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        orgID,
		Slug:        orgID,
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)
	customDomain, err := customdomainsRepo.New(conn).CreateCustomDomain(ctx, customdomainsRepo.CreateCustomDomainParams{
		OrganizationID:  orgID,
		Domain:          domain,
		ProvisionerKind: string(k8s.ProvisionerKindIngress),
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)
	// Reconcile only walks the convergence path for verified rows.
	_, err = customdomainsRepo.New(conn).SetCustomDomainVerified(ctx, customDomain.ID)
	require.NoError(t, err)

	provisioner := newBlockingProvisioner()
	reconciler := activities.NewCustomDomainIngress(logger, conn, &stubProvisionerFactory{provisioner: provisioner})
	reconcileResult := make(chan error, 1)
	go func() {
		reconcileResult <- reconciler.ReconcileCustomDomain(ctx, activities.ReconcileCustomDomainArgs{CustomDomainID: customDomain.ID})
	}()

	select {
	case <-provisioner.applyStarted:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "Apply did not start")
	}
	require.NoError(t, customdomainsRepo.New(conn).DeleteCustomDomain(ctx, orgID))
	close(provisioner.releaseApply)
	require.NoError(t, <-reconcileResult)

	calls := provisioner.Calls()
	require.Len(t, calls, 2)
	require.Equal(t, "Apply", calls[0].Method)
	require.Equal(t, "Delete", calls[1].Method)
	require.Equal(t, "race-resource", calls[1].ResourceName)
	require.Equal(t, "race-secret", calls[1].SecretName)

	deleted, err := customdomainsRepo.New(conn).GetCustomDomainRouteConfig(ctx, customDomain.ID)
	require.NoError(t, err)
	require.True(t, deleted.Deleted)
	require.False(t, deleted.IngressName.Valid)
	require.False(t, deleted.CertSecretName.Valid)
}

func TestReconcileCustomDomain_DeleteDuringConvergenceConvergesToDeleted(t *testing.T) {
	t.Parallel()

	const orgID = "org-delete-during-convergence"
	const domain = "delete-during-convergence.example.com"
	ctx := t.Context()
	logger := testenv.NewLogger(t)
	conn, err := infra.CloneTestDatabase(t, "delete_during_convergence_test")
	require.NoError(t, err)
	_, err = orgRepo.New(conn).UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        orgID,
		Slug:        orgID,
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)
	customDomain, err := customdomainsRepo.New(conn).CreateCustomDomain(ctx, customdomainsRepo.CreateCustomDomainParams{
		OrganizationID:  orgID,
		Domain:          domain,
		ProvisionerKind: string(k8s.ProvisionerKindIngress),
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)
	// Reconcile only walks the convergence path for verified rows.
	_, err = customdomainsRepo.New(conn).SetCustomDomainVerified(ctx, customDomain.ID)
	require.NoError(t, err)

	provisioner := newBlockingProvisioner()
	reconciler := activities.NewCustomDomainIngress(
		logger,
		conn,
		&stubProvisionerFactory{provisioner: provisioner},
		activities.WithSetupSleep(time.Second),
	)
	reconcileResult := make(chan error, 1)
	go func() {
		reconcileResult <- reconciler.ReconcileCustomDomain(ctx, activities.ReconcileCustomDomainArgs{CustomDomainID: customDomain.ID})
	}()
	select {
	case <-provisioner.applyStarted:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "Apply did not start")
	}
	close(provisioner.releaseApply)
	require.Eventually(t, func() bool {
		route, routeErr := customdomainsRepo.New(conn).GetCustomDomainRouteConfig(ctx, customDomain.ID)
		return routeErr == nil && route.IngressName.Valid
	}, 5*time.Second, 10*time.Millisecond)

	require.NoError(t, customdomainsRepo.New(conn).DeleteCustomDomain(ctx, orgID))
	require.NoError(t, <-reconcileResult)
	require.NoError(t, reconciler.ReconcileCustomDomain(ctx, activities.ReconcileCustomDomainArgs{CustomDomainID: customDomain.ID}))

	calls := provisioner.Calls()
	require.Len(t, calls, 3)
	require.Equal(t, "Apply", calls[0].Method)
	require.Equal(t, "Get", calls[1].Method)
	require.Equal(t, "Delete", calls[2].Method)
	require.Equal(t, "race-resource", calls[2].ResourceName)

	_, err = customdomainsRepo.New(conn).GetCustomDomainByID(ctx, customDomain.ID)
	require.Error(t, err)
	deleted, err := customdomainsRepo.New(conn).GetCustomDomainRouteConfig(ctx, customDomain.ID)
	require.NoError(t, err)
	require.False(t, deleted.IngressName.Valid)
	require.False(t, deleted.CertSecretName.Valid)
}

func TestReconcileCustomDomain_DeletedWithoutResourcesNoops(t *testing.T) {
	t.Parallel()

	const orgID = "org-delete-unactivated"
	ctx := t.Context()
	logger := testenv.NewLogger(t)
	conn, err := infra.CloneTestDatabase(t, "delete_unactivated_test")
	require.NoError(t, err)
	_, err = orgRepo.New(conn).UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        orgID,
		Slug:        orgID,
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)
	customDomain, err := customdomainsRepo.New(conn).CreateCustomDomain(ctx, customdomainsRepo.CreateCustomDomainParams{
		OrganizationID:  orgID,
		Domain:          "delete-unactivated.example.com",
		ProvisionerKind: string(k8s.ProvisionerKindIngress),
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)
	require.NoError(t, customdomainsRepo.New(conn).DeleteCustomDomain(ctx, orgID))

	provisioner := k8s.NewStubProvisioner(k8s.ProvisionerKindIngress, logger)
	reconciler := activities.NewCustomDomainIngress(logger, conn, &stubProvisionerFactory{provisioner: provisioner})
	require.NoError(t, reconciler.ReconcileCustomDomain(ctx, activities.ReconcileCustomDomainArgs{CustomDomainID: customDomain.ID}))
	require.Empty(t, provisioner.Calls())
}

func TestReconcileCustomDomain_DeletedRetriesDeleteIdempotently(t *testing.T) {
	t.Parallel()

	const orgID = "org-delete-retry"
	ctx := t.Context()
	logger := testenv.NewLogger(t)
	conn, err := infra.CloneTestDatabase(t, "delete_retry_test")
	require.NoError(t, err)
	_, err = orgRepo.New(conn).UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        orgID,
		Slug:        orgID,
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)
	customDomain, err := customdomainsRepo.New(conn).CreateCustomDomain(ctx, customdomainsRepo.CreateCustomDomainParams{
		OrganizationID:  orgID,
		Domain:          "delete-retry.example.com",
		IngressName:     pgtype.Text{String: "retry-resource", Valid: true},
		CertSecretName:  pgtype.Text{String: "retry-secret", Valid: true},
		ProvisionerKind: string(k8s.ProvisionerKindIngress),
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)
	require.NoError(t, customdomainsRepo.New(conn).DeleteCustomDomain(ctx, orgID))

	provisioner := k8s.NewStubProvisioner(k8s.ProvisionerKindIngress, logger)
	reconciler := activities.NewCustomDomainIngress(logger, conn, &stubProvisionerFactory{provisioner: provisioner})
	for range 2 {
		require.NoError(t, reconciler.ReconcileCustomDomain(ctx, activities.ReconcileCustomDomainArgs{CustomDomainID: customDomain.ID}))
	}
	calls := provisioner.Calls()
	require.Len(t, calls, 1)
	require.Equal(t, "Delete", calls[0].Method)
	require.Equal(t, "retry-resource", calls[0].ResourceName)
	require.Equal(t, "retry-secret", calls[0].SecretName)

	deleted, err := customdomainsRepo.New(conn).GetCustomDomainRouteConfig(ctx, customDomain.ID)
	require.NoError(t, err)
	require.False(t, deleted.IngressName.Valid)
	require.False(t, deleted.CertSecretName.Valid)
}

func TestReconcileCustomDomain_NotFoundNoops(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	conn, err := infra.CloneTestDatabase(t, "reconcile_not_found_test")
	require.NoError(t, err)
	provisioner := k8s.NewStubProvisioner(k8s.ProvisionerKindIngress, logger)
	reconciler := activities.NewCustomDomainIngress(logger, conn, &stubProvisionerFactory{provisioner: provisioner})
	require.NoError(t, reconciler.ReconcileCustomDomain(t.Context(), activities.ReconcileCustomDomainArgs{CustomDomainID: uuid.New()}))
	require.Empty(t, provisioner.Calls())
}

func TestReconcileCustomDomain_PendingDomainAppliesButNeverActivates(t *testing.T) {
	t.Parallel()

	const orgID = "org-pending-no-activate"
	const domain = "pending-no-activate.example.com"
	ctx := t.Context()
	logger := testenv.NewLogger(t)

	conn, err := infra.CloneTestDatabase(t, "pending_no_activate_test")
	require.NoError(t, err)
	_, err = orgRepo.New(conn).UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        orgID,
		Slug:        orgID,
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)
	customDomain, err := customdomainsRepo.New(conn).CreateCustomDomain(ctx, customdomainsRepo.CreateCustomDomainParams{
		OrganizationID:  orgID,
		Domain:          domain,
		IngressName:     pgtype.Text{},
		CertSecretName:  pgtype.Text{},
		ProvisionerKind: string(k8s.ProvisionerKindIngress),
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)

	stub := k8s.NewStubProvisioner(k8s.ProvisionerKindIngress, logger)
	reconciler := activities.NewCustomDomainIngress(logger, conn, &stubProvisionerFactory{provisioner: stub}, activities.WithSetupSleep(0))

	// Desired-state writes on a pending domain (root MCP selection, allowlist
	// edits) trigger reconciliation. Resources may pre-provision, but the
	// domain must never verify or activate without the TXT ownership proof.
	require.NoError(t, reconciler.ReconcileCustomDomain(ctx, activities.ReconcileCustomDomainArgs{CustomDomainID: customDomain.ID}))

	calls := stub.Calls()
	require.Len(t, calls, 1)
	require.Equal(t, "Apply", calls[0].Method)

	row, err := customdomainsRepo.New(conn).GetCustomDomainByDomain(ctx, domain)
	require.NoError(t, err)
	require.False(t, row.Verified, "reconciliation must never set verified")
	require.False(t, row.Activated, "an unverified domain must not activate")
}
