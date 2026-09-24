package mcpregistry

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"
	goahttp "goa.design/goa/v3/http"
	goa "goa.design/goa/v3/pkg"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/registry_discovery/server"
	gen "github.com/speakeasy-api/gram/server/gen/registry_discovery"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

var _ gen.Service = (*Service)(nil)

// AttachDiscovery mounts nothing until both persistence and authorization are ready.
func (s *Service) AttachDiscovery(ctx context.Context, mux goahttp.Muxer, enabled bool, a *auth.Auth, az *authz.Engine) error {
	if !enabled {
		return nil
	}
	if a == nil || az == nil {
		return errors.New("registry discovery authorization unavailable")
	}
	if err := s.Ready(ctx); err != nil {
		return err
	}
	s.auth = a
	s.authz = az
	endpoints := gen.NewEndpoints(s)
	endpoints.Use(middleware.MapErrors())
	m := discoveryMux{mux}
	srv.Mount(m, srv.New(endpoints, m, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, discoveryErrorFormatter))
	return nil
}

// Goa treats empty optional query strings as absent. Preserve this unsupported
// parameter's presence before the generated decoder, including ?updated_since=.
type discoveryMux struct{ goahttp.Muxer }

// Chi routes URL.Path when RawPath is absent; those parameters are already
// decoded. Goa's default Vars would unescape literal percent sequences again.
func (m discoveryMux) Vars(r *http.Request) map[string]string {
	if r.URL.RawPath != "" {
		return m.Muxer.Vars(r)
	}
	params := chi.RouteContext(r.Context()).URLParams
	vars := make(map[string]string, len(params.Keys))
	for i, key := range params.Keys {
		vars[key] = params.Values[i]
	}
	return vars
}

func (m discoveryMux) Handle(method, path string, h http.HandlerFunc) {
	m.Muxer.Handle(method, path, func(w http.ResponseWriter, r *http.Request) {
		q, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(discoveryWireError{Message: "invalid discovery request", Status: http.StatusBadRequest})
			return
		}
		if q.Has("updated_since") && q.Get("updated_since") == "" {
			q.Set("updated_since", "unsupported")
			r = r.Clone(r.Context())
			u := *r.URL
			u.RawQuery = q.Encode()
			r.URL = &u
		}
		h(w, r)
	})
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, scheme *security.APIKeyScheme) (context.Context, error) {
	if s.auth == nil {
		return ctx, oops.C(oops.CodeUnauthorized)
	}
	return s.auth.Authorize(ctx, key, scheme)
}

func (s *Service) authorizeDiscovery(ctx context.Context, updated *string) error {
	a, ok := contextvalues.GetAuthContext(ctx)
	if !ok || a == nil || a.ProjectID == nil || s.authz == nil {
		return oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: a.ProjectID.String(), Dimensions: nil}); err != nil {
		return err
	}
	if updated != nil {
		return &gen.RegistryDiscoveryError{Name: "discovery_bad_request", Message: ErrUnsupportedUpdatedSince.Error()}
	}
	return nil
}

func discoveryError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrNotFound):
		return &gen.RegistryDiscoveryError{Name: "discovery_not_found", Message: ErrNotFound.Error()}
	case errors.Is(err, ErrInvalidCursor), errors.Is(err, ErrInvalidListOptions):
		return &gen.RegistryDiscoveryError{Name: "discovery_bad_request", Message: err.Error()}
	default:
		return oops.C(oops.CodeUnexpected)
	}
}

func value(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func discoveryResult(p DiscoveryPage) *gen.RegistryDiscoveryPage {
	r := &gen.RegistryDiscoveryPage{Servers: make([]any, 0, len(p.Records)), Metadata: &gen.RegistryDiscoveryMetadata{Count: len(p.Records), NextCursor: nil}}
	if p.NextCursor != "" {
		r.Metadata.NextCursor = &p.NextCursor
	}
	for _, record := range p.Records {
		r.Servers = append(r.Servers, record)
	}
	return r
}

func (s *Service) DiscoverServers(ctx context.Context, p *gen.DiscoverServersPayload) (*gen.RegistryDiscoveryPage, error) {
	if err := s.authorizeDiscovery(ctx, p.UpdatedSince); err != nil {
		return nil, err
	}
	page, err := s.Discover(ctx, DiscoveryOptions{Search: value(p.Search), Version: value(p.Version), IncludeDeleted: p.IncludeDeleted, Cursor: value(p.Cursor), Limit: p.Limit, UpdatedSince: p.UpdatedSince})
	if err != nil {
		return nil, discoveryError(err)
	}
	return discoveryResult(page), nil
}

func (s *Service) DiscoverVersions(ctx context.Context, p *gen.DiscoverVersionsPayload) (*gen.RegistryDiscoveryPage, error) {
	if err := s.authorizeDiscovery(ctx, p.UpdatedSince); err != nil {
		return nil, err
	}
	row, err := s.LookupDiscoveryVersion(ctx, p.ServerName, "latest", p.IncludeDeleted)
	if err != nil {
		return nil, discoveryError(err)
	}
	return discoveryResult(DiscoveryPage{Records: []json.RawMessage{row.Data}, NextCursor: ""}), nil
}

func (s *Service) DiscoverVersion(ctx context.Context, p *gen.DiscoverVersionPayload) (any, error) {
	if err := s.authorizeDiscovery(ctx, p.UpdatedSince); err != nil {
		return nil, err
	}
	row, err := s.LookupDiscoveryVersion(ctx, p.ServerName, p.Version, p.IncludeDeleted)
	if err != nil {
		return nil, discoveryError(err)
	}
	return row.Data, nil
}

// Generated decoding errors must not echo rejected values or use Goa's envelope.
type discoveryWireError struct {
	Message string `json:"error"`
	Status  int    `json:"-"`
}

func (e discoveryWireError) StatusCode() int { return e.Status }

func discoveryErrorFormatter(ctx context.Context, err error) goahttp.Statuser {
	if discovery, ok := errors.AsType[*gen.RegistryDiscoveryError](err); ok {
		status := http.StatusBadRequest
		if discovery.Name == "discovery_not_found" {
			status = http.StatusNotFound
		}
		return discoveryWireError{Message: discovery.Message, Status: status}
	}
	response := goahttp.NewErrorResponse(ctx, err)
	// Shared service errors have explicit generated HTTP mappings. The generic
	// response status heuristic is still 400 here, before that mapping is applied.
	if service, ok := errors.AsType[*goa.ServiceError](err); ok {
		// Only errors declared by this service retain the shared envelope.
		//nolint:exhaustive // Other codes follow the generic status handling below.
		switch oops.Code(service.Name) {
		case oops.CodeUnauthorized, oops.CodeForbidden, oops.CodeBadRequest,
			oops.CodeNotFound, oops.CodeConflict, oops.CodeUnsupportedMedia,
			oops.CodeInvalid, oops.CodeInvariantViolation, oops.CodeUnexpected,
			oops.CodeGatewayError:
			return response
		}
	}

	if response.StatusCode() == http.StatusBadRequest {
		return discoveryWireError{Message: "invalid discovery request", Status: http.StatusBadRequest}
	}
	return response
}
