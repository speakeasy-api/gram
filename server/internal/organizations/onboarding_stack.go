package organizations

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	admingen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/supportmatrix"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// mdmVendor is one device-management choice the stack form offers.
type mdmVendor struct {
	Slug string
	Name string
}

const (
	// mdmVendorNone records that the organization has no device management.
	mdmVendorNone = "none"
	// mdmVendorOther records software the form does not list; its name is
	// kept beside it.
	mdmVendorOther = "other"
	// mdmVendorNameMaxLength bounds the free-text name for other.
	mdmVendorNameMaxLength = 200
)

// mdmVendors lists the software the form offers, ending with other. none is
// the answer to "do you use device management?" and is not listed.
var mdmVendors = []mdmVendor{
	{Slug: "jamf", Name: "Jamf Pro"},
	{Slug: "intune", Name: "Microsoft Intune"},
	{Slug: "iru", Name: "Iru"},
	{Slug: mdmVendorOther, Name: "Other"},
}

// OnboardingStackVendorInput is one vendor of an organization's stack.
type OnboardingStackVendorInput struct {
	Vendor string
	// PlanSlug is the support matrix plan the organization is on with the
	// vendor, or nil for a vendor that sells no plans.
	PlanSlug *string
}

// OnboardingStackInput is everything staff record about an organization's
// stack. Products are never recorded: a vendor implies all of them.
type OnboardingStackInput struct {
	Vendors []OnboardingStackVendorInput
	// MdmVendor is jamf, intune, iru, other or none.
	MdmVendor string
	// MdmVendorName names the software when MdmVendor is other.
	MdmVendorName *string
}

// LoadOnboardingStackOptions reads what the stack form offers from the
// support matrix in code: each vendor with its plans and platforms, in
// matrix order.
func LoadOnboardingStackOptions() (*admingen.AdminOnboardingStackOptions, error) {
	matrix, err := supportmatrix.Current()
	if err != nil {
		return nil, fmt.Errorf("load support matrix: %w", err)
	}
	options := &admingen.AdminOnboardingStackOptions{Vendors: []*admingen.AdminOnboardingVendorOption{}, MdmVendors: nil}
	byVendor := make(map[string]*admingen.AdminOnboardingVendorOption, len(matrix.Platforms))
	for _, platform := range matrix.Platforms {
		option, ok := byVendor[platform.Vendor]
		if !ok {
			option = &admingen.AdminOnboardingVendorOption{Vendor: platform.Vendor, Plans: []*admingen.AdminOnboardingPlan{}, Platforms: nil}
			byVendor[platform.Vendor] = option
			options.Vendors = append(options.Vendors, option)
		}
		option.Platforms = append(option.Platforms, &admingen.AdminOnboardingPlatform{Slug: platform.ID, Name: platform.Name, Family: platform.Family, Surface: platform.Surface})
	}
	for _, plan := range matrix.Plans {
		// The matrix refuses a plan whose vendor sells no platform.
		if option, ok := byVendor[plan.Vendor]; ok {
			option.Plans = append(option.Plans, &admingen.AdminOnboardingPlan{Slug: plan.ID, Name: plan.Name})
		}
	}
	options.MdmVendors = make([]*admingen.AdminMdmVendorOption, 0, len(mdmVendors))
	for _, vendor := range mdmVendors {
		options.MdmVendors = append(options.MdmVendors, &admingen.AdminMdmVendorOption{Slug: vendor.Slug, Name: vendor.Name})
	}
	return options, nil
}

// LoadOnboardingStack reads the stack staff recorded for an organization.
// The caller must authorize access to the explicit organization.
func LoadOnboardingStack(ctx context.Context, db repo.DBTX, organizationID string) (*admingen.AdminOnboardingStack, error) {
	queries := repo.New(db)
	row, err := queries.GetOrganizationOnboardingStack(ctx, organizationID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, fmt.Errorf("load onboarding stack: %w", err)
	}
	vendors, err := queries.ListOrganizationOnboardingVendors(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list onboarding stack vendors: %w", err)
	}
	stack := &admingen.AdminOnboardingStack{
		OrganizationID: organizationID,
		Vendors:        make([]*admingen.AdminOnboardingStackVendor, 0, len(vendors)),
		MdmVendor:      conv.FromPGText[string](row.MdmVendor),
		MdmVendorName:  conv.FromPGText[string](row.MdmVendorName),
	}
	for _, vendor := range vendors {
		planSlug := conv.FromPGText[string](vendor.PlanSlug)
		if planSlug == nil {
			planSlug = conv.FromPGText[string](vendor.LegacyPlanSlug)
		}
		stack.Vendors = append(stack.Vendors, &admingen.AdminOnboardingStackVendor{Vendor: vendor.Vendor, PlanSlug: planSlug})
	}
	return stack, nil
}

