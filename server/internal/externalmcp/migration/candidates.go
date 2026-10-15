// Package migration replaces hosted toolsets that proxy an external
// MCP attachment with remote MCP servers. A migration keeps the server's
// address, its user session issuer and the remote session clients behind it,
// so people stay signed in; it never creates an issuer or a client and never
// registers one with the upstream.
package migration

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"

	"github.com/speakeasy-api/gram/server/internal/externalmcp/migration/repo"
)

// Candidate is a live server backed by an external MCP attachment.
type Candidate struct {
	// Row is the candidate as stored.
	Row repo.ListCandidatesRow

	// RemoteURL is the attachment's upstream URL, or empty when the server
	// references several.
	RemoteURL string

	// CleanedURL is RemoteURL with unfilled template query parameters removed,
	// the URL the replacement dials. Empty when the URL cannot be cleaned.
	CleanedURL string

	// Blockers explain why the server cannot be migrated; empty when it can.
	Blockers []string

	// Targets are existing remote-backed servers in the same project that use
	// the same user session issuer, which the migration may merge into.
	Targets []Target

	// ToolCalls counts the server's tool calls over the reporting window; nil
	// when telemetry is unavailable.
	ToolCalls *uint64
}

// Target is an existing remote-backed server a migration may merge into.
type Target struct {
	// ID is the target's mcp_servers id.
	ID uuid.UUID

	// Name is the target's display name.
	Name string

	// Slug is the target's mcp_servers slug.
	Slug string

	// URL is the upstream URL the target dials.
	URL string
}

const (
	blockerNoIssuer       = "has no sign-in provider to reuse"
	blockerExternalOAuth  = "uses an external OAuth server, which remote MCP servers do not support"
	blockerOAuthProxy     = "uses an OAuth proxy, which remote MCP servers do not support"
	blockerHeaders        = "declares upstream headers, which have no stored values to copy"
	blockerManyURLs       = "proxies more than one upstream URL"
	blockerManyTransports = "proxies upstreams over more than one transport"
)

// ListCandidates returns every live external-MCP-backed server across all
// organizations, with its blockers and merge targets.
func (m *Migrator) ListCandidates(ctx context.Context) ([]Candidate, error) {
	rows, err := repo.New(m.db).ListCandidates(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("list external MCP candidates: %w", err)
	}
	candidates, err := buildCandidates(ctx, m.db, rows)
	if err != nil {
		return nil, err
	}
	if m.toolCalls != nil {
		counts, err := m.toolCalls.CountToolCalls(ctx, candidates)
		if err != nil {
			m.logger.WarnContext(ctx, "count external MCP candidate tool calls", attr.SlogError(err))
		} else {
			for i := range candidates {
				count := counts[candidates[i].Row.ToolsetID]
				candidates[i].ToolCalls = &count
			}
		}
	}
	return candidates, nil
}

func buildCandidates(ctx context.Context, db repo.DBTX, rows []repo.ListCandidatesRow) ([]Candidate, error) {
	issuers := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		if row.UserSessionIssuerID.Valid && !slices.Contains(issuers, row.UserSessionIssuerID.UUID) {
			issuers = append(issuers, row.UserSessionIssuerID.UUID)
		}
	}
	targets := map[uuid.UUID][]repo.ListRemoteServersOnIssuersRow{}
	if len(issuers) > 0 {
		servers, err := repo.New(db).ListRemoteServersOnIssuers(ctx, issuers)
		if err != nil {
			return nil, fmt.Errorf("list remote servers sharing candidate issuers: %w", err)
		}
		for _, server := range servers {
			targets[server.UserSessionIssuerID.UUID] = append(targets[server.UserSessionIssuerID.UUID], server)
		}
	}

	candidates := make([]Candidate, 0, len(rows))
	for _, row := range rows {
		candidate := assess(row)
		if row.UserSessionIssuerID.Valid {
			for _, server := range targets[row.UserSessionIssuerID.UUID] {
				if server.ProjectID != row.ProjectID {
					continue
				}
				candidate.Targets = append(candidate.Targets, Target{
					ID:   server.ID,
					Name: server.Name.String,
					Slug: server.Slug.String,
					URL:  server.Url,
				})
			}
		}
		candidates = append(candidates, candidate)
	}
	return candidates, nil
}

// assess derives a candidate's upstream URL and blockers from its row.
func assess(row repo.ListCandidatesRow) Candidate {
	candidate := Candidate{Row: row, RemoteURL: "", CleanedURL: "", Blockers: nil, Targets: nil, ToolCalls: nil}
	if !row.UserSessionIssuerID.Valid || !row.UserSessionIssuerSlug.Valid {
		candidate.Blockers = append(candidate.Blockers, blockerNoIssuer)
	}
	if row.ExternalOauthServerID.Valid {
		candidate.Blockers = append(candidate.Blockers, blockerExternalOAuth)
	}
	if row.OauthProxyServerID.Valid {
		candidate.Blockers = append(candidate.Blockers, blockerOAuthProxy)
	}
	if row.DeclaresHeaders {
		candidate.Blockers = append(candidate.Blockers, blockerHeaders)
	}
	if len(row.TransportTypes) != 1 || !supportedTransport(row.TransportTypes[0]) {
		candidate.Blockers = append(candidate.Blockers, blockerManyTransports)
	}
	if len(row.RemoteUrls) != 1 {
		candidate.Blockers = append(candidate.Blockers, blockerManyURLs)
		return candidate
	}
	candidate.RemoteURL = row.RemoteUrls[0]
	cleaned, err := CleanRemoteURL(candidate.RemoteURL)
	if err != nil {
		candidate.Blockers = append(candidate.Blockers, err.Error())
		return candidate
	}
	candidate.CleanedURL = cleaned
	return candidate
}

func supportedTransport(transport string) bool {
	return transport == "streamable-http" || transport == "sse"
}

// loadCandidate re-reads one candidate inside the migration transaction. It
// returns nil when the toolset is no longer a candidate.
func loadCandidate(ctx context.Context, tx pgx.Tx, toolsetID uuid.UUID) (*Candidate, error) {
	rows, err := repo.New(tx).ListCandidates(ctx, []uuid.UUID{toolsetID})
	if err != nil {
		return nil, fmt.Errorf("reload external MCP candidate: %w", err)
	}
	if len(rows) != 1 {
		return nil, nil
	}
	candidates, err := buildCandidates(ctx, tx, rows)
	if err != nil {
		return nil, err
	}
	return &candidates[0], nil
}

// SignInProvider names the candidate's user session issuer and the upstream
// authorization servers its clients sign in through.
func (c Candidate) SignInProvider() string {
	if !c.Row.UserSessionIssuerSlug.Valid {
		return ""
	}
	if len(c.Row.RemoteIssuerNames) == 0 {
		return c.Row.UserSessionIssuerSlug.String
	}
	return c.Row.UserSessionIssuerSlug.String + " (" + strings.Join(c.Row.RemoteIssuerNames, ", ") + ")"
}
