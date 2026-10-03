package functions

import (
	"context"
	"fmt"
	"strings"
	"text/template"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/inv"
)

type ImageRequest struct {
	ProjectID    uuid.UUID
	DeploymentID uuid.UUID
	FunctionID   uuid.UUID

	Runtime Runtime
	Version RunnerVersion
}

type ImageSelector interface {
	Select(ctx context.Context, req ImageRequest) (string, error)
}

type TemplateImageSelector struct {
	template *template.Template
}

func NewTemplateImageSelector(tpl string) *TemplateImageSelector {
	templ, err := template.New("functions-image").Parse(tpl)
	inv.Require("functions image selector", "template parses", err)
	return &TemplateImageSelector{template: templ}
}

func (s *TemplateImageSelector) Select(ctx context.Context, req ImageRequest) (string, error) {
	buf := new(strings.Builder)

	err := s.template.Execute(buf, req)
	if err != nil {
		return "", fmt.Errorf("render image name: %w", err)
	}

	return buf.String(), nil
}
