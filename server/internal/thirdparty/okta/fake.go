package okta

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// Fixtures seeds a Fake with in-memory Okta data.
type Fixtures struct {
	// Apps are the applications returned by ListApps and GetApp.
	Apps []App

	// AppUsers maps an application id to its user assignments.
	AppUsers map[string][]AppUser

	// AppGroups maps an application id to its group assignments.
	AppGroups map[string][]AppGroup

	// GrantedScopes are the scopes VerifyScopes reports as granted.
	GrantedScopes []string
}

// Fake is an in-memory Client for tests and local development. It matches
// on Query, Status, and Search only. Called directly it skips credential
// checks; clients from FakeFactory authenticate with their own Config.
type Fake struct {
	mu         sync.Mutex
	fixtures   Fixtures
	calls      []string
	err        error
	methodErrs map[string]error
	appErrs    map[string]map[string]error

	requiredSecret string
	bearerOnly     bool
	lastAuthMethod remotesessions.TokenEndpointAuthMethod
}

var _ Client = (*Fake)(nil)

func NewFake(fixtures Fixtures) *Fake {
	if fixtures.AppUsers == nil {
		fixtures.AppUsers = map[string][]AppUser{}
	}
	if fixtures.AppGroups == nil {
		fixtures.AppGroups = map[string][]AppGroup{}
	}
	return &Fake{
		mu:             sync.Mutex{},
		fixtures:       fixtures,
		calls:          nil,
		err:            nil,
		methodErrs:     map[string]error{},
		appErrs:        map[string]map[string]error{},
		requiredSecret: "",
		bearerOnly:     false,
		lastAuthMethod: "",
	}
}

// RequireClientSecret makes every call under a client_secret_basic Config
// fail with invalid_client, as Okta's token endpoint does, unless the Config's
// credential lease opens to secret. An empty secret clears the requirement.
func (f *Fake) RequireClientSecret(secret string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requiredSecret = secret
}

// SetBearerOnly makes VerifyScopes report a Bearer token (DPoPBound false),
// as an Okta app without DPoP enabled issues.
func (f *Fake) SetBearerOnly(bearerOnly bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bearerOnly = bearerOnly
}

// LastAuthMethod returns the auth method of the last call's Config; empty
// when no call ran under a Config.
func (f *Fake) LastAuthMethod() remotesessions.TokenEndpointAuthMethod {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastAuthMethod
}

// credentialError mirrors the token endpoint's refusal of a bad client
// secret, opening it through the Config's credential lease as the real client
// does, against requiredSecret read under f.mu. It runs without f.mu held, so
// a lease waiting on a database lock never blocks other fake calls.
func (f *Fake) credentialError(ctx context.Context, cfg *Config, requiredSecret string) error {
	if cfg == nil || requiredSecret == "" || authMethodOf(cfg) != remotesessions.TokenEndpointAuthMethodBasic {
		return nil
	}
	rejected := &APIError{Method: http.MethodPost, Path: tokenEndpointPath, StatusCode: http.StatusUnauthorized, ErrorCode: oautherr.CodeInvalidClient, Summary: "Client authentication failed."}
	if cfg.Credentials == nil {
		return rejected
	}
	lease, err := cfg.Credentials.Acquire(ctx, *cfg)
	if err != nil {
		return fmt.Errorf("acquire okta credential: %w", err)
	}
	defer lease.Close()
	secret, err := lease.ClientSecret(ctx)
	if err != nil {
		return fmt.Errorf("open okta client secret: %w", err)
	}
	if secret != requiredSecret {
		return rejected
	}
	return nil
}

// authMethodOf is the Config's token endpoint auth method, private_key_jwt
// when unset.
func authMethodOf(cfg *Config) remotesessions.TokenEndpointAuthMethod {
	if cfg.AuthMethod == "" {
		return remotesessions.TokenEndpointAuthMethodPrivateKeyJWT
	}
	return cfg.AuthMethod
}

// SetFixtures replaces the in-memory data for subsequent calls.
func (f *Fake) SetFixtures(fixtures Fixtures) {
	if fixtures.AppUsers == nil {
		fixtures.AppUsers = map[string][]AppUser{}
	}
	if fixtures.AppGroups == nil {
		fixtures.AppGroups = map[string][]AppGroup{}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fixtures = fixtures
}

// SetAppError makes one per-app method ("ListAppUsers" or "ListAppGroups")
// fail with err for appID only, until cleared with nil.
func (f *Fake) SetAppError(name, appID string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.appErrs == nil {
		f.appErrs = map[string]map[string]error{}
	}
	if err == nil {
		delete(f.appErrs[name], appID)
		return
	}
	if f.appErrs[name] == nil {
		f.appErrs[name] = map[string]error{}
	}
	f.appErrs[name][appID] = err
}

func (f *Fake) appError(name, appID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.appErrs[name][appID]
}

// SetError makes every subsequent call fail with err until cleared with nil.
func (f *Fake) SetError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

// SetMethodError makes one method (by name, for example "ListApps") fail with
// err until cleared with nil; SetError takes precedence.
func (f *Fake) SetMethodError(name string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err == nil {
		delete(f.methodErrs, name)
		return
	}
	if f.methodErrs == nil {
		f.methodErrs = make(map[string]error)
	}
	f.methodErrs[name] = err
}

// Calls returns the method names invoked so far, in order.
func (f *Fake) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *Fake) record(ctx context.Context, name string, cfg *Config) error {
	f.mu.Lock()
	f.calls = append(f.calls, name)
	if cfg != nil {
		f.lastAuthMethod = authMethodOf(cfg)
	}
	failure, requiredSecret := f.err, f.requiredSecret
	f.mu.Unlock()
	if failure != nil {
		return failure
	}
	if err := f.credentialError(ctx, cfg, requiredSecret); err != nil {
		return err
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	return f.methodErrs[name]
}

func (f *Fake) listApps(ctx context.Context, cfg *Config, req ListAppsRequest) ([]App, error) {
	if err := f.record(ctx, "ListApps", cfg); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]App, 0, len(f.fixtures.Apps))
	for _, app := range f.fixtures.Apps {
		if req.Status != "" && app.Status != req.Status {
			continue
		}
		if req.Query != "" && !strings.HasPrefix(strings.ToLower(app.Label), strings.ToLower(req.Query)) && !strings.HasPrefix(strings.ToLower(app.Name), strings.ToLower(req.Query)) {
			continue
		}
		out = append(out, cloneApp(app))
	}
	return out, nil
}

