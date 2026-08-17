package glint

import (
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
)

const (
	researchImportBoundaryAnalyzer = "researchimportboundary"
	researchImportBoundaryMessage  = "research agent packages must not import data-store packages: the model calling these tools is treated as hostile, so tenant data reaches the agent only through the briefing, compiled by trusted code before the run. Want more input? Add a briefing compiler, not an import."
)

// researchImportBoundaryPackages are the packages the boundary protects: the
// research agent loop and the web tools it can call. Prefix-matched so
// subpackages inherit the rule.
var researchImportBoundaryPackages = []string{
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/researchagent",
	"github.com/speakeasy-api/gram/server/internal/platformtools/research",
}

// researchImportBoundaryForbidden are import prefixes that reach a data
// store. A generated repo package is any gram-internal path ending in /repo.
var researchImportBoundaryForbidden = []string{
	"github.com/jackc/pgx",
	"database/sql",
	"github.com/ClickHouse/clickhouse-go",
	"github.com/redis/go-redis",
}

type researchImportBoundarySettings struct {
	Disabled bool `json:"disabled"`

	// Packages overrides the protected package prefixes, for tests whose
	// packages cannot carry the production import paths.
	Packages []string `json:"packages"`
}

func newResearchImportBoundaryAnalyzer(rule researchImportBoundarySettings) *analysis.Analyzer {
	protected := researchImportBoundaryPackages
	if len(rule.Packages) > 0 {
		protected = rule.Packages
	}

	return &analysis.Analyzer{
		Name: researchImportBoundaryAnalyzer,
		Doc:  researchImportBoundaryMessage,
		Run: func(pass *analysis.Pass) (any, error) {
			guarded := false
			for _, prefix := range protected {
				if pass.Pkg.Path() == prefix || strings.HasPrefix(pass.Pkg.Path(), prefix+"/") {
					guarded = true
					break
				}
			}
			if !guarded {
				return nil, nil
			}

			for _, file := range pass.Files {
				for _, imp := range file.Imports {
					path, err := strconv.Unquote(imp.Path.Value)
					if err != nil {
						continue
					}
					if researchImportBoundaryViolated(path) {
						pass.ReportRangef(imp, "import of %q is forbidden here: %s", path, researchImportBoundaryMessage)
					}
				}
			}

			return nil, nil
		},
	}
}

// researchImportBoundaryViolated reports whether path reaches a data store —
// a known driver, or any gram-internal generated repo package.
func researchImportBoundaryViolated(path string) bool {
	for _, prefix := range researchImportBoundaryForbidden {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}

	return strings.HasPrefix(path, "github.com/speakeasy-api/gram/") &&
		(strings.HasSuffix(path, "/repo") || strings.Contains(path, "/repo/"))
}
