// Package cimd resolves OAuth Client ID Metadata Document client_ids
// (draft-ietf-oauth-client-id-metadata-document): a client_id that is itself a
// URL pointing at a hosted JSON document describing the client, used in place
// of pre-registration or dynamic client registration.
//
// Both of the dev-idp's authorization servers accept CIMD client_ids -- the
// oauth2-1 server on the authorize leg and when a CIMD client authenticates
// with private_key_jwt, and each resource authorization server when a client
// redeems an ID-JAG -- so the dereferencing lives here rather than in either
// one.
//
// # Deviations from the draft
//
// The draft requires an https client_id and requires the server to refuse
// URLs resolving to special-use addresses (RFC 6890), excepting loopback that
// matches its own interface. The dev-idp serves loopback and nothing else, so
// it allows plain http and does no address filtering: the documents it is
// meant to read are the ones on the developer's own machine. That would be an
// SSRF vector in a real deployment.
package cimd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/speakeasy-api/gram/dev-idp/internal/jwks"
)

// maxDocumentBytes caps how much of a hosted document is read. The draft
// recommends a 5 KB ceiling to keep a hostile document from being a
// denial-of-service vector; the same bound covers a referenced JWKS, which is
// a few hundred bytes per key.
const maxDocumentBytes = 5 << 10

// ErrClientIDMismatch is returned by a resolve when a document's own client_id
// does not equal the URL it was fetched from. Callers distinguish it from a
// transport failure so the client sees which of the two went wrong -- the
// mismatch means a hosted document is claiming an identity that isn't its own,
// which is worth surfacing distinctly from "the host was unreachable".
var ErrClientIDMismatch = errors.New("client metadata document client_id does not match the client_id URL")

// Document is the subset of a Client ID Metadata Document the dev-idp reads.
//
// Per the draft a CIMD client cannot hold a shared secret -- client_secret
// MUST NOT appear and token_endpoint_auth_method MUST NOT name any
// secret-based method -- so a confidential CIMD client is necessarily a
// private_key_jwt one, and Jwks/JwksURI are how it says so.
type Document struct {
	ClientID                string   `json:"client_id"`
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	JwksURI                 string   `json:"jwks_uri"`
	// Jwks is an inline JWKS. The draft allows either this or JwksURI; when
	// both are present the inline one wins, since it needs no second fetch.
	Jwks json.RawMessage `json:"jwks"`
}

// IsClientID reports whether a client_id is a Client ID Metadata Document URL
// (an http or https URL) rather than an opaque registered client id.
func IsClientID(clientID string) bool {
	u, err := url.Parse(clientID)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
}

// Resolver fetches and caches client metadata documents.
//
// Caching matters because a CIMD client's keys are consulted on every request
// it authenticates, and the draft expects servers to respect HTTP cache
// headers. This keeps a fixed short TTL instead, which the draft permits as a
// server-defined bound and which is the right shape for a dev tool: long
// enough that a burst of requests is one fetch, short enough that
// republishing a rotated key takes effect without a restart.
//
// Only successful resolutions are cached; the draft forbids caching errors,
// and a client fixing its document should not have to wait out a TTL.
type Resolver struct {
	client *http.Client
	ttl    time.Duration

	mu sync.Mutex
	// Documents and resolved key sets are cached separately: the authorize
	// leg needs only the document, and following a jwks_uri to build the key
	// set is a second request that leg should not pay for.
	cache    map[string]cacheEntry
	keyCache map[string]keyEntry
}

// zeroDocument is the "nothing resolved" value. Named rather than written as
// a bare literal so the exhaustruct linter stays useful on real construction.
var zeroDocument = Document{
	ClientID:                "",
	ClientName:              "",
	RedirectURIs:            nil,
	TokenEndpointAuthMethod: "",
	JwksURI:                 "",
	Jwks:                    nil,
}

type cacheEntry struct {
	doc       Document
	expiresAt time.Time
}

type keyEntry struct {
	keys      jwks.Document
	hasKeys   bool
	expiresAt time.Time
}

func NewResolver(client *http.Client, ttl time.Duration) *Resolver {
	return &Resolver{
		client:   client,
		ttl:      ttl,
		mu:       sync.Mutex{},
		cache:    make(map[string]cacheEntry),
		keyCache: make(map[string]keyEntry),
	}
}

