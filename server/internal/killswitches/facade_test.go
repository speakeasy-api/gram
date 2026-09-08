//nolint:paralleltest // Integration fixtures share an isolated cloned database per test.
package killswitches

import (
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestFacadeActivateCoversCreateAndReactivationWithTypedOutcomes(t *testing.T) {
	conn, orgID := newLifecycleDatabase(t, "killswitch_facade_activate")
	lifecycle := newLifecycleServiceForTest(t, conn, func(OrganizationID, PrincipalKey) bool { return true }, func(OrganizationID, ResourceKey) bool { return true }, nil)
	facade, err := NewFacade(lifecycle)
	require.NoError(t, err)

	create := testActivateRequest(orgID, uuid.New())
	created, err := facade.ActivatePrescription(t.Context(), ActivatePrescriptionInput{
		MutationContext: create.MutationContext, Definition: create.Definition, PrincipalKind: create.PrincipalKind,
		PrincipalInput: create.PrincipalInput, ResourceKind: create.ResourceKind, Desired: create.Desired,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), created.Version)

	deactivated, err := facade.DeactivatePrescription(t.Context(), DeactivatePrescriptionRequest{
		MutationContext: testMutationContext(orgID, uuid.New()), PrescriptionID: created.PrescriptionID, ExpectedVersion: created.Version,
	})
	require.NoError(t, err)

	expected := deactivated.Version
	reactivate := ActivatePrescriptionInput{
		MutationContext: testMutationContext(orgID, uuid.New()), PrescriptionID: &created.PrescriptionID,
		ExpectedVersion: &expected, Desired: testDesired([]string{"tool:b"}),
	}
	reactivated, err := facade.ActivatePrescription(t.Context(), reactivate)
	require.NoError(t, err)
	require.Equal(t, int64(3), reactivated.Version)

	replayed, err := facade.ActivatePrescription(t.Context(), reactivate)
	require.NoError(t, err)
	require.True(t, replayed.Replayed)

	conflicting := reactivate
	conflicting.Desired.ExternalNote = "different"
	_, err = facade.ActivatePrescription(t.Context(), conflicting)
	require.ErrorIs(t, err, ErrOperationConflict)

	stale := reactivate
	stale.OperationID = uuid.New()
	_, err = facade.ActivatePrescription(t.Context(), stale)
	require.ErrorIs(t, err, ErrVersionConflict)
	var versionConflict *VersionConflictError
	require.ErrorAs(t, err, &versionConflict)
	require.Equal(t, reactivated.Version, versionConflict.Actual)
}

func TestFacadeCurrentReadsAreBoundedDeterministicAndTenantQualified(t *testing.T) {
	conn, orgID := newLifecycleDatabase(t, "killswitch_facade_reads")
	otherOrgID := "org_" + uuid.NewString()
	insertOrganization(t, conn, otherOrgID)
	lifecycle := newLifecycleServiceForTest(t, conn, func(OrganizationID, PrincipalKey) bool { return true }, func(OrganizationID, ResourceKey) bool { return true }, nil)
	facade, err := NewFacade(lifecycle)
	require.NoError(t, err)

	activate := func(organizationID, principal string, resources []string, internalNote, externalNote string) MutationResult {
		request := testActivateRequest(organizationID, uuid.New())
		request.PrincipalInput = principal
		request.Desired = testDesired(resources)
		request.Desired.InternalNote = internalNote
		request.Desired.ExternalNote = externalNote
		result, err := facade.ActivatePrescription(t.Context(), ActivatePrescriptionInput{
			MutationContext: request.MutationContext, Definition: request.Definition, PrincipalKind: request.PrincipalKind,
			PrincipalInput: request.PrincipalInput, ResourceKind: request.ResourceKind, Desired: request.Desired,
		})
		require.NoError(t, err)
		return result
	}

	first := activate(orgID, "user:first", []string{"tool:b", "tool:a"}, "internal first", "external first")
	second := activate(orgID, "user:second", []string{"tool:c"}, "internal second", "external second")
	foreign := activate(otherOrgID, "user:foreign", []string{"tool:x"}, "internal foreign", "external foreign")

	got, err := facade.GetPrescription(t.Context(), GetPrescriptionRequest{OrganizationID: OrganizationID(orgID), PrescriptionID: first.PrescriptionID})
	require.NoError(t, err)
	require.Equal(t, "internal first", got.InternalNote)
	require.Equal(t, "external first", got.ExternalNote)
	require.Equal(t, []ResourceKey{ResourceKey(orgID + ":tool:a"), ResourceKey(orgID + ":tool:b")}, got.SelectedResourceKeys)

	_, err = facade.GetPrescription(t.Context(), GetPrescriptionRequest{OrganizationID: OrganizationID(orgID), PrescriptionID: foreign.PrescriptionID})
	require.ErrorIs(t, err, ErrPrescriptionNotFound)
	_, err = facade.GetPrescription(t.Context(), GetPrescriptionRequest{OrganizationID: OrganizationID(otherOrgID), PrescriptionID: first.PrescriptionID})
	require.ErrorIs(t, err, ErrPrescriptionNotFound)

	listed, err := facade.ListPrescriptions(t.Context(), ListPrescriptionsRequest{OrganizationID: OrganizationID(orgID)})
	require.NoError(t, err)
	require.Len(t, listed.Prescriptions, 2)
	require.Nil(t, listed.NextAfterID)
	ids := []PrescriptionID{first.PrescriptionID, second.PrescriptionID}
	slices.Sort(ids)
	require.Equal(t, ids[0], listed.Prescriptions[0].ID)
	require.Equal(t, ids[1], listed.Prescriptions[1].ID)

	limited, err := facade.ListPrescriptions(t.Context(), ListPrescriptionsRequest{OrganizationID: OrganizationID(orgID), Limit: 1})
	require.NoError(t, err)
	require.Len(t, limited.Prescriptions, 1)
	require.Equal(t, listed.Prescriptions[0].ID, limited.Prescriptions[0].ID)
	require.Equal(t, listed.Prescriptions[0].ID, *limited.NextAfterID)

	next, err := facade.ListPrescriptions(t.Context(), ListPrescriptionsRequest{OrganizationID: OrganizationID(orgID), Limit: 1, AfterID: limited.NextAfterID})
	require.NoError(t, err)
	require.Equal(t, []CurrentPrescription{listed.Prescriptions[1]}, next.Prescriptions)
	require.Nil(t, next.NextAfterID)

	_, err = facade.ListPrescriptions(t.Context(), ListPrescriptionsRequest{OrganizationID: OrganizationID(orgID), AfterID: &foreign.PrescriptionID})
	require.ErrorIs(t, err, ErrPrescriptionNotFound)
	_, err = facade.ListPrescriptions(t.Context(), ListPrescriptionsRequest{OrganizationID: OrganizationID(orgID), Limit: MaxListPrescriptions + 1})
	require.ErrorIs(t, err, ErrInvalidArgument)
}

func TestFacadeListsDefinitionsWithoutEvaluatorDependency(t *testing.T) {
	conn, _ := newLifecycleDatabase(t, "killswitch_facade_definitions")
	lifecycle := newLifecycleServiceForTest(t, conn, func(OrganizationID, PrincipalKey) bool { return true }, func(OrganizationID, ResourceKey) bool { return true }, nil)
	facade, err := NewFacade(lifecycle)
	require.NoError(t, err)

	definitions, err := facade.ListDefinitions(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, definitions)
	for i := 1; i < len(definitions); i++ {
		require.Less(t, definitions[i-1].Key, definitions[i].Key)
	}
}
