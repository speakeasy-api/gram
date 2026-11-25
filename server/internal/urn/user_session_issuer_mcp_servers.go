package urn

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

const userSessionIssuerMCPServersPrefix = "user_session_issuer_mcp_servers"

// UserSessionIssuerMCPServers names every MCP server attached to one user
// session issuer, as a single logical resource. It is the audience of a
// session a shared authorization server mints for a token request that names
// no RFC 8707 resource: the default resource indicator RFC 9068 §3 calls for,
// which each of the issuer's MCP servers recognises as its own.
type UserSessionIssuerMCPServers struct {
	// ID is the user session issuer.
	ID uuid.UUID

	checked bool
	err     error
}

func NewUserSessionIssuerMCPServers(id uuid.UUID) UserSessionIssuerMCPServers {
	a := UserSessionIssuerMCPServers{
		ID:      id,
		checked: false,
		err:     nil,
	}

	_ = a.validate()

	return a
}

func ParseUserSessionIssuerMCPServers(value string) (UserSessionIssuerMCPServers, error) {
	if value == "" {
		return UserSessionIssuerMCPServers{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" || strings.Contains(parts[1], delimiter) {
		return UserSessionIssuerMCPServers{}, fmt.Errorf("%w: expected two segments (%s:<uuid>)", ErrInvalid, userSessionIssuerMCPServersPrefix)
	}

	if parts[0] != userSessionIssuerMCPServersPrefix {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return UserSessionIssuerMCPServers{}, fmt.Errorf("%w: expected %s urn (got: %q)", ErrInvalid, userSessionIssuerMCPServersPrefix, truncated)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return UserSessionIssuerMCPServers{}, fmt.Errorf("%w: invalid %s uuid", ErrInvalid, userSessionIssuerMCPServersPrefix)
	}

	parsed := NewUserSessionIssuerMCPServers(id)
	if err := parsed.validate(); err != nil {
		return UserSessionIssuerMCPServers{}, err
	}

	return parsed, nil
}

func (u UserSessionIssuerMCPServers) IsZero() bool {
	return u.ID == uuid.Nil
}

func (u UserSessionIssuerMCPServers) String() string {
	return userSessionIssuerMCPServersPrefix + delimiter + u.ID.String()
}

func (u *UserSessionIssuerMCPServers) validate() error {
	if u.checked {
		return u.err
	}

	u.checked = true

	if u.ID == uuid.Nil {
		u.err = fmt.Errorf("%w: empty id", ErrInvalid)
		return u.err
	}

	return nil
}
