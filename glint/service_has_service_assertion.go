package glint

import (
	"golang.org/x/tools/go/analysis"
)

const (
	serviceHasServiceAssertionAnalyzer = "servicehasserviceassertion"
	serviceHasServiceAssertionDoc      = "checks that services embedding annotations.Service declare a compile-time assertion that they implement the service interface"
)

type serviceHasServiceAssertionSettings struct {
	Disabled bool `json:"disabled"`
}

func newServiceHasServiceAssertionAnalyzer(rule serviceHasServiceAssertionSettings) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: serviceHasServiceAssertionAnalyzer,
		Doc:  serviceHasServiceAssertionDoc,
		Run: func(pass *analysis.Pass) (any, error) {
			annotated := findAnnotatedStructs(pass)
			if len(annotated) == 0 {
				return nil, nil
			}

			assertions := collectInterfaceAssertions(pass)
			for _, s := range annotated {
				if !hasAssertion(assertions, s.implOf, s.obj) {
					pass.ReportRangef(s.typeSpec, "%s embeds annotations.Service[%s, ...] but package is missing: var _ %s = (*%s)(nil)",
						s.name, s.implOf, s.implOf, s.name)
				}
			}

			return nil, nil
		},
	}
}
