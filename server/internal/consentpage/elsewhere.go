package consentpage

import (
	"net/url"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd"
)

// FinishElsewhereField is the approve form's opt-in for a client that runs on
// another machine: a VM, a container or an SSH session. Such a client waits on
// a loopback address that this browser cannot reach, so redirecting there
// fails and the code never arrives.
const FinishElsewhereField = "finish_elsewhere"

// IsLoopbackRedirect reports whether redirectURI points at a loopback
// listener, which only a browser on the client's own machine can reach.
func IsLoopbackRedirect(redirectURI string) bool {
	u, err := url.Parse(redirectURI)
	return err == nil && cimd.IsLoopbackRedirectURI(u)
}

// FinishElsewhereRequested reports whether an approval should be handed to the
// person as a command to run on the client's machine instead of redirecting
// this browser. Only loopback redirect URIs qualify: any other redirect is one
// the browser can follow, so the field is ignored for it.
func FinishElsewhereRequested(form url.Values, redirectURI string) bool {
	return form.Get(FinishElsewhereField) == "1" && IsLoopbackRedirect(redirectURI)
}

// CallbackCommand delivers callbackURL to the client from its own machine.
// The URL is one single-quoted POSIX shell word: inside single quotes nothing
// is special except the quote itself, which is closed, escaped and reopened,
// so a client's own redirect URI cannot end the quoting and start a command.
func CallbackCommand(callbackURL string) string {
	return "curl -fsS -o /dev/null '" + strings.ReplaceAll(callbackURL, "'", `'\''`) + "'"
}
