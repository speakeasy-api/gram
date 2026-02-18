package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/constants"
)

type FunctionRunnerKind string

const (
	FunctionRunnerKindLocal  FunctionRunnerKind = "local"
	FunctionRunnerKindFlyApp FunctionRunnerKind = "fly"
)

var functionRunnerKinds = map[FunctionRunnerKind]struct{}{
	FunctionRunnerKindLocal:  {},
	FunctionRunnerKindFlyApp: {},
}

type FunctionRunner struct {
	Kind    FunctionRunnerKind
	Tenancy string
	Name    string

	checked bool
	err     error
}

func NewFunctionRunner(kind FunctionRunnerKind, orgSlug, appName string) FunctionRunner {
	t := FunctionRunner{
		Kind:    kind,
		Tenancy: orgSlug,
		Name:    appName,

		checked: false,
		err:     nil,
	}

	_ = t.validate()

	return t
}

func newFunctionRunnerFromString(value string) (FunctionRunner, error) {
	var empty FunctionRunner

	if value == "" {
		return empty, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 4)
	if len(parts) != 4 {
		return empty, fmt.Errorf("%w: expected four segments", ErrInvalid)
	}

	if parts[0] != "gfr" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return empty, fmt.Errorf("%w: expected function runner urn (got: %q)", ErrInvalid, truncated)
	}

	t := NewFunctionRunner(FunctionRunnerKind(parts[1]), parts[2], parts[3])
	if t.err != nil {
		return empty, t.err
	}

	return t, nil
}

func (u FunctionRunner) IsZero() bool {
	return u.Kind == "" && u.Tenancy == "" && u.Name == ""
}

func (u FunctionRunner) String() string {
	return "gfr" + delimiter + string(u.Kind) + delimiter + u.Tenancy + delimiter + u.Name
}

func (u FunctionRunner) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("function runner urn to json: %w", err)
	}

	return b, nil
}

func (u *FunctionRunner) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read function runner urn string from json: %w", err)
	}

	parsed, err := newFunctionRunnerFromString(s)
	if err != nil {
		return fmt.Errorf("parse function runner urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *FunctionRunner) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into FunctionRunner", value)
	}

	parsed, err := newFunctionRunnerFromString(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u FunctionRunner) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u FunctionRunner) MarshalText() (text []byte, err error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal function runner urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *FunctionRunner) UnmarshalText(text []byte) error {
	parsed, err := newFunctionRunnerFromString(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal function runner text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *FunctionRunner) validate() error {
	if u.checked {
		return u.err
	}

	u.checked = true

	parts := [][2]string{
		{"kind", string(u.Kind)},
		{"org", u.Tenancy},
		{"name", u.Name},
	}

	for _, part := range parts {
		segment, v := part[0], part[1]
		if v == "" {
			u.err = fmt.Errorf("%w: empty %s", ErrInvalid, segment)
			return u.err
		}

		if len(v) > maxSegmentLength {
			u.err = fmt.Errorf("%w: %s segment is too long (max %d, got %d)", ErrInvalid, segment, maxSegmentLength, len(v))
			return u.err
		}

		if !constants.SlugPatternRE.MatchString(v) {
			u.err = fmt.Errorf("%w: disallowed characters in %s: %q", ErrInvalid, segment, v)
			return u.err
		}
	}

	if _, ok := functionRunnerKinds[u.Kind]; !ok {
		u.err = fmt.Errorf("%w: unknown function runner kind: %q", ErrInvalid, u.Kind)
		return u.err
	}

	return nil
}