// Document dereferences a CIMD client_id URL and returns the hosted metadata,
// from cache when it is still fresh.
func (r *Resolver) Document(ctx context.Context, clientID string) (Document, error) {
	if doc, ok := r.cached(clientID); ok {
		return doc, nil
	}

	doc, err := r.fetch(ctx, clientID, clientID)
	if err != nil {
		return Document{}, err
	}
	if doc.ClientID != clientID {
		return Document{}, fmt.Errorf("%w: document declared %q, fetched from %q", ErrClientIDMismatch, doc.ClientID, clientID)
	}

	r.mu.Lock()
	r.cache[clientID] = cacheEntry{doc: doc, expiresAt: time.Now().Add(r.ttl)}
	r.mu.Unlock()

	return doc, nil
}

// Keys resolves the public keys a CIMD client authenticates with, following
// jwks_uri when the document does not inline them.
//
// The false return means the document declares no keys at all, which is not
// an error: the draft makes key material optional, and a document without it
// describes a public client.
func (r *Resolver) Keys(ctx context.Context, clientID string) (jwks.Document, bool, error) {
	if entry, ok := r.cachedKeys(clientID); ok {
		return entry.keys, entry.hasKeys, nil
	}

	doc, err := r.Document(ctx, clientID)
	if err != nil {
		return jwks.Document{Keys: nil}, false, err
	}
	keys, hasKeys, err := r.KeysFor(ctx, doc)
	if err != nil {
		return jwks.Document{Keys: nil}, false, err
	}

	r.mu.Lock()
	r.keyCache[clientID] = keyEntry{
		keys:      keys,
		hasKeys:   hasKeys,
		expiresAt: time.Now().Add(r.ttl),
	}
	r.mu.Unlock()

	return keys, hasKeys, nil
}

// KeysFor resolves keys from an already-fetched document, for callers that
// need the rest of it too.
func (r *Resolver) KeysFor(ctx context.Context, doc Document) (jwks.Document, bool, error) {
	if len(doc.Jwks) > 0 {
		parsed, err := jwks.Parse(doc.Jwks)
		if err != nil {
			return jwks.Document{Keys: nil}, false, fmt.Errorf("inline jwks in %s: %w", doc.ClientID, err)
		}
		// Inline wins outright: falling through to jwks_uri on an empty inline
		// set would authenticate against a different key than the document's
		// plain reading promises.
		return parsed, len(parsed.Keys) > 0, nil
	}

	if doc.JwksURI == "" {
		return jwks.Document{Keys: nil}, false, nil
	}

	raw, err := r.get(ctx, doc.JwksURI)
	if err != nil {
		return jwks.Document{Keys: nil}, false, fmt.Errorf("fetch jwks_uri for %s: %w", doc.ClientID, err)
	}
	parsed, err := jwks.Parse(raw)
	if err != nil {
		return jwks.Document{Keys: nil}, false, fmt.Errorf("jwks at %s: %w", doc.JwksURI, err)
	}
	return parsed, len(parsed.Keys) > 0, nil
}

func (r *Resolver) cachedKeys(clientID string) (keyEntry, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.keyCache[clientID]
	if !ok || time.Now().After(entry.expiresAt) {
		return keyEntry{keys: jwks.Document{Keys: nil}, hasKeys: false, expiresAt: time.Time{}}, false
	}
	return entry, true
}

func (r *Resolver) cached(clientID string) (Document, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.cache[clientID]
	if !ok || time.Now().After(entry.expiresAt) {
		return zeroDocument, false
	}
	return entry.doc, true
}

func (r *Resolver) fetch(ctx context.Context, url, describeAs string) (Document, error) {
	raw, err := r.get(ctx, url)
	if err != nil {
		return Document{}, err
	}
	var doc Document
	if err := json.Unmarshal(raw, &doc); err != nil {
		return Document{}, fmt.Errorf("decode document at %s: %w", describeAs, err)
	}
	return doc, nil
}

func (r *Resolver) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request for %s: %w", url, err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get %s: status %d", url, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDocumentBytes))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", url, err)
	}
	return body, nil
}
