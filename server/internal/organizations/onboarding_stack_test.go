package organizations_test

import (
	"encoding/json"
	"testing"

	admingen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/organizations"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestOnboardingStackOptionsComeFromTheSupportMatrix(t *testing.T) {
	t.Parallel()

	options, err := organizations.LoadOnboardingStackOptions()
	require.NoError(t, err)

	vendors := make(map[string]*admingen.AdminOnboardingVendorOption, len(options.Vendors))
	order := make([]string, 0, len(options.Vendors))
	for _, vendor := range options.Vendors {
		vendors[vendor.Vendor] = vendor
		order = append(order, vendor.Vendor)
	}
	require.Equal(t, []string{"Anthropic", "OpenAI", "Cursor", "OpenCode", "Google", "OpenClaw", "GitHub"}, order, "catalog order, and no Others bucket")
	require.Len(t, vendors["Anthropic"].Plans, 2, "organizational plans only")
	require.Equal(t, "anthropic-enterprise", vendors["Anthropic"].Plans[1].Slug)
	require.Len(t, vendors["Google"].Plans, 2)
	require.Len(t, vendors["GitHub"].Plans, 2)
	require.Empty(t, vendors["OpenCode"].Plans)
	require.Len(t, vendors["Anthropic"].Platforms, 12)
	require.Equal(t, "Claude Code", vendors["Anthropic"].Platforms[0].Family)

	mdm := make([]string, 0, len(options.MdmVendors))
	for _, vendor := range options.MdmVendors {
		mdm = append(mdm, vendor.Slug)
	}
	require.Equal(t, []string{"jamf", "intune", "iru", "other"}, mdm)
}

func TestSaveOnboardingStackRecordsVendorsPlansAndDeviceManagement(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationOnboardingStackUpdated)
	require.NoError(t, err)

	empty, err := organizations.LoadOnboardingStack(ctx, ti.conn, ac.ActiveOrganizationID)
	require.NoError(t, err)
	require.Empty(t, empty.Vendors)
	require.Nil(t, empty.MdmVendor)

	actor := urn.NewPrincipal(urn.PrincipalTypeUser, "staff-test")
	saved, err := organizations.SaveOnboardingStack(ctx, ti.conn, audit.NewLogger(), ac.ActiveOrganizationID, organizations.OnboardingStackInput{
		Vendors: []organizations.OnboardingStackVendorInput{
			{Vendor: "OpenCode", PlanSlug: nil},
			{Vendor: "Anthropic", PlanSlug: conv.PtrEmpty("anthropic-enterprise")},
		},
		MdmVendor:     "other",
		MdmVendorName: conv.PtrEmpty("  Fleet  "),
	}, actor, nil)
	require.NoError(t, err)
	require.Equal(t, "other", *saved.MdmVendor)
	require.Equal(t, "Fleet", *saved.MdmVendorName, "the name is trimmed")
	require.Len(t, saved.Vendors, 2)
	require.Equal(t, "Anthropic", saved.Vendors[0].Vendor)
	require.Equal(t, "anthropic-enterprise", *saved.Vendors[0].PlanSlug)
	require.Equal(t, "OpenCode", saved.Vendors[1].Vendor)
	require.Nil(t, saved.Vendors[1].PlanSlug)

	loaded, err := organizations.LoadOnboardingStack(ctx, ti.conn, ac.ActiveOrganizationID)
	require.NoError(t, err)
	require.Equal(t, saved, loaded)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationOnboardingStackUpdated)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
	entry, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionOrganizationOnboardingStackUpdated)
	require.NoError(t, err)
	var snapshot audit.OrganizationOnboardingStackSnapshot
	require.NoError(t, json.Unmarshal(entry.AfterSnapshot, &snapshot))
	require.Len(t, snapshot.Vendors, 2)
	require.Equal(t, "other", *snapshot.MdmVendor)

	// Saving again replaces the vendors and clears the name with the vendor.
	saved, err = organizations.SaveOnboardingStack(ctx, ti.conn, audit.NewLogger(), ac.ActiveOrganizationID, organizations.OnboardingStackInput{
		Vendors:       []organizations.OnboardingStackVendorInput{{Vendor: "Cursor", PlanSlug: conv.PtrEmpty("cursor-teams")}},
		MdmVendor:     "none",
		MdmVendorName: conv.PtrEmpty("stale"),
	}, actor, nil)
	require.NoError(t, err)
	require.Len(t, saved.Vendors, 1)
	require.Equal(t, "Cursor", saved.Vendors[0].Vendor)
	require.Equal(t, "none", *saved.MdmVendor)
	require.Nil(t, saved.MdmVendorName)
}

func TestSaveOnboardingStackRejectsWhatTheCatalogDoesNotOffer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	actor := urn.NewPrincipal(urn.PrincipalTypeUser, "staff-test")

	anthropic := func(plan *string) organizations.OnboardingStackVendorInput {
		return organizations.OnboardingStackVendorInput{Vendor: "Anthropic", PlanSlug: plan}
	}
	for name, input := range map[string]organizations.OnboardingStackInput{
		"unknown vendor":             {Vendors: []organizations.OnboardingStackVendorInput{{Vendor: "Others", PlanSlug: nil}}, MdmVendor: "none", MdmVendorName: nil},
		"duplicate vendor":           {Vendors: []organizations.OnboardingStackVendorInput{anthropic(conv.PtrEmpty("anthropic-team")), anthropic(conv.PtrEmpty("anthropic-enterprise"))}, MdmVendor: "none", MdmVendorName: nil},
		"plan of another vendor":     {Vendors: []organizations.OnboardingStackVendorInput{anthropic(conv.PtrEmpty("cursor-teams"))}, MdmVendor: "none", MdmVendorName: nil},
		"missing plan":               {Vendors: []organizations.OnboardingStackVendorInput{anthropic(nil)}, MdmVendor: "none", MdmVendorName: nil},
		"plan for a planless vendor": {Vendors: []organizations.OnboardingStackVendorInput{{Vendor: "OpenCode", PlanSlug: conv.PtrEmpty("anthropic-team")}}, MdmVendor: "none", MdmVendorName: nil},
		"unknown device management":  {Vendors: nil, MdmVendor: "airwatch", MdmVendorName: nil},
		"other without a name":       {Vendors: nil, MdmVendor: "other", MdmVendorName: conv.PtrEmpty("   ")},
	} {
		result, err := organizations.SaveOnboardingStack(ctx, ti.conn, audit.NewLogger(), ac.ActiveOrganizationID, input, actor, nil)
		require.Nil(t, result, name)
		requireOopsCode(t, err, oops.CodeBadRequest)
	}

	loaded, err := organizations.LoadOnboardingStack(ctx, ti.conn, ac.ActiveOrganizationID)
	require.NoError(t, err)
	require.Empty(t, loaded.Vendors, "a rejected save writes nothing")
	require.Nil(t, loaded.MdmVendor)
}

func TestLoadOnboardingStackUnknownOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	_, err := organizations.LoadOnboardingStack(ctx, ti.conn, "org_does_not_exist")
	requireOopsCode(t, err, oops.CodeNotFound)
}
