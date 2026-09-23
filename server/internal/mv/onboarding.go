package mv

import (
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/onboarding"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/onboarding/repo"
)

// BuildOnboardingProductView converts a reference product row and the plans
// linked to it into the API shape.
func BuildOnboardingProductView(product repo.OnboardingProduct, plans []repo.OnboardingPlan) *gen.OnboardingProduct {
	view := &gen.OnboardingProduct{
		Slug:      product.Slug,
		Name:      product.Name,
		Vendor:    product.Vendor,
		SourceIds: append([]string{}, product.SourceIds...),
		Plans:     make([]*gen.OnboardingPlan, 0, len(plans)),
	}
	for _, plan := range plans {
		view.Plans = append(view.Plans, &gen.OnboardingPlan{Slug: plan.Slug, Vendor: plan.Vendor, Name: plan.Name})
	}
	return view
}

// BuildOnboardingAnswersView converts the stored answers row and the selected
// products into the API shape.
func BuildOnboardingAnswersView(answers repo.OrganizationOnboardingAnswer, selected []repo.ListOnboardingSelectedProductsRow) *gen.OnboardingAnswers {
	view := &gen.OnboardingAnswers{
		Products:    make([]*gen.OnboardingSelectedProduct, 0, len(selected)),
		MdmVendor:   conv.FromPGTextOrEmpty[string](answers.MdmVendor),
		UseCase:     conv.FromPGTextOrEmpty[string](answers.UseCase),
		CompletedAt: nil,
		UpdatedAt:   answers.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
	if answers.CompletedAt.Valid {
		view.CompletedAt = conv.PtrEmpty(answers.CompletedAt.Time.UTC().Format(time.RFC3339))
	}
	for _, sel := range selected {
		view.Products = append(view.Products, &gen.OnboardingSelectedProduct{
			ProductSlug: sel.ProductSlug,
			PlanSlug:    conv.FromPGText[string](sel.PlanSlug),
		})
	}
	return view
}
