// Package slackscopes names the bot scopes the Speakeasy Slack app asks for
// and reports which of them an install is missing.
package slackscopes

import "slices"

const (
	// UsersRead lets directory sync list workspace members.
	UsersRead = "users:read"

	// UsersReadEmail lets directory sync read member emails, which is how
	// members are matched to people.
	UsersReadEmail = "users:read.email"

	// ChatWrite lets the app send approval requests to approvers as DMs.
	ChatWrite = "chat:write"
)

// Directory are the scopes an install cannot connect without.
var Directory = []string{UsersRead, UsersReadEmail}

// Requested are the scopes every install asks for. Slack grants scopes at
// install time only, so a workspace installed before a scope joined this list
// keeps working without it until an admin reinstalls the app.
var Requested = []string{UsersRead, UsersReadEmail, ChatWrite}

// Missing lists the requested scopes an install has not granted, in the
// order they are requested.
func Missing(granted []string) []string {
	missing := []string{}
	for _, scope := range Requested {
		if !slices.Contains(granted, scope) {
			missing = append(missing, scope)
		}
	}
	return missing
}
