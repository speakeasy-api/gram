package authn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"github.com/speakeasy-api/gram/server/internal/channels/authn/repo"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd"
)

type registration struct {
	ClientName    string   `json:"client_name"`
	RedirectURIs  []string `json:"redirect_uris"`
	AuthMethod    string   `json:"token_endpoint_auth_method"`
	GrantTypes    []string `json:"grant_types"`
	ResponseTypes []string `json:"response_types"`
}

func (s *Service) register(w http.ResponseWriter, r *http.Request) {
	s.cleanup(r.Context())
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		oauthError(w, 400, "invalid_client_metadata")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	var v registration
	dec := json.NewDecoder(r.Body)
	if err = dec.Decode(&v); err != nil {
		oauthError(w, 400, "invalid_client_metadata")
		return
	}
	if v.AuthMethod == "" {
		v.AuthMethod = oauthwire.AuthMethodClientSecretBasic
	}
	if strings.TrimSpace(v.ClientName) == "" || len(v.ClientName) > 256 || len(v.RedirectURIs) == 0 || len(v.RedirectURIs) > 20 || !slices.Contains([]string{"none", "client_secret_basic", "client_secret_post"}, v.AuthMethod) {
		oauthError(w, 400, "invalid_client_metadata")
		return
	}
	for _, u := range v.RedirectURIs {
		if validateRedirectURI(u) != nil {
			oauthError(w, 400, "invalid_redirect_uri")
			return
		}
	}
	for _, g := range v.GrantTypes {
		if g != "authorization_code" && g != "refresh_token" {
			oauthError(w, 400, "invalid_client_metadata")
			return
		}
	}
	for _, g := range v.ResponseTypes {
		if g != "code" {
			oauthError(w, 400, "invalid_client_metadata")
			return
		}
	}
	id := randomToken()
	secret := ""
	hash := ""
	if v.AuthMethod != "none" {
		secret = randomToken()
		h, e := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
		if e != nil {
			oauthError(w, 500, "server_error")
			return
		}
		hash = string(h)
	}
	if err = repo.New(s.config.DB).RegisterClient(r.Context(), repo.RegisterClientParams{ClientID: id, ClientName: v.ClientName, RedirectUris: v.RedirectURIs, SecretHash: hash, TokenEndpointAuthMethod: v.AuthMethod}); err != nil {
		oauthError(w, 500, "server_error")
		return
	}
	out := map[string]any{"client_id": id, "client_name": v.ClientName, "redirect_uris": v.RedirectURIs, "token_endpoint_auth_method": v.AuthMethod, "grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"}}
	if secret != "" {
		out["client_secret"] = secret
		out["client_secret_expires_at"] = 0
	}
	writeJSON(w, 201, out)
}
func (s *Service) resolveClient(ctx context.Context, id string, metadata bool) (repo.ChannelsOauthClient, error) {
	if metadata && cimd.IsClientIDURL(id) {
		var cacheState cimd.CacheState
		result, err := s.clients.Resolve(ctx, id, cacheState)
		if err != nil || result.Document == nil {
			return emptyClient(), errors.New("invalid client metadata")
		}
		d := result.Document
		// Channels supports public CIMD clients; assertion authentication is not
		// advertised and must never be silently downgraded to unauthenticated.
		if d.DeclaredAuthMethod() != "none" {
			return emptyClient(), errors.New("unsupported client authentication method")
		}
		for _, redirect := range d.RedirectURIs {
			if err := validateRedirectURI(redirect); err != nil {
				return emptyClient(), fmt.Errorf("invalid client metadata redirect URI: %w", err)
			}
		}
		if err = repo.New(s.config.DB).UpsertMetadataClient(ctx, repo.UpsertMetadataClientParams{ClientID: id, ClientName: d.ClientName, RedirectUris: d.RedirectURIs}); err != nil {
			return emptyClient(), fmt.Errorf("persist Channels metadata client: %w", err)
		}
	}
	client, err := repo.New(s.config.DB).GetClient(ctx, id)
	if err != nil {
		return emptyClient(), fmt.Errorf("load Channels client: %w", err)
	}
	return client, nil
}
func (s *Service) authenticateClient(w http.ResponseWriter, r *http.Request) (repo.ChannelsOauthClient, bool) {
	if code := oauthwire.ClientAuthenticationError(r); code != "" {
		s.clientError(w, r, code)
		return emptyClient(), false
	}
	id := r.PostForm.Get("client_id")
	secret := r.PostForm.Get("client_secret")
	method := "none"
	if len(r.Header.Values("Authorization")) > 0 {
		var ok bool
		id, secret, ok = r.BasicAuth()
		if !ok {
			s.clientError(w, r, "invalid_client")
			return emptyClient(), false
		}
		var err error
		id, err = url.QueryUnescape(id)
		if err != nil {
			s.clientError(w, r, "invalid_client")
			return emptyClient(), false
		}
		secret, err = url.QueryUnescape(secret)
		if err != nil {
			s.clientError(w, r, "invalid_client")
			return emptyClient(), false
		}
		method = "client_secret_basic"
	} else if secret != "" {
		method = "client_secret_post"
	}
	c, err := s.resolveClient(r.Context(), id, false)
	if err != nil || c.TokenEndpointAuthMethod != method || (method != "none" && bcrypt.CompareHashAndPassword([]byte(c.SecretHash), []byte(secret)) != nil) || r.PostForm.Get("client_assertion") != "" || r.PostForm.Get("client_assertion_type") != "" {
		s.clientError(w, r, "invalid_client")
		return emptyClient(), false
	}
	return c, true
}
func (s *Service) clientError(w http.ResponseWriter, r *http.Request, code string) {
	status := 400
	if code == "invalid_client" {
		var challenge string
		status, challenge = oauthwire.InvalidClientResponse(r.Header)
		if challenge != "" {
			w.Header().Set("WWW-Authenticate", challenge)
		}
	}
	oauthError(w, status, code)
}

// validateRedirectURI applies Channels callback shape checks without changing
// the shared native-app scheme policy.
func validateRedirectURI(raw string) error {
	if err := oauthwire.ValidateRedirectURI(raw); err != nil {
		return fmt.Errorf("validate redirect URI: %w", err)
	}
	parsed, err := url.Parse(raw)
	if err != nil || strings.Contains(raw, "#") || parsed.User != nil {
		return errors.New("redirect URI must not contain a fragment or user information")
	}
	if scheme := strings.ToLower(parsed.Scheme); scheme == "http" || scheme == "https" {
		if !parsed.IsAbs() || parsed.Opaque != "" || parsed.Hostname() == "" {
			return errors.New("HTTP redirect URI must be an absolute URL with a host")
		}
	}
	return nil
}

func emptyClient() repo.ChannelsOauthClient { var client repo.ChannelsOauthClient; return client }
