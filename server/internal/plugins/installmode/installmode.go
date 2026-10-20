// Package installmode defines how the device agent installs a plugin for one
// assignment audience.
package installmode

import (
	"errors"
	"fmt"
)

// Mode is a plugin assignment's install mode, stored in
// plugin_assignments.install_mode.
type Mode string

const (
	// Required plugins are installed and the user can't turn them off.
	Required Mode = "required"

	// Default plugins are installed and the user can turn them off.
	Default Mode = "default"

	// Available plugins are not installed until the user turns them on.
	Available Mode = "available"
)

var ErrInvalid = errors.New("invalid plugin install mode")

// Parse validates a stored or client-supplied mode.
func Parse(value string) (Mode, error) {
	switch mode := Mode(value); mode {
	case Required, Default, Available:
		return mode, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrInvalid, value)
	}
}

// FromStored reads plugin_assignments.install_mode. An unrecognized value reads
// as Default, matching how the device agent query ranks it.
func FromStored(value string) Mode {
	mode, err := Parse(value)
	if err != nil {
		return Default
	}
	return mode
}

// Values lists every mode, strictest first.
func Values() []Mode {
	return []Mode{Required, Default, Available}
}
