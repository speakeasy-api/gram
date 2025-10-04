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
	FunctionsID  uuid.UUID

	Runtime Runtime
}

type ImageSelector interface {
	Select(ctx context.Context, req ImageRequest) (string, error)
}

type StaticRunnerImageSelector struct {
	template template.Template
}

func NewStaticRunnerImageSelector(tmplStr string) (*StaticRunnerImageSelector, error) {
	tmpl, err := template.New("docker-runner-image").Parse(tmplStr)
	if err != nil {
		return nil, fmt.Errorf("parse docker runner image name template: %w", err)
	}

	return &StaticRunnerImageSelector{
		template: *tmpl,
	}, nil
}

func (s *StaticRunnerImageSelector) Select(ctx context.Context, req ImageRequest) (string, error) {
	buf := new(strings.Builder)

	err := s.template.Execute(buf, req)
	if err != nil {
		return "", fmt.Errorf("render image name: %w", err)
	}

	return buf.String(), nil
}
