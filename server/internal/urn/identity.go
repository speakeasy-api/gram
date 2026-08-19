package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"net/mail"
	"strings"

	"github.com/google/uuid"
)

// IdentityKind represents the kind segment of an Identity URN.
type IdentityKind string

const (
	// IdentityKindUser addresses a Gram user by users.id. Audit actors,
	// chats, user sessions, org members and plugin assignments all carry
	// this id, and it is the canonical kind whenever the subject has a Gram
	// user row.
	IdentityKindUser IdentityKind = "user"

	// IdentityKindEmail addresses a subject by email address. Telemetry and
	// cost aggregate by email, and MDM and shadow-MCP inventory report one,
	// so those surfaces address a subject this way and let resolution find
	// the user behind it.
	IdentityKindEmail IdentityKind = "email"

	// IdentityKindExternal addresses a subject by the external user id the
	// calling agent reported. Risk findings are keyed on it, and it is the
	// only identifier available for usage that matched no directory row.
	IdentityKindExternal IdentityKind = "external"

	// IdentityKindAPIKey addresses a non-human subject acting under an API
	// key, matching the apikey session subjects user sessions already emit.
	IdentityKindAPIKey IdentityKind = "apikey"

	// IdentityKindAgent addresses an agent identity. Nothing mints one yet;
	// it parses today so that links and routes minted now stay valid.
	IdentityKindAgent IdentityKind = "agent"
)

var identityKinds = map[IdentityKind]struct{}{
	IdentityKindUser:     {},
	IdentityKindEmail:    {},
	IdentityKindExternal: {},
	IdentityKindAPIKey:   {},
	IdentityKindAgent:    {},
}

// Identity is the URN that addresses one subject — a person or an agent —
// across every Gram subsystem. Format: `<kind>:<id>`, e.g. `user:user_01abc`
// or `agent:01998c1e-…`.
//
// Each subsystem records activity under whichever identifier it has: audit
// logs hold a Gram user id, telemetry holds an email, risk holds an external
// user id. This URN lets any of them address the same subject, and identity
// resolution reports which URN is canonical for it.
//
// Distinct from Principal: a Principal is grantable in RBAC, whereas an
// Identity only has to name a subject. `external` and `apikey` subjects can
// never hold a grant, so they must not widen the RBAC vocabulary.
type Identity struct {
	// Kind selects which identifier space ID belongs to.
	Kind IdentityKind

	// ID is the identifier itself.
	ID string

	checked bool
	err     error
}

// NewUserIdentity constructs a `user:<id>` identity URN.
func NewUserIdentity(id string) Identity {
	u := Identity{Kind: IdentityKindUser, ID: id, checked: false, err: nil}
	_ = u.validate()
	return u
}

// NewEmailIdentity constructs an `email:<address>` identity URN. The address
// is normalized so two links built from differently-cased addresses address
// the same subject.
func NewEmailIdentity(email string) Identity {
	u := Identity{Kind: IdentityKindEmail, ID: normalizeIdentityEmail(email), checked: false, err: nil}
	_ = u.validate()
	return u
}

// NewExternalIdentity constructs an `external:<id>` identity URN from the
// external user id an agent reported. External ids are opaque, so the id is
// kept verbatim.
func NewExternalIdentity(id string) Identity {
	u := Identity{Kind: IdentityKindExternal, ID: id, checked: false, err: nil}
	_ = u.validate()
	return u
}

// NewAgentIdentity constructs an `agent:<id>` identity URN.
func NewAgentIdentity(id string) Identity {
	u := Identity{Kind: IdentityKindAgent, ID: id, checked: false, err: nil}
	_ = u.validate()
	return u
}

// ParseIdentity parses a string of the form `<kind>:<id>` into an Identity.
// Email ids are normalized during parsing rather than rejected: these URNs
// arrive from links built all over the dashboard, and a differently-cased
// address names the same subject.
func ParseIdentity(value string) (Identity, error) {
	if value == "" {
		return Identity{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	// Split on the first delimiter only: external ids may contain colons.
	parts := strings.SplitN(value, delimiter, 2)
	if len(parts) != 2 || parts[1] == "" {
		return Identity{}, fmt.Errorf("%w: expected two segments (kind:id)", ErrInvalid)
	}

	u := Identity{
		Kind:    IdentityKind(parts[0]),
		ID:      parts[1],
		checked: false,
		err:     nil,
	}

	if u.Kind == IdentityKindEmail {
		u.ID = normalizeIdentityEmail(u.ID)
	}

	if err := u.validate(); err != nil {
		return Identity{}, err
	}

	return u, nil
}

func (u Identity) IsZero() bool {
	return u.Kind == "" && u.ID == ""
}

func (u Identity) String() string {
	return string(u.Kind) + delimiter + u.ID
}

func (u Identity) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("identity urn to json: %w", err)
	}

	return b, nil
}

func (u *Identity) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read identity urn string from json: %w", err)
	}

	parsed, err := ParseIdentity(s)
	if err != nil {
		return fmt.Errorf("parse identity urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *Identity) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into Identity", value)
	}

	parsed, err := ParseIdentity(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u Identity) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u Identity) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal identity urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *Identity) UnmarshalText(text []byte) error {
	parsed, err := ParseIdentity(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal identity urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *Identity) validate() error {
	if u.checked {
		return u.err
	}

	u.checked = true

	if u.Kind == "" {
		u.err = fmt.Errorf("%w: empty kind", ErrInvalid)
		return u.err
	}

	if _, ok := identityKinds[u.Kind]; !ok {
		u.err = fmt.Errorf("%w: unknown identity kind: %q", ErrInvalid, u.Kind)
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

	// Email and apikey ids address rows in stores that key on those exact
	// shapes, so a malformed one would resolve to an identity that can never
	// match anything. Both are validated the way Principal and SessionSubject
	// validate theirs.
	if u.Kind == IdentityKindEmail {
		addr, err := mail.ParseAddress(u.ID)
		if err != nil {
			u.err = fmt.Errorf("%w: invalid email identity id: %w", ErrInvalid, err)
			return u.err
		}
		if addr.Address != u.ID || addr.Name != "" {
			u.err = fmt.Errorf("%w: email identity id must be the bare address", ErrInvalid)
			return u.err
		}
	}

	if u.Kind == IdentityKindAPIKey {
		if _, err := uuid.Parse(u.ID); err != nil {
			u.err = fmt.Errorf("%w: apikey identity id must be a uuid", ErrInvalid)
			return u.err
		}
	}

	return nil
}

// normalizeIdentityEmail mirrors conv.NormalizeEmail, which the urn package
// cannot import without a dependency cycle.
func normalizeIdentityEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