// SaveOnboardingStack replaces the recorded stack after checking it against
// the support matrix catalog. Its caller authenticates staff.
func SaveOnboardingStack(ctx context.Context, db *pgxpool.Pool, logger *audit.Logger, organizationID string, input OnboardingStackInput, actor urn.Principal, displayName *string) (*admingen.AdminOnboardingStack, error) {
	options, err := LoadOnboardingStackOptions()
	if err != nil {
		return nil, err
	}
	if err := validateOnboardingStack(input, options); err != nil {
		return nil, err
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin onboarding stack: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	queries := repo.New(tx)
	org, err := queries.LockOrganizationForSetupTaskUpdate(ctx, organizationID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, fmt.Errorf("lock onboarding stack organization: %w", err)
	}
	before, err := LoadOnboardingStack(ctx, tx, organizationID)
	if err != nil {
		return nil, err
	}
	var name *string
	if input.MdmVendor == mdmVendorOther {
		trimmed := strings.TrimSpace(conv.PtrValOr(input.MdmVendorName, ""))
		name = &trimmed
	}
	if err := queries.UpsertOrganizationOnboardingStack(ctx, repo.UpsertOrganizationOnboardingStackParams{OrganizationID: organizationID, MdmVendor: conv.ToPGText(input.MdmVendor), MdmVendorName: conv.PtrToPGText(name)}); err != nil {
		return nil, fmt.Errorf("save onboarding stack: %w", err)
	}
	if err := queries.DeleteOrganizationOnboardingVendors(ctx, organizationID); err != nil {
		return nil, fmt.Errorf("clear onboarding stack vendors: %w", err)
	}
	for _, vendor := range input.Vendors {
		if err := queries.InsertOrganizationOnboardingVendor(ctx, repo.InsertOrganizationOnboardingVendorParams{OrganizationID: organizationID, Vendor: vendor.Vendor, PlanSlug: conv.PtrToPGText(vendor.PlanSlug)}); err != nil {
			return nil, fmt.Errorf("save onboarding stack vendor %q: %w", vendor.Vendor, err)
		}
	}
	after, err := LoadOnboardingStack(ctx, tx, organizationID)
	if err != nil {
		return nil, err
	}
	if err := logger.LogOrganizationOnboardingStackUpdated(ctx, tx, audit.LogOrganizationOnboardingStackUpdatedEvent{
		OrganizationID: organizationID, Actor: actor, ActorDisplayName: displayName,
		OrganizationName: org.Name, OrganizationSlug: org.Slug,
		StackSnapshotBefore: onboardingStackSnapshot(before), StackSnapshotAfter: onboardingStackSnapshot(after),
	}); err != nil {
		return nil, fmt.Errorf("audit onboarding stack: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit onboarding stack: %w", err)
	}
	return after, nil
}

// validateOnboardingStack rejects anything the catalog does not offer: an
// unknown vendor, a duplicate, a plan of another vendor, a missing plan for a
// vendor that sells them, or other without a name.
func validateOnboardingStack(input OnboardingStackInput, options *admingen.AdminOnboardingStackOptions) error {
	seen := make(map[string]bool, len(input.Vendors))
	for _, vendor := range input.Vendors {
		if seen[vendor.Vendor] {
			return oops.E(oops.CodeBadRequest, nil, "vendor %q is listed twice", vendor.Vendor)
		}
		seen[vendor.Vendor] = true
		index := slices.IndexFunc(options.Vendors, func(option *admingen.AdminOnboardingVendorOption) bool { return option.Vendor == vendor.Vendor })
		if index < 0 {
			return oops.E(oops.CodeBadRequest, nil, "vendor %q is not in the support matrix", vendor.Vendor)
		}
		option := options.Vendors[index]
		switch {
		case len(option.Plans) == 0 && vendor.PlanSlug != nil:
			return oops.E(oops.CodeBadRequest, nil, "vendor %q sells no plans", vendor.Vendor)
		case len(option.Plans) > 0 && vendor.PlanSlug == nil:
			return oops.E(oops.CodeBadRequest, nil, "vendor %q needs a plan", vendor.Vendor)
		case vendor.PlanSlug != nil && !slices.ContainsFunc(option.Plans, func(plan *admingen.AdminOnboardingPlan) bool { return plan.Slug == *vendor.PlanSlug }):
			return oops.E(oops.CodeBadRequest, nil, "plan %q does not belong to %q", *vendor.PlanSlug, vendor.Vendor)
		}
	}
	if input.MdmVendor != mdmVendorNone && !slices.ContainsFunc(mdmVendors, func(vendor mdmVendor) bool { return vendor.Slug == input.MdmVendor }) {
		return oops.E(oops.CodeBadRequest, nil, "unknown device management vendor %q", input.MdmVendor)
	}
	if input.MdmVendor == mdmVendorOther {
		name := strings.TrimSpace(conv.PtrValOr(input.MdmVendorName, ""))
		if name == "" {
			return oops.E(oops.CodeBadRequest, nil, "name the device management software when choosing other")
		}
		if utf8.RuneCountInString(name) > mdmVendorNameMaxLength {
			return oops.E(oops.CodeBadRequest, nil, "device management software name must be at most %d characters", mdmVendorNameMaxLength)
		}
	}
	return nil
}

func onboardingStackSnapshot(stack *admingen.AdminOnboardingStack) *audit.OrganizationOnboardingStackSnapshot {
	snapshot := &audit.OrganizationOnboardingStackSnapshot{
		Vendors:       make([]audit.OrganizationOnboardingStackVendorSnapshot, 0, len(stack.Vendors)),
		MdmVendor:     stack.MdmVendor,
		MdmVendorName: stack.MdmVendorName,
	}
	for _, vendor := range stack.Vendors {
		snapshot.Vendors = append(snapshot.Vendors, audit.OrganizationOnboardingStackVendorSnapshot{Vendor: vendor.Vendor, Plan: vendor.PlanSlug})
	}
	return snapshot
}
