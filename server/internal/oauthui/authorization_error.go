// Package oauthui renders browser-facing OAuth responses shared by the
// Speakeasy authorization servers.
package oauthui

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"html/template"
	"net/http"
	"net/url"

	"github.com/speakeasy-api/gram/server/internal/oauthwire"
)

//go:embed authorization_error.html
var authorizationErrorHTML string

var authorizationErrorTemplate = template.Must(template.New("authorization-error").Parse(authorizationErrorHTML))

type authorizationErrorView struct {
	// Code is the OAuth error identifier.
	Code string

	// Description explains why authorization could not start.
	Description string

	// CallbackURL carries the same parameters as the server's error redirect.
	CallbackURL template.URL

	// Destination identifies the callback host or native application scheme.
	Destination string
}

// WriteAuthorizationError renders a terminal 400 page with an explicit callback
// link. Callers must build callbackURL only after matching the client's registered
// redirect URI. Scheme validation here protects the HTML link even for stored
// registrations created before the current redirect validation rules.
func WriteAuthorizationError(ctx context.Context, w http.ResponseWriter, callbackURL, code, description string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("render authorization error: %w", err)
	}
	if err := oauthwire.ValidateRedirectURI(callbackURL); err != nil {
		return fmt.Errorf("validate authorization error callback: %w", err)
	}
	uri, err := url.Parse(callbackURL)
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
	view := authorizationErrorView{
		Code:        code,
		Description: description,
		// Native application schemes require template.URL after scheme validation;
		// html/template still escapes the attribute's contents.
		CallbackURL: template.URL(callbackURL), //nolint:gosec // G203: registered callback validated above; native schemes must remain clickable.
		Destination: destination,
	}
	var body bytes.Buffer
	if err := authorizationErrorTemplate.Execute(&body, view); err != nil {
		return fmt.Errorf("render authorization error: %w", err)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusBadRequest)
	if _, err := w.Write(body.Bytes()); err != nil {
		return fmt.Errorf("write authorization error: %w", err)
	}
	return nil
}
