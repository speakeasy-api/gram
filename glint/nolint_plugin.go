package glint

import (
	"fmt"

	"github.com/golangci/plugin-module-register/register"
	"golang.org/x/tools/go/analysis"
)

const (
	// nolintPluginName is registered separately from the glint plugin because
	// golangci-lint treats each plugin as one linter: a diagnostic about a
	// //nolint:glint directive raised from inside the glint plugin would be
	// suppressed by that same directive.
	nolintPluginName = "glintnolint"
)

func init() {
	register.Plugin(nolintPluginName, NewNolint)
}

type nolintPluginSettings struct {
	Disabled bool `json:"disabled"`
}

type nolintPlugin struct {
	settings nolintPluginSettings
}

func NewNolint(rawSettings any) (register.LinterPlugin, error) {
	s, err := register.DecodeSettings[nolintPluginSettings](rawSettings)
	if err != nil {
		return nil, fmt.Errorf("decode settings: %w", err)
	}

	return &nolintPlugin{settings: s}, nil
}

func (p *nolintPlugin) BuildAnalyzers() ([]*analysis.Analyzer, error) {
	if p.settings.Disabled {
		return []*analysis.Analyzer{}, nil
	}

	analyzer, err := newGlintNolintAnalyzer()
	if err != nil {
		return nil, err
	}

	return []*analysis.Analyzer{analyzer}, nil
}

func (p *nolintPlugin) GetLoadMode() string {
	return register.LoadModeSyntax
}
