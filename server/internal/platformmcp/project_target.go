package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/google/uuid"
)

// projectSlugDescription and defaultableProjectSlugDescription describe the
// project_slug argument in hand-written schemas, in the words every tool uses
// to name a project.
const (
	projectSlugDescription            = "The project's slug, as list_projects returns it."
	defaultableProjectSlugDescription = "The project's slug, as list_projects returns it. Omit it to use the organization's Default project."
)

// unknownProjectID stands in for a project_slug that names no project in the
// caller's organization. It is the Max UUID, which no UUID generator produces,
// so the handler finds no project with it and answers exactly as it answers for
// any other project it cannot find or may not see. Refusing at the boundary
// instead would answer an unknown slug differently from a project the caller
// may not see, and so tell the caller which slugs exist.
var unknownProjectID = uuid.Max

// projectSlugTarget locates the two fields of a tool input that names its
// project by slug while its handler acts on the project's ID: the project_slug
// argument, and a ProjectID field hidden from the schema with `json:"-"`.
type projectSlugTarget struct {
	// slug is the index of the project_slug field.
	slug []int

	// id is the index of the ProjectID field the resolved ID is written to.
	id []int
}

// projectSlugTargetFor finds the project fields of a tool input type. A type
// without a hidden ProjectID field resolves its own project, so it has no
// target. A hidden ProjectID without a project_slug argument to fill it from is
// a declaration error, caught when the tool is registered.
func projectSlugTargetFor[In any](toolName string) (projectSlugTarget, bool) {
	inputType := reflect.TypeFor[In]()
	if inputType.Kind() != reflect.Struct {
		return projectSlugTarget{slug: nil, id: nil}, false
	}
	id, ok := inputType.FieldByName("ProjectID")
	if !ok || id.Tag.Get("json") != "-" {
		return projectSlugTarget{slug: nil, id: nil}, false
	}
	if id.Type.Kind() != reflect.String {
		panic(fmt.Sprintf("platformmcp: tool %q hides a ProjectID that is not a string", toolName))
	}
	for _, field := range reflect.VisibleFields(inputType) {
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "project_slug" && field.Type.Kind() == reflect.String {
			return projectSlugTarget{slug: field.Index, id: id.Index}, true
		}
	}
	panic(fmt.Sprintf("platformmcp: tool %q hides a ProjectID but takes no project_slug to resolve it from", toolName))
}

// resolveProjectSlug replaces the input's project_slug with the ID of the
// project it names, written to the hidden ProjectID, so handlers and the
// services behind them keep acting on project IDs while callers name projects
// by slug. The slug is cleared so the input names its project exactly once.
//
// An omitted slug leaves both empty for the handler, which already knows what
// an omitted project means for its tool: the schema requires the slug where
// the tool needs one, and a tool that defaults to the organization's Default
// project or reads the whole organization does so itself.
func (r *Registrar) resolveProjectSlug(ctx context.Context, target projectSlugTarget, input any) error {
	value := reflect.ValueOf(input).Elem()
	slug := strings.TrimSpace(value.FieldByIndex(target.slug).String())
	if slug == "" {
		return nil
	}
	if r.projects == nil {
		return ErrUnavailable
	}
	principal, err := principalFromToolContext(ctx)
	if err != nil {
		return err
	}
	projectID := unknownProjectID
	project, err := r.projects.ResolveProject(ctx, principal.OrganizationID, slug)
	switch {
	case err == nil:
		projectID = project.ID
	case !errors.Is(err, ErrRegistrationInvalid):
		return fmt.Errorf("resolve platform mcp project slug: %w", err)
	}
	value.FieldByIndex(target.id).SetString(projectID.String())
	value.FieldByIndex(target.slug).SetString("")
	return nil
}
