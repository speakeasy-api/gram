package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// SessionSubjectKind represents the kind segment of a SessionSubject URN.
type SessionSubjectKind string

const (
	SessionSubjectKindUser      SessionSubjectKind = "user"
	SessionSubjectKindAPIKey    SessionSubjectKind = "apikey"
	SessionSubjectKindAnonymous SessionSubjectKind = "anonymous"
)

var sessionSubjectKinds = map[SessionSubjectKind]struct{}{
	SessionSubjectKindUser:      {},
	SessionSubjectKindAPIKey:    {},
	SessionSubjectKindAnonymous: {},
}

// SessionSubject is the URN that may appear as the `sub` claim of a
// Gram-issued session JWT. Format: `<kind>:<id>` where kind is exactly one of
// `user`, `apikey`, or `anonymous`.
//
// `role` is NOT a valid session subject — roles are not authentication
// principals; use urn.Principal for RBAC subjects.
type SessionSubject struct {
	Kind SessionSubjectKind
	ID   string

	checked bool
	err     error
}

// NewUserSubject constructs a `user:<id>` session subject.
func NewUserSubject(id string) SessionSubject {
	s := SessionSubject{Kind: SessionSubjectKindUser, ID: id, checked: false, err: nil}
	_ = s.validate()
	return s
}

// NewAPIKeySubject constructs an `apikey:<uuid>` session subject.
func NewAPIKeySubject(id uuid.UUID) SessionSubject {
	s := SessionSubject{Kind: SessionSubjectKindAPIKey, ID: id.String(), checked: false, err: nil}
	_ = s.validate()
	return s
}

// NewAnonymousSubject constructs an `anonymous:<mcp-session-id>` session
// subject. The id segment is the same value the MCP handler injects into the
// user_session_issuer per goal #11 of the RFC.
func NewAnonymousSubject(mcpSessionID string) SessionSubject {
	s := SessionSubject{Kind: SessionSubjectKindAnonymous, ID: mcpSessionID, checked: false, err: nil}
	_ = s.validate()
	return s
}

// ParseSessionSubject parses a string of the form `<kind>:<id>` into a
// SessionSubject.
func ParseSessionSubject(value string) (SessionSubject, error) {
	if value == "" {
		return SessionSubject{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" {
		return SessionSubject{}, fmt.Errorf("%w: expected two segments (kind:id)", ErrInvalid)
	}

	s := SessionSubject{
		Kind:    SessionSubjectKind(parts[0]),
		ID:      parts[1],
		checked: false,
		err:     nil,
	}

	if err := s.validate(); err != nil {
		return SessionSubject{}, err
	}

	return s, nil
}

func (u SessionSubject) IsZero() bool {
	return u.Kind == "" && u.ID == ""
}

func (u SessionSubject) String() string {
	return string(u.Kind) + delimiter + u.ID
}

func (u SessionSubject) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("session subject urn to json: %w", err)
	}

	return b, nil
}

func (u *SessionSubject) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read session subject urn string from json: %w", err)
	}

	parsed, err := ParseSessionSubject(s)
	if err != nil {
		return fmt.Errorf("parse session subject urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *SessionSubject) Scan(value any) error {
	if value == nil {
		return nil
	}

	var s string
	switch v := value.(type) {
	case string:
		s = v
	case []byte:
		s = string(v)
	default:
		return fmt.Errorf("cannot scan %T into SessionSubject", value)
	}

	parsed, err := ParseSessionSubject(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u SessionSubject) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u SessionSubject) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal session subject urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *SessionSubject) UnmarshalText(text []byte) error {
	parsed, err := ParseSessionSubject(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal session subject urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *SessionSubject) validate() error {
	if u.checked {
		return u.err
	}

	u.checked = true

	if u.Kind == "" {
		u.err = fmt.Errorf("%w: empty kind", ErrInvalid)
		return u.err
	}

	if _, ok := sessionSubjectKinds[u.Kind]; !ok {
		u.err = fmt.Errorf("%w: unknown session subject kind: %q", ErrInvalid, u.Kind)
		return u.err
	}

	if u.ID == "" {
		u.err = fmt.Errorf("%w: empty id", ErrInvalid)
		return u.err
	}

	if len(u.ID) > maxSegmentLength {
		u.err = fmt.Errorf("%w: id segment is too long (max %d, got %d)", ErrInvalid, maxSegmentLength, len(u.ID))
		return u.err
	}

	if u.Kind == SessionSubjectKindAPIKey {
		if _, parseErr := uuid.Parse(u.ID); parseErr != nil {
			u.err = fmt.Errorf("%w: apikey id must be a uuid", ErrInvalid)
			return u.err
		}
	}

	return nil
}
