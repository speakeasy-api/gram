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
	"html"
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

//go:embed authorization_error_fallback.html
var authorizationErrorFallbackHTML string

// Pre-render the static shell so the fallback needs no template execution,
// script nonce, or request context when the normal renderer fails.
var authorizationErrorFallbackStart, authorizationErrorFallbackEnd = func() (string, string) {
	tmpl := template.Must(template.Must(template.New("authorization-error-fallback").Parse(authorizationErrorFallbackHTML)).Parse(consentpage.LogoTemplate))
	var start, end bytes.Buffer
	if err := tmpl.ExecuteTemplate(&start, "start", consentpage.Styles); err != nil {
		panic(fmt.Sprintf("render authorization error fallback start: %v", err))
	}
	if err := tmpl.ExecuteTemplate(&end, "end", nil); err != nil {
		panic(fmt.Sprintf("render authorization error fallback end: %v", err))
	}
	return start.String(), end.String()
}()

// AuthorizationError describes a refused browser authorization or approval
// request. Protocol values only appear under Technical details.
type AuthorizationError struct {
	// Status is the original HTTP failure status. Zero defaults to 400.
	Status int

	// Reason selects an explanation independently of the public error code.
	Reason Reason

	// Approval identifies a staff change approval rather than a connection.
	Approval bool

	// ErrorID is the public service error identifier, when available.
	ErrorID string

	// CallbackURL is the client's registered redirect URI with the error
	// parameters already applied, the same URL an automatic redirect would use.
	// Empty when no validated callback is available.
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

// Reason describes what the person can do about a browser failure.
type Reason string

const (
	// ReasonDefault derives the explanation from the error code and status.
	ReasonDefault Reason = ""
	// ReasonOrganizationBlocked identifies an explicit organization policy refusal.
	ReasonOrganizationBlocked Reason = "organization-blocked"
	// ReasonExpired identifies a link that expired or was already used.
	ReasonExpired Reason = "expired"
	// ReasonSessionChanged identifies an invalid browser or changed sign-in session.
	ReasonSessionChanged Reason = "session-changed"
	// ReasonInvalidClient identifies an unknown or invalid client registration.
	ReasonInvalidClient Reason = "invalid-client"
	// ReasonInvalidLink identifies invalid authorization parameters.
	ReasonInvalidLink Reason = "invalid-link"
	// ReasonUnavailable identifies a dependency failure independently of HTTP status.
	ReasonUnavailable Reason = "unavailable"
)

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

func classifyError(page AuthorizationError) errorKind {
	if page.Reason != ReasonDefault {
		return errorKind(page.Reason)
	}
	if page.Status >= http.StatusInternalServerError {
		return errorKindUnavailable
	}
	if page.Status == http.StatusForbidden || page.Status == http.StatusUnauthorized {
		return errorKindNotAllowed
	}
	code := page.Code
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
	// Approval selects staff change approval wording.
	Approval bool

	// ErrorID preserves the public service error identifier.
	ErrorID string

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
		{"Error ID", view.ErrorID},
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

// WriteAuthorizationError renders a terminal error page with an optional callback
// link. Rendering failures produce a static branded page before returning the error.
// Callers must build CallbackURL only after matching the client's registered
// redirect URI. Scheme validation here protects the HTML link even for stored
// registrations created before the current redirect validation rules.
func WriteAuthorizationError(ctx context.Context, w http.ResponseWriter, page AuthorizationError) (err error) {
	status := page.Status
	if status == 0 {
		status = http.StatusBadRequest
	}
	committed := false
	defer func() {
		if err != nil && !committed {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Pragma", "no-cache")
			w.Header().Set("Referrer-Policy", "no-referrer")
			w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; font-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.WriteHeader(status)
			_, _ = fmt.Fprintf(w, "%s<p class=\"font-mono text-sm break-all\">%s</p><p class=\"text-sm text-muted-foreground break-words\">%s</p>%s", authorizationErrorFallbackStart, html.EscapeString(page.Code), html.EscapeString(page.Description), authorizationErrorFallbackEnd)
		}
	}()
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("render authorization error: %w", err)
	}
	destination := ""
	if page.CallbackURL != "" {
		if err := oauthwire.ValidateRedirectURI(page.CallbackURL); err != nil {
			return fmt.Errorf("validate authorization error callback: %w", err)
		}
		uri, err := url.Parse(page.CallbackURL)
		if err != nil {
			return fmt.Errorf("parse authorization error callback: %w", err)
		}
		destination = uri.Host
		if uri.Scheme != "http" && uri.Scheme != "https" {
			destination = uri.Scheme + ":"
			if uri.Host != "" {
				destination += "//" + uri.Host
			}
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
		Approval:       page.Approval,
		ErrorID:        page.ErrorID,
		Kind:           classifyError(page),
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
	committed = true
	w.WriteHeader(status)
	if _, err := w.Write(body.Bytes()); err != nil {
		return fmt.Errorf("write authorization error: %w", err)
	}
	return nil
}
