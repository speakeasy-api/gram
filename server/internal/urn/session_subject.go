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
	// MaxSessionSubjectIDLength is the byte limit for the subject ID segment.
	MaxSessionSubjectIDLength = maxSegmentLength

	SessionSubjectKindUser      SessionSubjectKind = "user"
	SessionSubjectKindAPIKey    SessionSubjectKind = "apikey"
	SessionSubjectKindAgent     SessionSubjectKind = "agent"
	SessionSubjectKindAnonymous SessionSubjectKind = "anonymous"
	SessionSubjectKindWorkload  SessionSubjectKind = "workload"
)

// MaxWorkloadExternalSubjectLength is the room left for the external subject
// in a workload subject's id, after the issuer uuid and its delimiter. Exported
// so admission can reject an over-long subject where an operator can fix it,
// rather than at token exchange.
const MaxWorkloadExternalSubjectLength = MaxWorkloadSubjectIDLength - uuidStringLength - len(delimiter)

// MaxWorkloadSubjectIDLength is the byte limit for a workload subject's id. It
// exceeds MaxSessionSubjectIDLength because the platform picks the external
// subject, and an AWS IAM ARN can carry a 512-character path. It stays under
// Postgres's ~2.7 KB B-tree entry limit and common ~8 KB header limits.
const MaxWorkloadSubjectIDLength = 1024

// uuidStringLength is the length of a uuid in canonical text form.
const uuidStringLength = 36

var sessionSubjectKinds = map[SessionSubjectKind]struct{}{
	SessionSubjectKindUser:      {},
	SessionSubjectKindAPIKey:    {},
	SessionSubjectKindAgent:     {},
	SessionSubjectKindAnonymous: {},
	SessionSubjectKindWorkload:  {},
}

// SessionSubject is the URN that may appear as the `sub` claim of a
// Gram-issued session JWT. Format: `<kind>:<id>` where kind is exactly one of
// `user`, `apikey`, `agent`, `anonymous`, or `workload`.
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

// NewAgentSubject constructs an `agent:<uuid>` session subject.
func NewAgentSubject(id uuid.UUID) SessionSubject {
	s := SessionSubject{Kind: SessionSubjectKindAgent, ID: id.String(), checked: false, err: nil}
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

// NewWorkloadSubject constructs a
// `workload:<workloadIssuerID>:<externalSubject>` session subject.
//
// A `sub` is unique only within its issuer, so the issuer is part of the
// identity. It is named by its workload_issuers row id rather than its URL, so
// the identity survives a discovery refresh or URL edit; re-registering an
// issuer is a new identity.
func NewWorkloadSubject(workloadIssuerID uuid.UUID, externalSubject string) SessionSubject {
	s := SessionSubject{
		Kind:    SessionSubjectKindWorkload,
		ID:      workloadIssuerID.String() + delimiter + externalSubject,
		checked: false,
		err:     nil,
	}
	_ = s.validate()
	return s
}

// Workload splits a `workload:` subject into its issuer id and external
// subject. It returns an error for any other kind.
func (u SessionSubject) Workload() (uuid.UUID, string, error) {
	if err := u.validate(); err != nil {
		return uuid.Nil, "", err
	}
	if u.Kind != SessionSubjectKindWorkload {
		return uuid.Nil, "", fmt.Errorf("%w: not a workload subject: %q", ErrInvalid, u.Kind)
	}

	issuerID, externalSubject, err := splitWorkloadID(u.ID)
	if err != nil {
		return uuid.Nil, "", err
	}

	return issuerID, externalSubject, nil
}

// splitWorkloadID splits a workload id on the first delimiter only, since
// external subjects often contain colons (`repo:owner/name:ref:refs/heads/main`).
func splitWorkloadID(id string) (uuid.UUID, string, error) {
	parts := strings.SplitN(id, delimiter, 2)
	if len(parts) != 2 {
		return uuid.Nil, "", fmt.Errorf("%w: workload id must be <issuer-id>:<external-subject>", ErrInvalid)
	}
	issuerPart, externalSubject := parts[0], parts[1]

	// Canonical form only: uuid.Parse also accepts uppercase and braced forms,
	// which would give one workload several strings that compare unequal.
	issuerID, err := uuid.Parse(issuerPart)
	if err != nil || issuerID.String() != issuerPart {
		return uuid.Nil, "", fmt.Errorf("%w: workload issuer reference must be a canonical uuid", ErrInvalid)
	}
	if issuerID == uuid.Nil {
		// The nil uuid parses but names no workload_issuers row.
		return uuid.Nil, "", fmt.Errorf("%w: workload issuer reference is the nil uuid", ErrInvalid)
	}
	if externalSubject == "" {
		return uuid.Nil, "", fmt.Errorf("%w: workload external subject is empty", ErrInvalid)
	}

	return issuerID, externalSubject, nil
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

	maxIDLength := MaxSessionSubjectIDLength
	if u.Kind == SessionSubjectKindWorkload {
		maxIDLength = MaxWorkloadSubjectIDLength
	}
	if len(u.ID) > maxIDLength {
		u.err = fmt.Errorf("%w: id segment is too long (max %d, got %d)", ErrInvalid, maxIDLength, len(u.ID))
		return u.err
	}

	if u.Kind == SessionSubjectKindAPIKey || u.Kind == SessionSubjectKindAgent {
		id, parseErr := uuid.Parse(u.ID)
		if parseErr != nil {
			u.err = fmt.Errorf("%w: %s id must be a uuid", ErrInvalid, u.Kind)
			return u.err
		}
		if u.Kind == SessionSubjectKindAgent && id.String() != u.ID {
			u.err = fmt.Errorf("%w: agent id must use canonical uuid format", ErrInvalid)
			return u.err
		}
	}

	if u.Kind == SessionSubjectKindWorkload {
		// An id that does not split into an issuer and a subject cannot say
		// which issuer vouched for the workload.
		if _, _, splitErr := splitWorkloadID(u.ID); splitErr != nil {
			u.err = splitErr
			return u.err
		}
	}

	return nil
}
