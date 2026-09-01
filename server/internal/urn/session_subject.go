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
	SessionSubjectKindAnonymous SessionSubjectKind = "anonymous"
	SessionSubjectKindWorkload  SessionSubjectKind = "workload"
)

// MaxWorkloadExternalSubjectLength is how much of a workload subject's id
// segment is left for the external subject once the issuer reference and its
// delimiter are accounted for.
//
// Exported because the useful place to enforce it is where an operator admits
// a workload identity, not where a session is minted. A subject too long to
// fit is a configuration problem, and rejecting it at admission puts the error
// in front of the person who can shorten it; discovering it at token exchange
// instead produces a workload that authenticates correctly and then cannot
// hold a session, with nothing in the failure naming the cause.
const MaxWorkloadExternalSubjectLength = MaxSessionSubjectIDLength - uuidStringLength - len(delimiter)

// uuidStringLength is the width of a uuid in its canonical text form, so the
// budget above is derived rather than written down as a number.
const uuidStringLength = 36

var sessionSubjectKinds = map[SessionSubjectKind]struct{}{
	SessionSubjectKindUser:      {},
	SessionSubjectKindAPIKey:    {},
	SessionSubjectKindAnonymous: {},
	SessionSubjectKindWorkload:  {},
}

// SessionSubject is the URN that may appear as the `sub` claim of a
// Gram-issued session JWT. Format: `<kind>:<id>` where kind is exactly one of
// `user`, `apikey`, `anonymous`, or `workload`.
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

// NewWorkloadSubject constructs a
// `workload:<remoteSessionIssuerID>:<externalSubject>` session subject.
//
// Both halves are load-bearing. A `sub` is unique within the issuer that
// minted it and never across issuers, so an identity carrying only the
// external subject would let two workloads vouched for by two different
// issuers collide — one machine's session, grants, and audit trail attributed
// to another. The issuer is referenced by its remote_session_issuers row id
// rather than its URL: that URL is deliberately non-unique across the tri-tier
// catalog, so it does not identify the trust decision that admitted the
// request, and it is unbounded in length where a uuid is not.
func NewWorkloadSubject(remoteSessionIssuerID uuid.UUID, externalSubject string) SessionSubject {
	s := SessionSubject{
		Kind:    SessionSubjectKindWorkload,
		ID:      remoteSessionIssuerID.String() + delimiter + externalSubject,
		checked: false,
		err:     nil,
	}
	_ = s.validate()
	return s
}

// Workload splits a `workload:` subject back into the issuer it was vouched
// for by and the external subject that issuer asserted. It reports an error
// for any other kind, so a caller cannot read a user or api key subject as a
// workload by accident.
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

// splitWorkloadID parses a workload id segment into its two halves. Split on
// the first delimiter only: external subjects from these platforms are
// themselves colon-heavy (`repo:owner/name:ref:refs/heads/main`), so
// everything after the issuer reference belongs to the subject verbatim.
func splitWorkloadID(id string) (uuid.UUID, string, error) {
	issuerPart, externalSubject, found := strings.Cut(id, delimiter)
	if !found {
		return uuid.Nil, "", fmt.Errorf("%w: workload id must be <issuer-id>:<external-subject>", ErrInvalid)
	}

	issuerID, err := uuid.Parse(issuerPart)
	if err != nil {
		return uuid.Nil, "", fmt.Errorf("%w: workload issuer reference must be a uuid", ErrInvalid)
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

	if len(u.ID) > MaxSessionSubjectIDLength {
		u.err = fmt.Errorf("%w: id segment is too long (max %d, got %d)", ErrInvalid, MaxSessionSubjectIDLength, len(u.ID))
		return u.err
	}

	if u.Kind == SessionSubjectKindAPIKey {
		if _, parseErr := uuid.Parse(u.ID); parseErr != nil {
			u.err = fmt.Errorf("%w: apikey id must be a uuid", ErrInvalid)
			return u.err
		}
	}

	if u.Kind == SessionSubjectKindWorkload {
		// Structural, not cosmetic: an id that does not split into an issuer
		// and a subject cannot say which issuer vouched for the workload, and
		// a subject that carries no issuer is exactly the collision this kind
		// exists to prevent.
		if _, _, splitErr := splitWorkloadID(u.ID); splitErr != nil {
			u.err = splitErr
			return u.err
		}
	}

	return nil
}
