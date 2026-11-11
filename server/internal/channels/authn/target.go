package authn

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

// Target is resolved from the canonical resource URL, never from the active
// organization or a default project in the browser session.
type Target struct {
	OrganizationID   string
	ProjectID        uuid.UUID
	OrganizationSlug string
	ProjectSlug      string
	Resource         string
}
type targetKey struct{}

func WithTarget(ctx context.Context, target Target) context.Context {
	return context.WithValue(ctx, targetKey{}, target)
}
func TargetFromContext(ctx context.Context) (Target, bool) {
	t, ok := ctx.Value(targetKey{}).(Target)
	return t, ok
}

// ResourceSlugs rejects aliases, escaped separators and noncanonical resources.
func ResourceSlugs(path string) (org, project, endpoint string, ok bool) {
	rest, found := strings.CutPrefix(path, "/channels/")
	if !found {
		return "", "", "", false
	}
	parts := strings.Split(rest, "/")
	if len(parts) != 3 || (parts[2] != "mcp" && parts[2] != "api") {
		return "", "", "", false
	}
	for _, s := range parts[:2] {
		if s == "" || s == "." || s == ".." || strings.ContainsAny(s, "%\\ \t\r\n") {
			return "", "", "", false
		}
	}
	return parts[0], parts[1], parts[2], true
}
func ResolveTarget(ctx context.Context, db *pgxpool.Pool, origin, resource string) (Target, error) {
	u, err := url.Parse(resource)
	if err != nil || u.Scheme+"://"+u.Host != strings.TrimRight(origin, "/") || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(resource, "#") || u.RawPath != "" {
		return Target{}, errors.New("invalid channels resource")
	}
	orgSlug, projectSlug, endpoint, ok := ResourceSlugs(u.Path)
	if !ok || endpoint != "mcp" || db == nil {
		return Target{}, errors.New("invalid channels resource")
	}
	org, err := organizationsrepo.New(db).GetOrganizationMetadataBySlug(ctx, orgSlug)
	if err != nil {
		return Target{}, fmt.Errorf("resolve Channels organization: %w", err)
	}
	project, err := projectsrepo.New(db).GetProjectBySlug(ctx, projectsrepo.GetProjectBySlugParams{Slug: projectSlug, OrganizationID: org.ID})
	if err != nil {
		return Target{}, fmt.Errorf("resolve Channels project: %w", err)
	}
	canonical := strings.TrimRight(origin, "/") + "/channels/" + org.Slug + "/" + project.Slug + "/mcp"
	if resource != canonical {
		return Target{}, errors.New("invalid channels resource")
	}
	return Target{OrganizationID: org.ID, ProjectID: project.ID, OrganizationSlug: org.Slug, ProjectSlug: project.Slug, Resource: canonical}, nil
}
func (s *Service) target(ctx context.Context, resource string) (Target, error) {
	return ResolveTarget(ctx, s.config.DB, s.origin, resource)
}
