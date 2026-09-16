package okta

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

// Fixtures seeds a Fake with in-memory Okta data.
type Fixtures struct {
	// Apps are the applications returned by ListApps and GetApp.
	Apps []App

	// AppUsers maps an application id to its user assignments.
	AppUsers map[string][]AppUser

	// AppGroups maps an application id to its group assignments.
	AppGroups map[string][]AppGroup

	// Groups are the groups returned by ListGroups.
	Groups []Group

	// GrantedScopes are the scopes VerifyScopes reports as granted.
	GrantedScopes []string
}

// Fake is an in-memory Client for tests and local development. It matches
// on Query and Search only; ListAppsRequest.Status is ignored.
type Fake struct {
	mu       sync.Mutex
	fixtures Fixtures
	calls    []string
	err      error
}

var _ Client = (*Fake)(nil)

func NewFake(fixtures Fixtures) *Fake {
	if fixtures.AppUsers == nil {
		fixtures.AppUsers = map[string][]AppUser{}
	}
	if fixtures.AppGroups == nil {
		fixtures.AppGroups = map[string][]AppGroup{}
	}
	return &Fake{mu: sync.Mutex{}, fixtures: fixtures, calls: nil, err: nil}
}

// SetError makes every subsequent call fail with err until cleared with nil.
func (f *Fake) SetError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

// Calls returns the method names invoked so far, in order.
func (f *Fake) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *Fake) record(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, name)
	return f.err
}

func (f *Fake) ListApps(_ context.Context, req ListAppsRequest) ([]App, error) {
	if err := f.record("ListApps"); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]App, 0, len(f.fixtures.Apps))
	for _, app := range f.fixtures.Apps {
		if req.Query != "" && !strings.HasPrefix(strings.ToLower(app.Label), strings.ToLower(req.Query)) && !strings.HasPrefix(strings.ToLower(app.Name), strings.ToLower(req.Query)) {
			continue
		}
		out = append(out, app)
	}
	return out, nil
}

func (f *Fake) GetApp(_ context.Context, appID string) (*App, error) {
	if err := f.record("GetApp"); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, app := range f.fixtures.Apps {
		if app.ID == appID {
			found := app
			return &found, nil
		}
	}
	return nil, &APIError{Method: http.MethodGet, Path: "/api/v1/apps/" + appID, StatusCode: http.StatusNotFound, ErrorCode: "E0000007", Summary: fmt.Sprintf("Not found: Resource not found: %s (AppInstance)", appID)}
}

func (f *Fake) ListAppUsers(_ context.Context, req ListAppUsersRequest) ([]AppUser, error) {
	if err := f.record("ListAppUsers"); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.fixtures.AppUsers[req.AppID]), nil
}

func (f *Fake) ListAppGroups(_ context.Context, req ListAppGroupsRequest) ([]AppGroup, error) {
	if err := f.record("ListAppGroups"); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.fixtures.AppGroups[req.AppID]), nil
}

func (f *Fake) ListGroups(_ context.Context, req ListGroupsRequest) ([]Group, error) {
	if err := f.record("ListGroups"); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Group, 0, len(f.fixtures.Groups))
	for _, g := range f.fixtures.Groups {
		if req.Search != "" && !strings.HasPrefix(strings.ToLower(g.Name), strings.ToLower(req.Search)) {
			continue
		}
		out = append(out, g)
	}
	return out, nil
}

func (f *Fake) VerifyScopes(_ context.Context, required []string) (*ScopeVerification, error) {
	if err := f.record("VerifyScopes"); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	missing := make([]string, 0)
	for _, s := range required {
		if !slices.Contains(f.fixtures.GrantedScopes, s) {
			missing = append(missing, s)
		}
	}
	granted := make([]string, 0, len(f.fixtures.GrantedScopes))
	granted = append(granted, f.fixtures.GrantedScopes...)
	return &ScopeVerification{Granted: granted, Missing: missing, DPoPBound: true, ExpiresAt: time.Now().Add(time.Hour)}, nil
}