func (f *Fake) getApp(ctx context.Context, cfg *Config, appID string) (*App, error) {
	if err := f.record(ctx, "GetApp", cfg); err != nil {
		return nil, err
	}
	if err := validateAppID(appID); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, app := range f.fixtures.Apps {
		if app.ID == appID {
			found := cloneApp(app)
			return &found, nil
		}
	}
	return nil, &APIError{Method: http.MethodGet, Path: "/api/v1/apps/" + url.PathEscape(appID), StatusCode: http.StatusNotFound, ErrorCode: "E0000007", Summary: fmt.Sprintf("Not found: Resource not found: %s (AppInstance)", appID)}
}

// cloneApp copies the Features slice so callers cannot mutate fixtures.
func cloneApp(app App) App {
	app.Features = slices.Clone(app.Features)
	return app
}

func (f *Fake) listAppUsers(ctx context.Context, cfg *Config, req ListAppUsersRequest) ([]AppUser, error) {
	if err := f.record(ctx, "ListAppUsers", cfg); err != nil {
		return nil, err
	}
	if err := validateAppID(req.AppID); err != nil {
		return nil, err
	}
	if err := f.appError("ListAppUsers", req.AppID); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.fixtures.AppUsers[req.AppID]), nil
}

func (f *Fake) listAppGroups(ctx context.Context, cfg *Config, req ListAppGroupsRequest) ([]AppGroup, error) {
	if err := f.record(ctx, "ListAppGroups", cfg); err != nil {
		return nil, err
	}
	if err := validateAppID(req.AppID); err != nil {
		return nil, err
	}
	if err := f.appError("ListAppGroups", req.AppID); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.fixtures.AppGroups[req.AppID]), nil
}

func (f *Fake) verifyScopes(ctx context.Context, cfg *Config, required []string) (*ScopeVerification, error) {
	if err := f.record(ctx, "VerifyScopes", cfg); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	// Like Okta, a token carries only the requested scopes that are granted,
	// an unscoped request carries every granted scope, and an all-ungranted
	// request is refused with invalid_scope, which maps to no token at all.
	granted := make([]string, 0, len(required))
	missing := make([]string, 0)
	if len(required) == 0 {
		granted = append(granted, f.fixtures.GrantedScopes...)
	}
	for _, s := range required {
		if slices.Contains(f.fixtures.GrantedScopes, s) {
			granted = append(granted, s)
		} else {
			missing = append(missing, s)
		}
	}
	if len(granted) == 0 {
		return &ScopeVerification{Granted: granted, Missing: missing, DPoPBound: false, ExpiresAt: time.Time{}}, nil
	}
	return &ScopeVerification{Granted: granted, Missing: missing, DPoPBound: !f.bearerOnly, ExpiresAt: time.Now().Add(time.Hour)}, nil
}

// configuredFake is a FakeFactory client: it shares its org's Fake but
// authenticates with the Config it was requested with.
type configuredFake struct {
	fake *Fake

	cfg Config
}

var _ Client = (*configuredFake)(nil)

func (f *Fake) ListApps(ctx context.Context, req ListAppsRequest) ([]App, error) {
	return f.listApps(ctx, nil, req)
}

func (f *Fake) GetApp(ctx context.Context, appID string) (*App, error) {
	return f.getApp(ctx, nil, appID)
}

func (f *Fake) ListAppUsers(ctx context.Context, req ListAppUsersRequest) ([]AppUser, error) {
	return f.listAppUsers(ctx, nil, req)
}

func (f *Fake) ListAppGroups(ctx context.Context, req ListAppGroupsRequest) ([]AppGroup, error) {
	return f.listAppGroups(ctx, nil, req)
}

func (f *Fake) VerifyScopes(ctx context.Context, required []string) (*ScopeVerification, error) {
	return f.verifyScopes(ctx, nil, required)
}

func (c *configuredFake) ListApps(ctx context.Context, req ListAppsRequest) ([]App, error) {
	return c.fake.listApps(ctx, &c.cfg, req)
}

func (c *configuredFake) GetApp(ctx context.Context, appID string) (*App, error) {
	return c.fake.getApp(ctx, &c.cfg, appID)
}

func (c *configuredFake) ListAppUsers(ctx context.Context, req ListAppUsersRequest) ([]AppUser, error) {
	return c.fake.listAppUsers(ctx, &c.cfg, req)
}

func (c *configuredFake) ListAppGroups(ctx context.Context, req ListAppGroupsRequest) ([]AppGroup, error) {
	return c.fake.listAppGroups(ctx, &c.cfg, req)
}

func (c *configuredFake) VerifyScopes(ctx context.Context, required []string) (*ScopeVerification, error) {
	return c.fake.verifyScopes(ctx, &c.cfg, required)
}
