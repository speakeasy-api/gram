// Package oauthui renders browser-facing OAuth responses shared by the
// Speakeasy authorization servers.
package oauthui

import (
	"bytes"
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/consentpage"
	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
)

//go:embed authorization_error.html
var authorizationErrorHTML string

// Script is the authorization error page's only script, the technical details
// copy button. Tests allowlist this exact text to prove the page never
// navigates on its own.
//
//go:embed authorization_error_copy.js
var Script string

var authorizationErrorTemplate = template.Must(template.Must(template.New("authorization-error").Parse(authorizationErrorHTML)).Parse(consentpage.LogoTemplate))

// AuthorizationError describes an authorization request that stopped before
// the person signed in. The page explains it in their terms; the OAuth wire
// values only appear under Technical details.
type AuthorizationError struct {
	// CallbackURL is the client's registered redirect URI with the error
	// parameters already applied, the same URL an automatic redirect would use.
	CallbackURL string

	// Code is the OAuth error code. It selects the page's explanation.
	Code string

	// Description is the OAuth error_description, written for developers.
	Description string

	// ClientName is the MCP client's display name, as on the consent page.
	// Empty when the client has none.
	ClientName string

	// ClientID is the OAuth client_id the request presented.
	ClientID string

	// ClientIDOrigin is the host a client metadata document was fetched from,
	// the one part of a CIMD client's identity it cannot choose. Empty for
	// other clients.
	ClientIDOrigin string

	// ServerName names the MCP server the client was connecting to, as on the
	// consent page. Empty when the request did not identify one.
	ServerName string
}

// errorKind groups OAuth error codes by who can act on them, which is what
// the person reading the page needs to know.
type errorKind string

const (
	// errorKindIncompatible: the MCP client sent a request this server does
	// not support. Its developer has to fix it.
	errorKindIncompatible errorKind = "incompatible"
	// errorKindWrongServer: the client asked for a different resource than
	// this server, usually a misconfigured server URL.
	errorKindWrongServer errorKind = "wrong-server"
	// errorKindNotAllowed: the client or person is refused by policy, which
	// the organization's administrators control.
	errorKindNotAllowed errorKind = "not-allowed"
	// errorKindUnavailable: a Speakeasy-side failure that retrying may clear.
	errorKindUnavailable errorKind = "unavailable"
	// errorKindOther: any code not classified above.
	errorKindOther errorKind = "other"
)

func classifyErrorCode(code string) errorKind {
	switch code {
	case oautherr.CodeInvalidRequest, oautherr.CodeUnsupportedResponseType, oautherr.CodeInvalidScope:
		return errorKindIncompatible
	case oautherr.CodeInvalidTarget:
		return errorKindWrongServer
	case oautherr.CodeAccessDenied, oautherr.CodeInvalidClient, oautherr.CodeUnauthorizedClient:
		return errorKindNotAllowed
	case oautherr.CodeServerError, oautherr.CodeTemporarilyUnavailable:
		return errorKindUnavailable
	default:
		return errorKindOther
	}
}

type authorizationErrorView struct {
	// Styles is the shared authorization page stylesheet.
	Styles template.CSS

	// Kind selects the explanation and next step.
	Kind errorKind

	// ClientName, ServerName, ClientID and ClientIDOrigin mirror
	// AuthorizationError.
	ClientName     string
	ServerName     string
	ClientID       string
	ClientIDOrigin string

	// Code and Description are the OAuth error, shown under Technical details.
	Code        string
	Description string

	// CallbackURL carries the same parameters as the server's error redirect.
	CallbackURL template.URL

	// Destination identifies the callback host or native application scheme.
	Destination string

	// Reference is the request's trace ID, which administrators and support
	// can look up. Empty when the request is not traced.
	Reference string

	// Time is when the request was refused, in UTC.
	Time string

	// TechnicalDetails is the plain-text copy of the technical details, with
	// the client and server names the panel leaves to the page title, so it
	// reads on its own when pasted into a ticket.
	TechnicalDetails string

	// ScriptNonce allows the page's one inline script, the copy button.
	ScriptNonce string

	// Script is the copy button script, embedded from a reviewed file.
	Script template.JS
}

func technicalDetailsText(view authorizationErrorView) string {
	var text strings.Builder
	text.WriteString("Speakeasy authorization error")
	for _, field := range []struct{ label, value string }{
		{"Reference", view.Reference},
		{"Time", view.Time},
		{"Error", view.Code},
		{"Error description", view.Description},
		{"MCP client", view.ClientName},
		{"MCP client ID", view.ClientID},
		{"Client verified from", view.ClientIDOrigin},
		{"MCP server", view.ServerName},
		{"Returns to", view.Destination},
	} {
		if field.value != "" {
			fmt.Fprintf(&text, "\n%s: %s", field.label, field.value)
		}
	}
	return text.String()
}

// WriteAuthorizationError renders a terminal 400 page with an explicit callback
// link. Callers must build CallbackURL only after matching the client's registered
// redirect URI. Scheme validation here protects the HTML link even for stored
// registrations created before the current redirect validation rules.
func WriteAuthorizationError(ctx context.Context, w http.ResponseWriter, page AuthorizationError) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("render authorization error: %w", err)
	}
	if err := oauthwire.ValidateRedirectURI(page.CallbackURL); err != nil {
		return fmt.Errorf("validate authorization error callback: %w", err)
	}
	uri, err := url.Parse(page.CallbackURL)
	if err != nil {
		return fmt.Errorf("parse authorization error callback: %w", err)
	}
	destination := uri.Host
	if uri.Scheme != "http" && uri.Scheme != "https" {
		destination = uri.Scheme + ":"
		if uri.Host != "" {
			destination += "//" + uri.Host
		}
	}
	reference := ""
	if traceID := trace.SpanContextFromContext(ctx).TraceID(); traceID.IsValid() {
		reference = traceID.String()
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return fmt.Errorf("generate authorization error script nonce: %w", err)
	}
	view := authorizationErrorView{
		Styles:         consentpage.Styles,
		Kind:           classifyErrorCode(page.Code),
		ClientName:     page.ClientName,
		ServerName:     page.ServerName,
		ClientID:       page.ClientID,
		ClientIDOrigin: page.ClientIDOrigin,
		Code:           page.Code,
		Description:    page.Description,
		// Native application schemes require template.URL after scheme validation;
		// html/template still escapes the attribute's contents.
		CallbackURL: template.URL(page.CallbackURL), //nolint:gosec // G203: registered callback validated above; native schemes must remain clickable.
		Destination: destination,
		Reference:   reference,
		Time:        time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
		// Built from the other fields below.
		TechnicalDetails: "",
		ScriptNonce:      base64.RawURLEncoding.EncodeToString(nonce),
		Script:           template.JS(Script), //nolint:gosec // G203: embedded at build time from a reviewed file; no request data reaches it.
	}
	view.TechnicalDetails = technicalDetailsText(view)
	var body bytes.Buffer
	if err := authorizationErrorTemplate.Execute(&body, view); err != nil {
		return fmt.Errorf("render authorization error: %w", err)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Referrer-Policy", "no-referrer")
	// The nonce admits only the copy button script; the page still never
	// navigates on its own.
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; font-src 'self'; script-src 'nonce-"+view.ScriptNonce+"'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusBadRequest)
	if _, err := w.Write(body.Bytes()); err != nil {
		return fmt.Errorf("write authorization error: %w", err)
	}
	return nil
}
