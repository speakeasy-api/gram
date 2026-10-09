package proxy

import (
	"errors"
	"fmt"
	"strings"
)

// EnvironmentHeaderPrefix marks an environment entry that an MCP server linked
// to the environment sends upstream as a header. The match is exact and
// case-sensitive, while every other entry in the environment is ignored, so
// linking an environment never exports variables that were not written for
// this purpose. The header name is the rest of the entry name with each
// underscore read as a dash, in canonical HTTP casing: MCP_HEADER_X-Instance-Url
// and MCP_HEADER_X_INSTANCE_URL are both sent as X-Instance-Url, the spelling
// upstreams that drop underscored header names accept.
const EnvironmentHeaderPrefix = "MCP_HEADER_"

// ErrInvalidEnvironmentHeader reports an environment entry that opted in with
// [EnvironmentHeaderPrefix] but cannot be sent. Serving refuses the request
// rather than falling back to the source's value for that header.
var ErrInvalidEnvironmentHeader = errors.New("invalid environment header")

// ErrUndecryptableEnvironmentHeader reports an opted-in environment entry whose
// stored value could not be decrypted.
var ErrUndecryptableEnvironmentHeader = errors.New("undecryptable environment header")

// EnvironmentHeaderStatus classifies one opted-in environment entry.
type EnvironmentHeaderStatus string

const (
	// EnvironmentHeaderMapped is an entry that is sent upstream.
	EnvironmentHeaderMapped EnvironmentHeaderStatus = "mapped"

	// EnvironmentHeaderInvalidName is an entry whose name after the prefix is
	// not an HTTP field name.
	EnvironmentHeaderInvalidName EnvironmentHeaderStatus = "invalid_name"

	// EnvironmentHeaderReserved is an entry naming a header that Speakeasy,
	// the HTTP transport or the MCP protocol owns.
	EnvironmentHeaderReserved EnvironmentHeaderStatus = "reserved"

	// EnvironmentHeaderEmptyValue is an entry whose value is empty or only
	// spaces and tabs, which the HTTP transport would send as nothing.
	EnvironmentHeaderEmptyValue EnvironmentHeaderStatus = "empty_value"

	// EnvironmentHeaderInvalidValue is an entry whose value contains bytes
	// that cannot appear in an HTTP field value.
	EnvironmentHeaderInvalidValue EnvironmentHeaderStatus = "invalid_value"

	// EnvironmentHeaderDuplicate is an entry naming the same header as
	// another entry, compared case-insensitively with underscores read as
	// dashes.
	EnvironmentHeaderDuplicate EnvironmentHeaderStatus = "duplicate"

	// EnvironmentHeaderUndecryptable is an entry whose stored value could not
	// be decrypted.
	EnvironmentHeaderUndecryptable EnvironmentHeaderStatus = "undecryptable"
)

// EnvironmentHeaderEntry is one environment entry offered for inspection.
type EnvironmentHeaderEntry struct {
	// Name is the environment entry name, including its prefix.
	Name string

	// Value is the decrypted entry value. It is ignored when Undecryptable
	// is set.
	Value string

	// Undecryptable reports that the stored value could not be decrypted.
	Undecryptable bool
}

// EnvironmentHeaderInspection is the outcome for one opted-in environment
// entry. It deliberately carries no exported value, so a report built from it
// cannot leak one.
type EnvironmentHeaderInspection struct {
	// EntryName is the environment entry name, including its prefix.
	EntryName string

	// HeaderName is the canonical header the entry targets. It is empty when
	// the name after the prefix is not an HTTP field name.
	HeaderName string

	// Status classifies the entry.
	Status EnvironmentHeaderStatus

	value string
}

// IsEnvironmentHeaderEntry reports whether an environment entry opted in to
// being sent upstream.
func IsEnvironmentHeaderEntry(name string) bool {
	return strings.HasPrefix(name, EnvironmentHeaderPrefix)
}

