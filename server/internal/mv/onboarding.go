package mv

import (
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/onboarding"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/onboarding/repo"
)

// BuildOnboardingProviderView converts a reference provider row, the plans it
// sells and the products it makes into the API shape.
func BuildOnboardingProviderView(provider repo.OnboardingProvider, plans []repo.ListOnboardingPlansRow, products []repo.ListOnboardingProductsRow) *gen.OnboardingProvider {
	view := &gen.OnboardingProvider{
		Slug:     provider.Slug,
		Name:     provider.Name,
		Plans:    make([]*gen.OnboardingPlan, 0, len(plans)),
		Products: make([]*gen.OnboardingProduct, 0, len(products)),
	}
	for _, plan := range plans {
		view.Plans = append(view.Plans, &gen.OnboardingPlan{Slug: plan.Slug, Name: plan.Name})
	}
	for _, product := range products {
		view.Products = append(view.Products, &gen.OnboardingProduct{
			Slug:      product.Slug,
			Name:      product.Name,
			SourceIds: append([]string{}, product.SourceIds...),
		})
	}
	return view
}

// BuildOnboardingAnswersView converts the stored answers row, the selected
// providers and the selected products into the API shape.
func BuildOnboardingAnswersView(answers repo.OrganizationOnboardingAnswer, providers []repo.ListOnboardingSelectedProvidersRow, products []repo.ListOnboardingSelectedProductsRow) *gen.OnboardingAnswers {
	view := &gen.OnboardingAnswers{
		Providers:    make([]*gen.OnboardingSelectedProvider, 0, len(providers)),
		ProductSlugs: make([]string, 0, len(products)),
		MdmVendor:    conv.FromPGTextOrEmpty[string](answers.MdmVendor),
		UseCase:      conv.FromPGText[string](answers.UseCase),
		CompletedAt:  nil,
		UpdatedAt:    answers.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
	if answers.CompletedAt.Valid {
		view.CompletedAt = conv.PtrEmpty(answers.CompletedAt.Time.UTC().Format(time.RFC3339))
	}
	for _, sel := range providers {
		view.Providers = append(view.Providers, &gen.OnboardingSelectedProvider{
			ProviderSlug: sel.ProviderSlug,
			PlanSlug:     conv.FromPGText[string](sel.PlanSlug),
		})
	}
	for _, sel := range products {
		view.ProductSlugs = append(view.ProductSlugs, sel.ProductSlug)
	}
	return view
}
