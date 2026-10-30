// Package executionprofiles keeps the execution-selection profiles assistant
// turns bind: for each assistant, root trigger and execution context, a
// versioned set of the typed auth client each MCP server the assistant reaches
// is served with. A turn credential names the version it was issued with, so
// its choices never change under it and a turn never selects a client by its
// agent alone.
//
// A version's choices are decided when a turn credential is issued, from the
// server's configuration, and kept: a later version carries every earlier
// choice and only adds choices for servers that had none. A server whose
// configuration offers exactly one client the context can use gets it; a
// server with no auth client that an administrator decided needs no upstream
// authentication gets an explicit no-auth choice; anything else, including a
// server whose need is still undecided, is left without a choice, reported as
// a gap, and never guessed. A turn with no user (userless) never chooses a
// personal remote_session client or EMA, which act as a user.
//
// A version's choices are upstream_client_selections rows whose credential is
// the version (upstreamclients.CredentialKindExecutionProfileVersion), so the
// auth client backfill reports their coverage with every other credential's.
//
// The profile grants nothing. Permission to use the chosen client is checked
// live on every request, and the upstream client choice stays authoritative
// until the typed auth client resolver is enabled for the organization.
package executionprofiles

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Context is whether a turn runs for a user it identified.
type Context string

const (
	// ContextUserBacked is a turn run as the agent acting for the user its
	// event identified.
	ContextUserBacked Context = "user_backed"

	// ContextUserless is a turn run as its trigger's workload, with no user.
	ContextUserless Context = "userless"
)

// Contexts lists every execution context.
var Contexts = []Context{ContextUserBacked, ContextUserless}

// Sources of a version.
const (
	// SourceIssuance is a version decided when a turn credential was issued.
	SourceIssuance = "issuance"

	// SourceBackfill is a version decided by the offline backfill.
	SourceBackfill = "backfill"

	// SourceAdmin is a version an administrator changed a choice in.
	SourceAdmin = "admin"
)

// Auth client types a decision distinguishes.
const (
	typeRemoteSession    = "remote_session"
	typeEMA              = "ema"
	typeServiceAccount   = "service_account"
	typeStaticCredential = "static_credential" // #nosec G101 -- a client type, not a credential
	typePassThrough      = "pass_through"
)

// GapReason says why a server has no choice in a profile.
type GapReason string

const (
	// GapSelectionRequired is a server several clients could serve; an
	// administrator chooses which one the profile uses.
	GapSelectionRequired GapReason = "selection_required"

	// GapPersonalRequiresUser is a server only a personal client serves, on a
	// profile whose turns have no user to act as.
	GapPersonalRequiresUser GapReason = "personal_requires_user"

	// GapNoClient is a server whose upstream OAuth client no live, enabled
	// auth client carries.
	GapNoClient GapReason = "no_client"

	// GapSetupIncomplete is a server that needs upstream authentication and
	// offers no auth client.
	GapSetupIncomplete GapReason = "setup_incomplete"

	// GapUpstreamAuthUndecided is a server that offers no auth client and
	// whose need for upstream authentication an administrator has not
	// decided.
	GapUpstreamAuthUndecided GapReason = "upstream_auth_undecided"
)

// Gap is one server a profile has no choice for.
type Gap struct {
	MCPServerID uuid.UUID `json:"mcp_server_id"`
	Reason      GapReason `json:"reason"`
}

// decision is a new choice for one server: an auth client, or uuid.Nil for
// an explicit no-auth choice.
type decision struct {
	serverID     uuid.UUID
	authClientID uuid.UUID
}

