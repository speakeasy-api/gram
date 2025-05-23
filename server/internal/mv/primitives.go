package mv

import (
	"maps"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/gen/types"
)

type ProjectID uuid.UUID
type DeploymentID uuid.UUID
type ToolsetSlug types.Slug
type ToolID uuid.UUID

type Confirm string

const (
	// ConfirmAlways is the default confirmation mode and means that a client
	// must always request user confirmation before a tool call.
	ConfirmAlways Confirm = "always"
	// ConfirmSession is a confirmation mode that means that a client must
	// request user confirmation the first time a tool is called in a chat
	// session. It also implies that the user can also confirm a single tool
	// call i.e. "Allow once" and "Allow for this chat" fall under this mode.
	ConfirmSession Confirm = "session"
	// ConfirmNever is a confirmation mode that means no user confirmation is
	// needed to perform a given tool call.
	ConfirmNever Confirm = "never"
)

var ConfirmValues = slices.Sorted(maps.Values(map[Confirm]string{
	ConfirmAlways:  string(ConfirmAlways),
	ConfirmSession: string(ConfirmSession),
	ConfirmNever:   string(ConfirmNever),
}))

func SanitizeConfirm(confirm string) (Confirm, bool) {
	switch strings.ToLower(confirm) {
	case "always":
		return ConfirmAlways, true
	case "session":
		return ConfirmSession, true
	case "never":
		return ConfirmNever, true
	default:
		return ConfirmAlways, false
	}
}

func SanitizeConfirmPtr(confirm *string) (Confirm, bool) {
	if confirm == nil {
		return ConfirmAlways, true
	}

	return SanitizeConfirm(*confirm)
}

func (c Confirm) IsValid() bool {
	_, ok := SanitizeConfirm(string(c))
	return ok
}
