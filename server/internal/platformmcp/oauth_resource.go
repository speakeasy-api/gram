package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	platformoauth "github.com/speakeasy-api/gram/server/internal/platformmcp/oauth"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type AHPProjectOption struct{ ID, Name, Slug string }

type AHPProjects interface {
	EligibleProjects(context.Context, Principal) ([]AHPProjectOption, error)
	AuthorizeProject(context.Context, Principal, string) (context.Context, error)
}

func (s *OAuthHTTP) resourcePath() string {
	if s.ahpProjects != nil {
		return "/ahp"
	}
	return "/platform-mcp"
}

func (s *OAuthHTTP) resourceAt(base *url.URL) string {
	if s.ahpProjects == nil {
		return platformResource(base)
	}
	return ahpResource(base)
}

func (s *OAuthHTTP) requestBaseURL(ctx context.Context) *url.URL {
	if s.ahpProjects == nil {
		return platformBaseURL(ctx, s.baseURL)
	}
	base, err := url.Parse(requestorigin.BaseURL(ctx, s.baseURL.String()))
	if err != nil {
		return s.baseURL
	}
	return base
}

func (s *OAuthHTTP) endpointURL(base *url.URL, segment string) string {
	return s.resourceAt(base) + "/" + segment
}

func (s *OAuthHTTP) protectedResourceURL(base *url.URL) string {
	if s.ahpProjects == nil {
		return platformProtectedResourceMetadataURL(base)
	}
	endpoint := base.JoinPath(".well-known", "oauth-protected-resource", "ahp")
	endpoint.RawQuery, endpoint.Fragment, endpoint.RawFragment = "", "", ""
	endpoint.ForceQuery = false
	return endpoint.String()
}

func (s *OAuthHTTP) challengeNamespace() string {
	if s.ahpProjects != nil {
		return "ahp:"
	}
	return ""
}

func (s *OAuthHTTP) challengePrefix() string {
	return platformOAuthChallengePrefix + s.challengeNamespace()
}

func (s *OAuthHTTP) authorizeConnection(ctx context.Context, connection platformoauth.Connection, resource string) error {
	subject, err := urn.ParseSessionSubject(connection.Subject)
	if err != nil || subject.Kind != urn.SessionSubjectKindUser {
		return ErrForbidden
	}
	principal := Principal{UserID: subject.ID, OrganizationID: connection.OrganizationID, ConnectionID: connection.ID, Generation: connection.Generation, ClientID: connection.ClientID, Surface: SurfacePlatformMCP}
	if err := s.gateAndAuthorize(ctx, principal); err != nil {
		return err
	}
	if s.ahpProjects != nil {
		if connection.ResourceURI != resource || connection.ProjectID == "" {
			return ErrForbidden
		}
		if _, err := s.ahpProjects.AuthorizeProject(ctx, principal, connection.ProjectID); err != nil {
			return fmt.Errorf("authorize AHP project: %w", err)
		}
	}
	return nil
}

func (s *OAuthHTTP) connectionResource(base *url.URL) string {
	if s.ahpProjects == nil {
		return ""
	}
	return s.resourceAt(base)
}

func ahpResource(base *url.URL) string {
	endpoint := base.JoinPath("ahp")
	endpoint.RawQuery, endpoint.Fragment, endpoint.RawFragment = "", "", ""
	endpoint.ForceQuery = false
	return endpoint.String()
}

func writeAHPProjectAuthorizationError(w http.ResponseWriter, r *http.Request, challenge oauthChallenge, err error) {
	code, description := "access_denied", "The selected project is not available to your account."
	if errors.Is(err, ErrUnavailable) {
		code, description = "temporarily_unavailable", "Project access could not be verified."
	}
	redirectOAuthError(w, r, challenge.RedirectURI, challenge.State, &oauthwire.Error{Code: code, Description: description})
}

// readChallenge accepts only server-generated IDs before constructing a cache key.
func (s *OAuthHTTP) readChallenge(ctx context.Context, state string, consume bool) (oauthChallenge, error) {
	id, err := uuid.Parse(state)
	if err != nil || id.String() != state {
		return oauthChallenge{}, ErrUnauthorized
	}
	key := s.challengePrefix() + state
	challenge, err := s.cache.Get(ctx, key)
	if err != nil {
		return oauthChallenge{}, fmt.Errorf("read OAuth challenge: %w", err)
	}
	if challenge.Namespace != s.challengeNamespace() || challenge.ID != state {
		return oauthChallenge{}, ErrUnauthorized
	}
	if consume {
		challenge, err = s.cache.GetAndDelete(ctx, key)
		if err != nil {
			return oauthChallenge{}, fmt.Errorf("consume OAuth challenge: %w", err)
		}
		if challenge.Namespace != s.challengeNamespace() || challenge.ID != state {
			return oauthChallenge{}, ErrUnauthorized
		}
	}
	return challenge, nil
}
