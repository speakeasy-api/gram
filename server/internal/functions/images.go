package functions

import (
	"context"
	"fmt"
	"strings"
	"text/template"

	"github.com/google/uuid"
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

func NewTemplateImageSelector(tpl string) (*TemplateImageSelector, error) {
	templ, err := template.New("functions-image").Parse(tpl)
	if err != nil {
		return nil, fmt.Errorf("parse image template: %w", err)
	}

	return &TemplateImageSelector{template: templ}, nil
}

func (s *TemplateImageSelector) Select(ctx context.Context, req ImageRequest) (string, error) {
	buf := new(strings.Builder)

	err := s.template.Execute(buf, req)
	if err != nil {
		return "", fmt.Errorf("render image name: %w", err)
	}

	return buf.String(), nil
}