// decide decides a server's choice for a context. A server's upstream OAuth
// client is its single associated one, which is what serves it today; its
// remote_session, EMA or service_account auth client serves the choice. A server
// with no associated upstream OAuth client is served by its headers, as a
// static_credential or pass_through auth client, or by nothing.
func decide(row ListProfileServersRow, execCtx Context) (decision, *Gap) {
	gap := func(reason GapReason) (decision, *Gap) {
		return decision{serverID: uuid.Nil, authClientID: uuid.Nil}, &Gap{MCPServerID: row.McpServerID, Reason: reason}
	}
	choose := func(id uuid.UUID) (decision, *Gap) {
		return decision{serverID: row.McpServerID, authClientID: id}, nil
	}
	// A server that needs no upstream sign-in takes no credential, matching
	// the choices an administrator may record for it.
	if row.RequiresUpstreamAuth.Valid && !row.RequiresUpstreamAuth.Bool {
		return choose(uuid.Nil)
	}
	switch len(row.RegistrationIds) {
	case 0:
		headers, oauth := []uuid.UUID{}, false
		for i, id := range row.AuthClientIds {
			switch row.AuthClientTypes[i] {
			case typeStaticCredential, typePassThrough:
				headers = append(headers, id)
			default:
				oauth = true
			}
		}
		switch {
		case oauth || len(headers) > 1:
			return gap(GapSelectionRequired)
		case len(headers) == 1:
			return choose(headers[0])
		case !row.RequiresUpstreamAuth.Valid:
			return gap(GapUpstreamAuthUndecided)
		case row.RequiresUpstreamAuth.Bool:
			return gap(GapSetupIncomplete)
		default:
			return choose(uuid.Nil)
		}
	case 1:
		registration := row.RegistrationIds[0]
		candidates, personal := []uuid.UUID{}, false
		for i, id := range row.AuthClientIds {
			if row.AuthClientRegistrationIds[i] != registration {
				continue
			}
			switch row.AuthClientTypes[i] {
			case typeServiceAccount:
				candidates = append(candidates, id)
			case typeRemoteSession, typeEMA:
				if execCtx == ContextUserless {
					personal = true
					continue
				}
				candidates = append(candidates, id)
			}
		}
		switch {
		case len(candidates) == 1:
			return choose(candidates[0])
		case len(candidates) > 1:
			return gap(GapSelectionRequired)
		case personal:
			return gap(GapPersonalRequiresUser)
		default:
			return gap(GapNoClient)
		}
	default:
		return gap(GapSelectionRequired)
	}
}

// Target is the assistant, root trigger and execution context a turn runs
// under.
type Target struct {
	OrganizationID string
	ProjectID      uuid.UUID
	AssistantID    uuid.UUID
	TriggerID      uuid.UUID
	Context        Context
}

// Binding is the profile version a turn credential binds.
type Binding struct {
	VersionID uuid.UUID
	Version   int64
	// Written reports that this bind recorded the version.
	Written bool
	// Gaps lists the assistant's servers the version has no choice for.
	Gaps []Gap
}

// TxStarter begins the transaction a bind runs in.
type TxStarter interface {
	BeginTx(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error)
}

// readCommitted is the isolation a bind needs: each statement takes its own
// snapshot, so the version read after LockProfile includes one a turn that
// held the lock before it committed.
var readCommitted = pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: "", DeferrableMode: "", BeginQuery: "", CommitQuery: ""}