// InspectEnvironmentHeaders classifies every opted-in entry. Entries without
// [EnvironmentHeaderPrefix] are skipped. The result keeps the input order.
func InspectEnvironmentHeaders(entries []EnvironmentHeaderEntry) []EnvironmentHeaderInspection {
	inspections := make([]EnvironmentHeaderInspection, 0, len(entries))
	keyCounts := make(map[string]int, len(entries))
	for _, entry := range entries {
		if !IsEnvironmentHeaderEntry(entry.Name) {
			continue
		}
		inspection := inspectEnvironmentHeader(entry)
		if inspection.HeaderName != "" {
			keyCounts[headerKey(inspection.HeaderName)]++
		}
		inspections = append(inspections, inspection)
	}

	for i := range inspections {
		inspection := &inspections[i]
		if inspection.Status == EnvironmentHeaderMapped && keyCounts[headerKey(inspection.HeaderName)] > 1 {
			inspection.Status = EnvironmentHeaderDuplicate
		}
	}
	return inspections
}

func inspectEnvironmentHeader(entry EnvironmentHeaderEntry) EnvironmentHeaderInspection {
	inspection := EnvironmentHeaderInspection{
		EntryName:  entry.Name,
		HeaderName: "",
		Status:     EnvironmentHeaderMapped,
		value:      "",
	}

	suffix := strings.ReplaceAll(strings.TrimPrefix(entry.Name, EnvironmentHeaderPrefix), "_", "-")
	name, err := NormalizeHeaderName(suffix)
	// A name is never trimmed into validity: the entry must spell the header
	// exactly.
	if err == nil && suffix == strings.Trim(suffix, " ") {
		inspection.HeaderName = name
	}

	switch {
	case entry.Undecryptable:
		inspection.Status = EnvironmentHeaderUndecryptable
	case inspection.HeaderName == "":
		inspection.Status = EnvironmentHeaderInvalidName
	case isReservedTunneledDestination(inspection.HeaderName):
		inspection.Status = EnvironmentHeaderReserved
	case strings.Trim(entry.Value, " \t") == "":
		inspection.Status = EnvironmentHeaderEmptyValue
	case ValidateHeaderValue(entry.Value) != nil:
		inspection.Status = EnvironmentHeaderInvalidValue
	default:
		inspection.value = entry.Value
	}
	return inspection
}

// EnvironmentHeaderRows returns the configured headers to send for a set of
// inspections, or an error naming the first entry that cannot be sent. Errors
// name entries and statuses, never values. An undecryptable entry wraps
// [ErrUndecryptableEnvironmentHeader]; every other failure wraps
// [ErrInvalidEnvironmentHeader].
func EnvironmentHeaderRows(inspections []EnvironmentHeaderInspection) ([]ConfiguredHeader, error) {
	for _, inspection := range inspections {
		if inspection.Status == EnvironmentHeaderUndecryptable {
			return nil, fmt.Errorf("%w: environment entry %q", ErrUndecryptableEnvironmentHeader, inspection.EntryName)
		}
	}
	for _, inspection := range inspections {
		if inspection.Status != EnvironmentHeaderMapped {
			return nil, fmt.Errorf("%w: environment entry %q is %s", ErrInvalidEnvironmentHeader, inspection.EntryName, inspection.Status)
		}
	}

	rows := make([]ConfiguredHeader, 0, len(inspections))
	for _, inspection := range inspections {
		rows = append(rows, ConfiguredHeader{
			IsRequired:             true,
			Name:                   inspection.HeaderName,
			StaticValue:            inspection.value,
			ValueFromRequestHeader: "",
		})
	}
	return rows, nil
}

// SameHeaderField reports whether two header names address the same field at
// an upstream that reads names case-insensitively and treats underscores as
// dashes.
func SameHeaderField(a, b string) bool {
	return headerKey(a) == headerKey(b)
}
