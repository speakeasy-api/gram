package mv

import (
	gen "github.com/speakeasy-api/gram/server/gen/workload_identities"
	"github.com/speakeasy-api/gram/server/internal/workloadpolicy/catalog"
)

// BuildWorkloadPlatformCatalogView renders the catalog's platforms, in the
// order the catalog returns them.
func BuildWorkloadPlatformCatalogView(platforms []catalog.Platform) *gen.WorkloadPlatformCatalog {
	views := make([]*gen.WorkloadPlatform, 0, len(platforms))
	for _, platform := range platforms {
		views = append(views, buildWorkloadPlatformView(platform))
	}
	return &gen.WorkloadPlatformCatalog{Platforms: views}
}

func buildWorkloadPlatformView(p catalog.Platform) *gen.WorkloadPlatform {
	variables := make([]*gen.WorkloadPlatformVariable, 0, len(p.Variables))
	for _, v := range p.Variables {
		variables = append(variables, &gen.WorkloadPlatformVariable{
			Key:            v.Key,
			Tier:           string(v.Tier),
			Label:          v.Label,
			Help:           v.Help,
			Placeholder:    v.Placeholder,
			Pattern:        v.Pattern,
			PatternMessage: v.PatternMessage,
		})
	}

	return &gen.WorkloadPlatform{
		Key:         p.Key,
		DisplayName: p.DisplayName,
		Description: p.Description,
		Icon:        p.Icon,
		Enabled:     p.Enabled,
		Issuer:      buildWorkloadPlatformConstantView(p.Issuer),
		JwksURI:     buildWorkloadPlatformConstantView(p.JWKSURI),
		Variables:   variables,
		Subject: &gen.WorkloadPlatformSubject{
			Template: p.Subject.Template,
			Wildcard: p.Subject.Wildcard,
		},
	}
}

func buildWorkloadPlatformConstantView(c catalog.Constant) *gen.WorkloadPlatformConstant {
	return &gen.WorkloadPlatformConstant{
		Value:      c.Value,
		Visibility: string(c.Visibility),
	}
}