// Bind returns the profile version a turn under target binds, deciding a new
// version first when a server the assistant reaches has no choice yet and one
// can be decided. It creates the profile on its first turn.
func Bind(ctx context.Context, db TxStarter, target Target, source string) (Binding, error) {
	var zero Binding
	if target.OrganizationID == "" || target.ProjectID == uuid.Nil || target.AssistantID == uuid.Nil || target.TriggerID == uuid.Nil {
		return zero, errors.New("execution profile target is incomplete")
	}
	if target.Context != ContextUserBacked && target.Context != ContextUserless {
		return zero, fmt.Errorf("unknown execution context %q", target.Context)
	}
	tx, err := db.BeginTx(ctx, readCommitted)
	if err != nil {
		return zero, fmt.Errorf("begin execution profile bind: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	binding, err := bindTx(ctx, New(tx), target, source)
	if err != nil {
		return zero, err
	}
	if err := tx.Commit(ctx); err != nil {
		return zero, fmt.Errorf("commit execution profile bind: %w", err)
	}
	return binding, nil
}

// plan is what a bind under target would record: the profile's latest
// version, the new choices it can decide and the servers left without one.
type plan struct {
	profileID uuid.UUID
	versionID uuid.UUID
	version   int64
	decisions []decision
	gaps      []Gap
}

// writes reports whether a bind records a new version.
func (p plan) writes() bool {
	return p.version == 0 || len(p.decisions) > 0
}

func planTx(ctx context.Context, q *Queries, target Target, profileID, versionID uuid.UUID, version int64) (plan, error) {
	servers, err := q.ListProfileServers(ctx, ListProfileServersParams{
		VersionID:      uuid.NullUUID{UUID: versionID, Valid: version > 0},
		OrganizationID: target.OrganizationID,
		AssistantID:    target.AssistantID,
		ProjectID:      target.ProjectID,
	})
	if err != nil {
		return plan{}, fmt.Errorf("list execution profile servers: %w", err)
	}
	p := plan{profileID: profileID, versionID: versionID, version: version, decisions: []decision{}, gaps: []Gap{}}
	for _, row := range servers {
		if row.HasChoice {
			continue
		}
		d, gap := decide(row, target.Context)
		if gap != nil {
			p.gaps = append(p.gaps, *gap)
			continue
		}
		p.decisions = append(p.decisions, d)
	}
	return p, nil
}

// preview decides what a bind under target would record without writing or
// locking anything.
func preview(ctx context.Context, q *Queries, target Target) (plan, error) {
	profile, err := q.GetProfile(ctx, GetProfileParams{
		OrganizationID:    target.OrganizationID,
		ProjectID:         target.ProjectID,
		AssistantID:       target.AssistantID,
		TriggerInstanceID: target.TriggerID,
		ExecutionContext:  string(target.Context),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return planTx(ctx, q, target, uuid.Nil, uuid.Nil, 0)
	case err != nil:
		return plan{}, fmt.Errorf("read execution profile: %w", err)
	}
	return planTx(ctx, q, target, profile.ID, profile.VersionID, profile.Version)
}

func bindTx(ctx context.Context, q *Queries, target Target, source string) (Binding, error) {
	var zero Binding
	if err := q.EnsureProfile(ctx, EnsureProfileParams{
		OrganizationID:    target.OrganizationID,
		ProjectID:         target.ProjectID,
		AssistantID:       target.AssistantID,
		TriggerInstanceID: target.TriggerID,
		ExecutionContext:  string(target.Context),
	}); err != nil {
		return zero, fmt.Errorf("ensure execution profile: %w", err)
	}
	if _, err := q.LockProfile(ctx, LockProfileParams{
		OrganizationID:    target.OrganizationID,
		ProjectID:         target.ProjectID,
		AssistantID:       target.AssistantID,
		TriggerInstanceID: target.TriggerID,
		ExecutionContext:  string(target.Context),
	}); err != nil {
		return zero, fmt.Errorf("lock execution profile: %w", err)
	}
	profile, err := q.GetProfile(ctx, GetProfileParams{
		OrganizationID:    target.OrganizationID,
		ProjectID:         target.ProjectID,
		AssistantID:       target.AssistantID,
		TriggerInstanceID: target.TriggerID,
		ExecutionContext:  string(target.Context),
	})
	if err != nil {
		return zero, fmt.Errorf("read execution profile: %w", err)
	}
	p, err := planTx(ctx, q, target, profile.ID, profile.VersionID, profile.Version)
	if err != nil {
		return zero, err
	}
	gaps := p.gaps
	if !p.writes() {
		return Binding{VersionID: profile.VersionID, Version: profile.Version, Written: false, Gaps: gaps}, nil
	}
	next := profile.Version + 1
	versionID, err := q.CreateProfileVersion(ctx, CreateProfileVersionParams{
		ProfileID:      profile.ID,
		OrganizationID: target.OrganizationID,
		ProjectID:      target.ProjectID,
		Version:        next,
		Source:         source,
	})
	if err != nil {
		return zero, fmt.Errorf("create execution profile version: %w", err)
	}
	if profile.Version > 0 {
		if err := q.CopyProfileVersionChoices(ctx, CopyProfileVersionChoicesParams{
			ToVersionID:    versionID,
			FromVersionID:  profile.VersionID,
			OrganizationID: target.OrganizationID,
			ProjectID:      target.ProjectID,
		}); err != nil {
			return zero, fmt.Errorf("copy execution profile choices: %w", err)
		}
	}
	for _, d := range p.decisions {
		n, err := q.RecordProfileChoice(ctx, RecordProfileChoiceParams{
			VersionID:      versionID,
			Source:         source,
			OrganizationID: target.OrganizationID,
			AuthClientID:   uuid.NullUUID{UUID: d.authClientID, Valid: d.authClientID != uuid.Nil},
			McpServerID:    d.serverID,
		})
		if err != nil {
			return zero, fmt.Errorf("record execution profile choice: %w", err)
		}
		if n == 0 {
			// The client changed since it was decided from; the next turn
			// decides again.
			gaps = append(gaps, Gap{MCPServerID: d.serverID, Reason: GapNoClient})
		}
	}
	return Binding{VersionID: versionID, Version: next, Written: true, Gaps: gaps}, nil
}
