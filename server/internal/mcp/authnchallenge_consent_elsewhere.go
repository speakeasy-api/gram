package mcp

import (
	_ "embed"
	"fmt"
	"html/template"
	"net/http"
	"net/url"

	"github.com/speakeasy-api/gram/server/internal/consentpage"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd"
)

// finishElsewhereField is the approve form's opt-in for a client that runs on
// another machine: a VM, a container or an SSH session. Such a client waits on
// a loopback address that this browser cannot reach, so redirecting there
// fails and the code never arrives.
const finishElsewhereField = "finish_elsewhere"

//go:embed consent_finish_elsewhere_template.html
var consentFinishElsewhereTemplateHTML string

var consentFinishElsewhereTemplate = template.Must(
	template.Must(template.New("finish-elsewhere").Parse(consentFinishElsewhereTemplateHTML)).Parse(consentpage.LogoTemplate),
)

// isLoopbackRedirect reports whether redirectURI points at a loopback
// listener, which only a browser on the client's own machine can reach.
func isLoopbackRedirect(redirectURI string) bool {
	u, err := url.Parse(redirectURI)
	return err == nil && cimd.IsLoopbackRedirectURI(u)
}

// finishElsewhereRequested reports whether an approval should be handed to the
// person as a command to run on the client's machine instead of redirecting
// this browser. Only loopback redirect URIs qualify: any other redirect is one
// the browser can follow, so the field is ignored for it.
func finishElsewhereRequested(form url.Values, redirectURI string) bool {
	return form.Get(finishElsewhereField) == "1" && isLoopbackRedirect(redirectURI)
}

// consentFinishElsewhereData is the field set the finish-elsewhere page
// renders against.
type consentFinishElsewhereData struct {
	// Styles is the compiled consent page stylesheet.
	Styles template.CSS

	// ClientName is the registered name of the client that was given access.
	ClientName string

	// ListenAddress is the host and port the client listens on, so the person
	// can tell which machine the command belongs to.
	ListenAddress string

	// CallbackURL is the authorization response the client is waiting for,
	// for clients that ask for the redirect address to be pasted in.
	CallbackURL string

	// Command delivers CallbackURL to the client from its own machine.
	Command string

	// CodeLifetime is how long the code in CallbackURL can be exchanged.
	CodeLifetime string

	// ScriptURL is the consent page script, which wires the copy buttons.
	ScriptURL string
}

// renderConsentFinishElsewhere shows the authorization response as a command
// to run where the client listens. Showing it is no more exposure than the
// redirect it replaces: the same URL would otherwise sit in this browser's
// address bar, and its code is bound to the client's PKCE verifier, so only the
// client that started the flow can redeem it.
func renderConsentFinishElsewhere(w http.ResponseWriter, clientName, callbackURL string) error {
	u, err := url.Parse(callbackURL)
	if err != nil {
		return fmt.Errorf("parse client callback: %w", err)
	}
	minutes := int(userSessionGrantTTL.Minutes())
	data := consentFinishElsewhereData{
		Styles:        consentpage.Styles,
		ClientName:    clientName,
		ListenAddress: u.Host,
		CallbackURL:   callbackURL,
		Command:       "curl -fsS -o /dev/null " + shellSingleQuote(callbackURL),
		CodeLifetime:  fmt.Sprintf("%d minutes", minutes),
		ScriptURL:     consentScriptURL,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// The page carries a live authorization code: never cache it, and never
	// leak it in a Referer to anything the page links to.
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
	w.Header().Set("X-Frame-Options", "DENY")
	if err := consentFinishElsewhereTemplate.Execute(w, data); err != nil {
		return fmt.Errorf("render finish elsewhere page: %w", err)
	}
	return nil
}
