package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/constants"
)

// SpendRule is a versioned URN for a spend control rule, e.g.
// "spend_rule:eng-monthly-cap:v3". The slug names the rule (unique per
// organization, immutable after creation) and the version segment pins the
// exact rule configuration that produced an event so historical events remain
// interpretable after the rule is edited.
type SpendRule struct {
	Slug    string
	Version int64
}

func NewSpendRule(slug string, version int64) SpendRule {
	return SpendRule{
		Slug:    slug,
		Version: version,
	}
}

func ParseSpendRule(value string) (SpendRule, error) {
	if value == "" {
		return SpendRule{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 3)
	if len(parts) != 3 || parts[1] == "" || parts[2] == "" || strings.Contains(parts[2], delimiter) {
		return SpendRule{}, fmt.Errorf("%w: expected three segments (spend_rule:<slug>:v<version>)", ErrInvalid)
	}

	if parts[0] != "spend_rule" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return SpendRule{}, fmt.Errorf("%w: expected spend_rule urn (got: %q)", ErrInvalid, truncated)
	}

	if len(parts[2]) > maxSegmentLength {
		return SpendRule{}, fmt.Errorf("%w: version segment is too long", ErrInvalid)
	}

	versionText, ok := strings.CutPrefix(parts[2], "v")
	if !ok || versionText == "" {
		return SpendRule{}, fmt.Errorf("%w: invalid spend_rule version prefix", ErrInvalid)
	}

	version, err := strconv.ParseInt(versionText, 10, 64)
	if err != nil {
		return SpendRule{}, fmt.Errorf("%w: invalid spend_rule version", ErrInvalid)
	}

	parsed := NewSpendRule(parts[1], version)
	if err := parsed.validate(); err != nil {
		return SpendRule{}, err
	}

	return parsed, nil
}

func (u SpendRule) IsZero() bool {
	return u.Slug == "" && u.Version == 0
}

func (u SpendRule) String() string {
	return "spend_rule" + delimiter + u.Slug + delimiter + "v" + strconv.FormatInt(u.Version, 10)
}

func (u SpendRule) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("spend_rule urn to json: %w", err)
	}

	return b, nil
}

func (u *SpendRule) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read spend_rule urn string from json: %w", err)
	}

	parsed, err := ParseSpendRule(s)
	if err != nil {
		return fmt.Errorf("parse spend_rule urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *SpendRule) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into SpendRule", value)
	}

	parsed, err := ParseSpendRule(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u SpendRule) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u SpendRule) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal spend_rule urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *SpendRule) UnmarshalText(text []byte) error {
	parsed, err := ParseSpendRule(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal spend_rule urn text: %w", err)
	}

	*u = parsed

	return nil
}

// validate recomputes on every call (no caching) and takes a value receiver so
// the exported Slug/Version fields cannot be mutated after construction into a
// state that Value/MarshalJSON/MarshalText would then serialize unchecked.
func (u SpendRule) validate() error {
	if u.Slug == "" {
		return fmt.Errorf("%w: empty slug", ErrInvalid)
	}

	if !constants.SlugPatternRE.MatchString(u.Slug) {
		return fmt.Errorf("%w: disallowed characters in slug: %q", ErrInvalid, u.Slug[:min(maxSegmentLength, len(u.Slug))])
	}

	if u.Version < 1 {
		return fmt.Errorf("%w: version must be at least 1", ErrInvalid)
	}

	return nil
}
