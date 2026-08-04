// Package cimd resolves OAuth Client ID Metadata Document client_ids
// (draft-ietf-oauth-client-id-metadata-document): a client_id that is itself a
// URL pointing at a hosted JSON document describing the client, used in place
// of pre-registration or dynamic client registration.
//
// Both of the dev-idp's authorization servers accept CIMD client_ids -- the
// oauth2-1 server on the authorize leg, and each resource authorization server
// when a client redeems an ID-JAG -- so the dereferencing lives here rather
// than in either one.
package cimd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// ErrClientIDMismatch is returned by Fetch when a document's own client_id
// does not equal the URL it was fetched from. Callers distinguish it from a
// transport failure so the client sees which of the two went wrong -- the
// mismatch means a hosted document is claiming an identity that isn't its own,
// which is worth surfacing distinctly from "the host was unreachable".
var ErrClientIDMismatch = errors.New("client metadata document client_id does not match the client_id URL")

// maxDocumentBytes caps how much of a hosted metadata document is read. Client
// metadata documents are small; 64 KiB is comfortably above any legitimate
// document and below any DoS-relevant size.
const maxDocumentBytes = 64 << 10

// Document is the subset of a Client ID Metadata Document the dev-idp
// validates.
type Document struct {
	ClientID     string   `json:"client_id"`
	RedirectURIs []string `json:"redirect_uris"`
}

// IsClientID reports whether a client_id is a Client ID Metadata Document URL
// (an http or https URL) rather than an opaque registered client id.
func IsClientID(clientID string) bool {
	u, err := url.Parse(clientID)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
}

// Fetch dereferences a CIMD client_id URL and parses the hosted metadata
// document. Dev-only: plain HTTP is allowed (the draft requires HTTPS) so
// localhost documents work during local development.
//
// Per the draft the document's own client_id MUST equal the URL it was fetched
// from; Fetch enforces that so callers cannot forget to.
func Fetch(ctx context.Context, client *http.Client, clientID string) (Document, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, clientID, nil)
	if err != nil {
		return Document{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return Document{}, fmt.Errorf("get %s: %w", clientID, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return Document{}, fmt.Errorf("get %s: status %d", clientID, resp.StatusCode)
	}

	var doc Document
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxDocumentBytes)).Decode(&doc); err != nil {
		return Document{}, fmt.Errorf("decode document: %w", err)
	}
	if doc.ClientID != clientID {
		return Document{}, fmt.Errorf("%w: document declared %q, fetched from %q", ErrClientIDMismatch, doc.ClientID, clientID)
	}
	return doc, nil
}
