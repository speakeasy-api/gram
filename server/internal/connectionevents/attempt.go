// Package connectionevents records MCP connection attempts in ClickHouse: one
// row per step, shared by every flow. It holds metadata only and never holds a
// request up.
//
// A call site names what happened and nothing else:
//
//	attempt := connectionevents.Begin(scope)
//	recorder.Step(ctx, attempt, connectionevents.StepInboundAuthorize, connectionevents.ResultAccepted)
//	recorder.End(ctx, attempt, connectionevents.StepInboundConsent, connectionevents.CodeCanceled)
//
//	recorder.Once(ctx, scope, connectionevents.StepMCPRequest, connectionevents.CodeInvalidBearerToken)
package connectionevents

import (
	"context"
	"encoding/json"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd"
)

const (
	serverKindMCP             = "mcp"
	serverKindMeta            = "meta"
	serverKindToolset         = "toolset"
	serverKindProviderSession = "provider_session"
)

const (
	// maxProviderHostBytes is the DNS limit on a host name plus room for a port.
	maxProviderHostBytes = 261

	// maxAppKeyBytes bounds a client metadata URL. Real ones are under 200 bytes.
	maxAppKeyBytes = 512
)

// App identifies the app behind an attempt. A DCR client ID is per install and
// an unvalidated client_id is caller-chosen, so neither is stored: app_key is a
// rollup sort key and has to stay small.
type App struct{ key string }

var (
	// AppDCR is any dynamically registered app.
	AppDCR = App{"dcr"}

	// AppFirstParty is the Speakeasy dashboard.
	AppFirstParty = App{"first_party"}

	// AppUnverified is a client that has not been validated yet.
	AppUnverified = App{"unverified_client"}

	// AppWorkload is a workload presenting its own assertion.
	AppWorkload = App{"workload"}

	// AppBackground is a background job acting for a stored session.
	AppBackground = App{"background"}
)

// AppOf returns the App of a validated client_id: its client metadata URL
// without credentials, query or fragment, or AppDCR. An empty client_id is the
// dashboard. Use AppUnverified for a client not validated yet.
func AppOf(clientID string) App {
	if clientID == "" {
		return AppFirstParty
	}
	if !cimd.IsClientIDURL(clientID) {
		return AppDCR
	}
	parsed, err := url.Parse(clientID)
	if err != nil || parsed.Host == "" {
		return AppUnverified
	}
	key := "https://" + strings.ToLower(parsed.Host) + parsed.EscapedPath()
	if len(key) > maxAppKeyBytes {
		return AppUnverified
	}
	return App{key}
}

// Scope is what stays the same on every step of an attempt. Build it with one
// of the constructors. An empty organization or a nil server records nothing
// but a dropped-row count.
type Scope struct {
	organizationID string
	projectID      uuid.UUID
	serverKind     string
	serverID       uuid.UUID
	appKey         string
}

// ServerScope is an attempt on a project's MCP server.
func ServerScope(organizationID string, projectID, serverID uuid.UUID, app App) Scope {
	return Scope{organizationID, projectID, serverKindMCP, serverID, app.key}
}

// GatewayScope is an attempt on a project's gateway.
func GatewayScope(organizationID string, projectID, gatewayID uuid.UUID, app App) Scope {
	return Scope{organizationID, projectID, serverKindMeta, gatewayID, app.key}
}

// OrganizationGatewayScope is an attempt on an organization gateway, which
// belongs to no project.
func OrganizationGatewayScope(organizationID string, gatewayID uuid.UUID, app App) Scope {
	return Scope{organizationID, uuid.Nil, serverKindMeta, gatewayID, app.key}
}

// ToolsetScope is an attempt on a toolset endpoint with no server row.
func ToolsetScope(organizationID string, projectID, toolsetID uuid.UUID, app App) Scope {
	return Scope{organizationID, projectID, serverKindToolset, toolsetID, app.key}
}

// ProviderSessionScope is an attempt that reaches a provider and no server,
// such as a scheduled refresh. Its rows store an empty server_id.
func ProviderSessionScope(organizationID string, projectID uuid.UUID, app App) Scope {
	return Scope{organizationID, projectID, serverKindProviderSession, uuid.Nil, app.key}
}

func (s Scope) valid() bool {
	if s.organizationID == "" || s.serverKind == "" {
		return false
	}
	return s.serverID != uuid.Nil || s.serverKind == serverKindProviderSession
}

type scopeKey struct{}

// WithScope marks a request's scope for code that has no endpoint in hand.
func WithScope(ctx context.Context, scope Scope) context.Context {
	return context.WithValue(ctx, scopeKey{}, scope)
}

// ScopeFrom returns the request's scope, or fallback when there is none.
func ScopeFrom(ctx context.Context, fallback Scope) Scope {
	if scope, ok := ctx.Value(scopeKey{}).(Scope); ok {
		return scope
	}
	return fallback
}

