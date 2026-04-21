package glint

import (
	"golang.org/x/tools/go/analysis"
)

const (
	serviceHasAutherAssertionAnalyzer = "servicehasautherassertion"
	serviceHasAutherAssertionDoc      = "checks that services embedding annotations.Service declare a compile-time assertion that they implement the auther interface"
)

type serviceHasAutherAssertionSettings struct {
	Disabled bool `json:"disabled"`
}

func newServiceHasAutherAssertionAnalyzer(rule serviceHasAutherAssertionSettings) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: serviceHasAutherAssertionAnalyzer,
		Doc:  serviceHasAutherAssertionDoc,
		Run: func(pass *analysis.Pass) (any, error) {
			annotated := findAnnotatedStructs(pass)
			if len(annotated) == 0 {
				return nil, nil
			}

			assertions := collectInterfaceAssertions(pass)
			for _, s := range annotated {
				if !hasAssertion(assertions, s.authBy, s.obj) {
					pass.ReportRangef(s.typeSpec, "%s embeds annotations.Service[..., %s] but package is missing: var _ %s = (*%s)(nil)",
						s.name, s.authBy, s.authBy, s.name)
				}
			}

			return nil, nil
		},
	}
}
