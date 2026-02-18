package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/constants"
)

type Tool struct {
	Kind   ToolKind
	Source string
	Name   string

	checked bool
	err     error
}

func NewTool(kind ToolKind, source, name string) Tool {
	t := Tool{
		Kind:   kind,
		Source: source,
		Name:   name,

		checked: false,
		err:     nil,
	}

	_ = t.validate()

	return t
}

func ParseTool(value string) (Tool, error) {
	if value == "" {
		return Tool{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.SplitN(value, delimiter, 4)
	if len(parts) != 4 {
		return Tool{}, fmt.Errorf("%w: expected four segments", ErrInvalid)
	}

	if parts[0] != "tools" {
		truncated := parts[0][:min(maxSegmentLength, len(parts[0]))]
		return Tool{}, fmt.Errorf("%w: expected tools urn (got: %q)", ErrInvalid, truncated)
	}

	t := Tool{
		Kind:   ToolKind(parts[1]),
		Source: parts[2],
		Name:   parts[3],

		checked: false,
		err:     nil,
	}

	if err := t.validate(); err != nil {
		return Tool{}, err
	}

	return t, nil
}

func (u Tool) IsZero() bool {
	return u.Kind == "" && u.Source == "" && u.Name == ""
}

func (u Tool) String() string {
	return "tools" + delimiter + string(u.Kind) + delimiter + u.Source + delimiter + u.Name
}

func (u Tool) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("tool urn to json: %w", err)
	}

	return b, nil
}

func (u *Tool) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read tool urn string from json: %w", err)
	}

	parsed, err := ParseTool(s)
	if err != nil {
		return fmt.Errorf("parse tool urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *Tool) Scan(value any) error {
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
		return fmt.Errorf("cannot scan %T into Tool", value)
	}

	parsed, err := ParseTool(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u Tool) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u Tool) MarshalText() (text []byte, err error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal tool urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *Tool) UnmarshalText(text []byte) error {
	parsed, err := ParseTool(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal tool urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *Tool) validate() error {
	if u.checked {
		return u.err
	}

	u.checked = true

	parts := [][2]string{
		{"kind", string(u.Kind)},
		{"source", u.Source},
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

	if _, ok := toolKinds[u.Kind]; !ok {
		u.err = fmt.Errorf("%w: unknown tool kind: %q", ErrInvalid, u.Kind)
		return u.err
	}

	return nil
}
