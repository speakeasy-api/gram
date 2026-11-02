// Package mcpconnectionattempts records MCP connection attempts in ClickHouse: one
// row per step, shared by every flow. It holds metadata only and never holds a
// request up.
//
// A call site names what happened and nothing else:
//
//	attempt := mcpconnectionattempts.Begin(scope)
//	recorder.Step(ctx, attempt, mcpconnectionattempts.StepInboundAuthorize, mcpconnectionattempts.ResultAccepted)
//	recorder.End(ctx, attempt, mcpconnectionattempts.StepInboundConsent, mcpconnectionattempts.CodeCanceled)
//
//	recorder.Once(ctx, scope, mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeInvalidBearerToken)
package mcpconnectionattempts

import (
	"encoding/json"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd"
)

const (
	serverKindMCP              = "mcp"
	serverKindMeta             = "meta"
	serverKindToolset          = "toolset"
	serverKindProviderSession  = "provider_session"
	serverKindIdentityProvider = "identity_provider"
)

// maxProviderHostBytes is the DNS limit on a host name plus room for a port.
const maxProviderHostBytes = 261

// App identifies the app behind an attempt. app_key is a rollup sort key that a
// caller can reach before sign-in, so it holds a host or a kind and nothing else.
type App struct{ key string }

var (
	// AppDCR is any dynamically registered app.
	AppDCR = App{"dcr"}

	// AppFirstParty is the Speakeasy dashboard.
	AppFirstParty = App{"first_party"}

	// AppUnverified is a client that has not been validated yet.
	AppUnverified = App{"unverified_client"}

	// AppWorkload is a sign-in started for a workload's connection.
	AppWorkload = App{"workload"}

	// AppCodeMode is a sign-in started from a code mode connection link.
	AppCodeMode = App{"code_mode"}

	// AppRecoveryLink is a sign-in started from a link an assistant was given.
	AppRecoveryLink = App{"recovery_link"}
)

// AppOf returns the App of a validated client_id: the host of its client
// metadata URL, or AppDCR. An empty client_id is the dashboard.
func AppOf(clientID string) App {
	if clientID == "" {
		return AppFirstParty
	}
	if !cimd.IsClientIDURL(clientID) {
		return AppDCR
	}
	host := NormalizeProviderHost(clientID)
	if host == "" {
		return AppUnverified
	}
	return App{host}
}

// String is the stored app_key.
func (a App) String() string { return a.key }

// Scope is what stays the same on every step of an attempt. Build it with one
// of the constructors. An empty organization or nil server drops the row.
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

// IdentityProviderScope is an attempt that reaches an organization's identity
// provider and no server or project, such as renewing a company sign-in.
func IdentityProviderScope(organizationID string, app App) Scope {
	return Scope{organizationID, uuid.Nil, serverKindIdentityProvider, uuid.Nil, app.key}
}

func (s Scope) valid() bool {
	if s.organizationID == "" || s.serverKind == "" {
		return false
	}
	return s.serverID != uuid.Nil || s.serverKind == serverKindProviderSession || s.serverKind == serverKindIdentityProvider
}

// Attempt is one connection attempt. Its fields are set once, so every step stamps the same values.
type Attempt struct {
	scope     Scope
	id        uuid.UUID
	startedAt time.Time
}

// Begin starts an attempt: its clock starts now.
func Begin(scope Scope) Attempt {
	return AttemptAt(uuid.New(), time.Now(), scope)
}

// AttemptAt is an attempt with a known id and start: one derived from sign-in
// state, or seed data.
func AttemptAt(id uuid.UUID, startedAt time.Time, scope Scope) Attempt {
	return Attempt{scope: scope, id: id, startedAt: startedAt.UTC()}
}

// ID is shared by every step.
func (a Attempt) ID() uuid.UUID { return a.id }

// StartedAt is when the attempt started.
func (a Attempt) StartedAt() time.Time { return a.startedAt }

// IsZero reports an attempt that was never begun. Recording on it does nothing.
func (a Attempt) IsZero() bool { return a.id == uuid.Nil }

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

// UnmarshalJSON implements json.Unmarshaler. Unreadable values become a zero attempt, so they can't fail a sign-in.
func (a *Attempt) UnmarshalJSON(data []byte) error {
	var stored attemptJSON
	if err := json.Unmarshal(data, &stored); err != nil {
		*a = NoAttempt
		return nil
	}
	*a = Attempt{
		scope:     Scope{stored.OrganizationID, stored.ProjectID, stored.ServerKind, stored.ServerID, stored.AppKey},
		id:        stored.ID,
		startedAt: stored.StartedAt,
	}
	return nil
}

// MarshalBinary implements encoding.BinaryMarshaler. The Redis cache's msgpack skips unexported fields.
func (a Attempt) MarshalBinary() ([]byte, error) {
	if a.IsZero() {
		return nil, nil
	}
	return a.MarshalJSON()
}

// UnmarshalBinary implements encoding.BinaryUnmarshaler, like UnmarshalJSON.
func (a *Attempt) UnmarshalBinary(data []byte) error {
	return a.UnmarshalJSON(data)
}

// NoAttempt is the zero Attempt, for a login that is not a connection attempt.
var NoAttempt = Attempt{
	scope:     Scope{organizationID: "", projectID: uuid.Nil, serverKind: "", serverID: uuid.Nil, appKey: ""},
	id:        uuid.Nil,
	startedAt: time.Time{},
}

// NormalizeProviderHost reduces a URL or bare host to lowercase host, without scheme, path,
// credentials, trailing dot or port 80/443. A tenant subdomain stays. No usable host returns "".
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
	// Both drop on any scheme, so normalizing twice gives the same result.
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