// Attempt is one connection attempt. It travels in sign-in state as one value
// and its fields are set once, so a later step cannot stamp a different server,
// project or start time.
type Attempt struct {
	scope     Scope
	id        uuid.UUID
	startedAt time.Time
}

// Begin starts an attempt: its clock starts now.
func Begin(scope Scope) Attempt {
	return AttemptAt(uuid.New(), time.Now(), scope)
}

// AttemptAt is an attempt with a chosen id and start, for seed data and tests.
func AttemptAt(id uuid.UUID, startedAt time.Time, scope Scope) Attempt {
	return Attempt{scope: scope, id: id, startedAt: startedAt.UTC()}
}

// ID is shared by every step.
func (a Attempt) ID() uuid.UUID { return a.id }

// StartedAt is when the attempt started.
func (a Attempt) StartedAt() time.Time { return a.startedAt }

// IsZero reports an attempt that was never started, such as one read from
// sign-in state written before attempts existed.
func (a Attempt) IsZero() bool { return a.id == uuid.Nil }

// OrBegin returns the attempt, or begins one when it is zero.
func (a Attempt) OrBegin(scope Scope) Attempt {
	if !a.IsZero() {
		return a
	}
	return Begin(scope)
}

// attemptJSON is how an attempt is stored in sign-in state.
type attemptJSON struct {
	ID             uuid.UUID `json:"id"`
	StartedAt      time.Time `json:"started_at"`
	OrganizationID string    `json:"organization_id"`
	ProjectID      uuid.UUID `json:"project_id"`
	ServerKind     string    `json:"server_kind"`
	ServerID       uuid.UUID `json:"server_id"`
	AppKey         string    `json:"app_key"`
}

// MarshalJSON implements json.Marshaler.
func (a Attempt) MarshalJSON() ([]byte, error) {
	//nolint:wrapcheck // encoding/json adds the context
	return json.Marshal(attemptJSON{
		ID:             a.id,
		StartedAt:      a.startedAt,
		OrganizationID: a.scope.organizationID,
		ProjectID:      a.scope.projectID,
		ServerKind:     a.scope.serverKind,
		ServerID:       a.scope.serverID,
		AppKey:         a.scope.appKey,
	})
}

// UnmarshalJSON implements json.Unmarshaler. A value it cannot read becomes a
// zero attempt rather than an error, because an error would fail the sign-in
// whose state carries it.
func (a *Attempt) UnmarshalJSON(data []byte) error {
	var stored attemptJSON
	if err := json.Unmarshal(data, &stored); err != nil {
		*a = Attempt{scope: Scope{}, id: uuid.Nil, startedAt: time.Time{}} //nolint:exhaustruct // the zero scope
		return nil
	}
	*a = Attempt{
		scope:     Scope{stored.OrganizationID, stored.ProjectID, stored.ServerKind, stored.ServerID, stored.AppKey},
		id:        stored.ID,
		startedAt: stored.StartedAt,
	}
	return nil
}

// MarshalBinary implements encoding.BinaryMarshaler, which is what the Redis
// cache encodes sign-in state with. Its encoding skips unexported fields.
func (a Attempt) MarshalBinary() ([]byte, error) {
	if a.IsZero() {
		return nil, nil
	}
	return a.MarshalJSON()
}

// UnmarshalBinary implements encoding.BinaryUnmarshaler. As with UnmarshalJSON,
// a value it cannot read becomes a zero attempt.
func (a *Attempt) UnmarshalBinary(data []byte) error {
	return a.UnmarshalJSON(data)
}

// NoAttempt is the zero Attempt, for flows that begin one later.
var NoAttempt = Attempt{
	scope:     Scope{organizationID: "", projectID: uuid.Nil, serverKind: "", serverID: uuid.Nil, appKey: ""},
	id:        uuid.Nil,
	startedAt: time.Time{},
}

// NormalizeProviderHost reduces an issuer URL or bare host to the host people
// and rollups group by: lowercase, no scheme, path, credentials, trailing dot
// or default port. A tenant subdomain stays. It returns "" for input with no
// usable host, and normalizing twice changes nothing.
func NormalizeProviderHost(issuer string) string {
	issuer = strings.TrimSpace(issuer)
	if issuer == "" {
		return ""
	}
	if !strings.Contains(issuer, "://") {
		issuer = "https://" + issuer
	}
	parsed, err := url.Parse(issuer)
	if err != nil {
		return ""
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if host == "" {
		return ""
	}
	// 443 and 80 both drop on any scheme: an output is reparsed as https, so
	// keeping http's :443 would normalize differently the second time.
	port := parsed.Port()
	if port == "443" || port == "80" {
		port = ""
	}
	switch {
	case port != "":
		host = net.JoinHostPort(host, port)
	case strings.Contains(host, ":"):
		host = "[" + host + "]"
	}
	if len(host) > maxProviderHostBytes {
		return ""
	}
	return host
}

// String is the stored app_key.
func (a App) String() string { return a.key }
