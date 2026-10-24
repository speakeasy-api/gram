package executionprofiles

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
)

var (
	// ErrServerNotReached is a choice for a server the assistant does not
	// reach through a proxy, so no profile serves it.
	ErrServerNotReached = errors.New("the assistant does not reach this server through Speakeasy")

	// ErrVersionChanged is a change decided from a version that is no longer
	// the profile's latest.
	ErrVersionChanged = errors.New("the assistant's credentials changed since they were read; read them again")

	// ErrChoiceChanged is a credential that was withdrawn from the server
	// while the choice was being recorded.
	ErrChoiceChanged = errors.New("the server's credentials changed; review the choice and try again")
)

// Choice is the credential a profile serves one server with.
type Choice struct {
	// AuthClientID is the auth client, or invalid for no upstream
	// authentication.
	AuthClientID uuid.NullUUID
	// Source is how the choice was made. A choice the next turn would decide
	// on its own, not yet recorded, has SourceAutomatic.
	Source string
}

// SourceAutomatic marks a choice the next turn decides from the server's
// configuration, which no version holds yet.
const SourceAutomatic = "automatic"

// Option is one credential a server offers that a profile's context can use.
type Option struct {
	AuthClientID uuid.UUID
	Name         string
	ClientType   string
}

// ServerView is one server a profile serves.
type ServerView struct {
	MCPServerID uuid.UUID
	// Choice is the server's choice, or nil when it has none.
	Choice *Choice
	// Gap says why a server with no choice gets none on its own.
	Gap *GapReason
	// Options are the credentials the profile's context can choose.
	Options []Option
	// NoAuthAllowed reports that the server was decided to need no upstream
	// authentication, so no-auth can be chosen.
	NoAuthAllowed bool
}

// View is a profile's latest version as the dashboard shows it.
type View struct {
	Target  Target
	Version int64
	Servers []ServerView
}

// ChoiceInput is an administrator's change to one server's choice: an auth
// client, no upstream authentication, or, with Clear, back to the choice the
// next turn decides.
type ChoiceInput struct {
	AuthClientID uuid.NullUUID
	NoAuth       bool
	Clear        bool
	// ExpectedVersion is the latest version the change was decided from.
	ExpectedVersion int64
}

// Describe reads a profile's latest version: each server's choice, why a
// server has none, and the credentials its context can choose.
func Describe(ctx context.Context, db DBTX, target Target) (View, error) {
	q := New(db)
	profile, err := q.GetProfile(ctx, GetProfileParams{
		OrganizationID:    target.OrganizationID,
		ProjectID:         target.ProjectID,
		AssistantID:       target.AssistantID,
		TriggerInstanceID: target.TriggerID,
		ExecutionContext:  string(target.Context),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		profile = GetProfileRow{ID: uuid.Nil, VersionID: uuid.Nil, Version: 0}
	case err != nil:
		return View{}, fmt.Errorf("read execution profile: %w", err)
	}
	servers, err := q.ListProfileServers(ctx, ListProfileServersParams{
		VersionID:      uuid.NullUUID{UUID: profile.VersionID, Valid: profile.Version > 0},
		OrganizationID: target.OrganizationID,
		AssistantID:    target.AssistantID,
		ProjectID:      target.ProjectID,
	})
	if err != nil {
		return View{}, fmt.Errorf("list execution profile servers: %w", err)
	}
	recorded := map[uuid.UUID]Choice{}
	if profile.Version > 0 {
		rows, err := q.ListVersionChoices(ctx, ListVersionChoicesParams{VersionID: profile.VersionID, OrganizationID: target.OrganizationID})
		if err != nil {
			return View{}, fmt.Errorf("list execution profile choices: %w", err)
		}
		for _, row := range rows {
			recorded[row.McpServerID] = Choice{AuthClientID: row.AuthClientID, Source: row.Source}
		}
	}
	ids := make([]uuid.UUID, 0, len(servers))
	for _, row := range servers {
		ids = append(ids, row.McpServerID)
	}
	offered, err := upstreamclientsrepo.New(db).ListServerConsentAuthClients(ctx, upstreamclientsrepo.ListServerConsentAuthClientsParams{OrganizationID: target.OrganizationID, McpServerIds: ids})
	if err != nil {
		return View{}, fmt.Errorf("list server auth clients: %w", err)
	}
	options := map[uuid.UUID][]Option{}
	live := map[uuid.UUID]bool{}
	for _, row := range offered {
		if !row.AuthClientID.Valid {
			continue
		}
		live[row.AuthClientID.UUID] = true
		if upstreamclients.CheckConfiguredChoice([]upstreamclientsrepo.ListServerConsentAuthClientsRow{row}, row.AuthClientID, target.Context == ContextUserless) != nil {
			continue
		}
		options[row.McpServerID] = append(options[row.McpServerID], Option{AuthClientID: row.AuthClientID.UUID, Name: row.AuthClientName.String, ClientType: row.ClientType.String})
	}

	view := View{Target: target, Version: profile.Version, Servers: make([]ServerView, 0, len(servers))}
	for _, row := range servers {
		server := ServerView{
			MCPServerID: row.McpServerID, Choice: nil, Gap: nil, Options: options[row.McpServerID],
			NoAuthAllowed: row.RequiresUpstreamAuth.Valid && !row.RequiresUpstreamAuth.Bool,
		}
		if server.Options == nil {
			server.Options = []Option{}
		}
		if choice, ok := recorded[row.McpServerID]; ok {
			server.Choice = &choice
			// A recorded client the server no longer offers enabled serves
			// nothing until another is chosen.
			switch {
			case choice.AuthClientID.Valid && !live[choice.AuthClientID.UUID]:
				reason := GapNoClient
				server.Gap = &reason
			case !choice.AuthClientID.Valid && !server.NoAuthAllowed:
				// A no-auth choice serves nothing once the server needs, or
				// has not decided whether it needs, upstream authentication.
				reason := GapSelectionRequired
				switch {
				case len(server.Options) > 0:
				case row.RequiresUpstreamAuth.Valid:
					reason = GapSetupIncomplete
				default:
					reason = GapUpstreamAuthUndecided
				}
				server.Gap = &reason
			}
		} else if d, gap := decide(row, target.Context); gap != nil {
			server.Gap = &gap.Reason
		} else {
			server.Choice = &Choice{AuthClientID: uuid.NullUUID{UUID: d.authClientID, Valid: d.authClientID != uuid.Nil}, Source: SourceAutomatic}
		}
		view.Servers = append(view.Servers, server)
	}
	return view, nil
}

// SetChoice records an administrator's change to one server's choice in a new
// version that keeps every other choice. beforeCommit runs in the same
// transaction with the choice it replaced, so the change and its audit entry
// commit together. Turns already running keep the version they bound; later
// turns bind the new one. The choice grants nothing: each request checks the
// turn may use it. A userless profile represents nobody, so it chooses shared
// credentials only.
func SetChoice(ctx context.Context, db TxStarter, target Target, serverID uuid.UUID, input ChoiceInput, beforeCommit func(ctx context.Context, tx pgx.Tx, previous *Choice) error) error {
	if target.OrganizationID == "" || target.ProjectID == uuid.Nil || target.AssistantID == uuid.Nil || target.TriggerID == uuid.Nil {
		return errors.New("execution profile target is incomplete")
	}
	if target.Context != ContextUserBacked && target.Context != ContextUserless {
		return fmt.Errorf("unknown execution context %q", target.Context)
	}
	tx, err := db.BeginTx(ctx, readCommitted)
	if err != nil {
		return fmt.Errorf("begin execution profile change: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	q := New(tx)
	key := EnsureProfileParams{
		OrganizationID:    target.OrganizationID,
		ProjectID:         target.ProjectID,
		AssistantID:       target.AssistantID,
		TriggerInstanceID: target.TriggerID,
		ExecutionContext:  string(target.Context),
	}
	if err := q.EnsureProfile(ctx, key); err != nil {
		return fmt.Errorf("ensure execution profile: %w", err)
	}
	if _, err := q.LockProfile(ctx, LockProfileParams(key)); err != nil {
		return fmt.Errorf("lock execution profile: %w", err)
	}
	profile, err := q.GetProfile(ctx, GetProfileParams(key))
	if err != nil {
		return fmt.Errorf("read execution profile: %w", err)
	}
	if profile.Version != input.ExpectedVersion {
		return ErrVersionChanged
	}
	servers, err := q.ListProfileServers(ctx, ListProfileServersParams{
		VersionID:      uuid.NullUUID{UUID: profile.VersionID, Valid: profile.Version > 0},
		OrganizationID: target.OrganizationID,
		AssistantID:    target.AssistantID,
		ProjectID:      target.ProjectID,
	})
	if err != nil {
		return fmt.Errorf("list execution profile servers: %w", err)
	}
	var reachedRow *ListProfileServersRow
	for i := range servers {
		if servers[i].McpServerID == serverID {
			reachedRow = &servers[i]
		}
	}
	if reachedRow == nil {
		return ErrServerNotReached
	}
	var previous *Choice
	if !reachedRow.HasChoice {
		// The choice the next turn would make on its own is what this replaces.
		if d, gap := decide(*reachedRow, target.Context); gap == nil {
			previous = &Choice{AuthClientID: uuid.NullUUID{UUID: d.authClientID, Valid: d.authClientID != uuid.Nil}, Source: SourceAutomatic}
		}
	}
	if profile.Version > 0 {
		rows, err := q.ListVersionChoices(ctx, ListVersionChoicesParams{VersionID: profile.VersionID, OrganizationID: target.OrganizationID})
		if err != nil {
			return fmt.Errorf("list execution profile choices: %w", err)
		}
		for _, row := range rows {
			if row.McpServerID == serverID {
				previous = &Choice{AuthClientID: row.AuthClientID, Source: row.Source}
			}
		}
	}

	clientID := input.AuthClientID
	if input.NoAuth {
		clientID = uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	}
	if !input.Clear {
		offered, err := upstreamclientsrepo.New(tx).ListServerConsentAuthClients(ctx, upstreamclientsrepo.ListServerConsentAuthClientsParams{OrganizationID: target.OrganizationID, McpServerIds: []uuid.UUID{serverID}})
		if err != nil {
			return fmt.Errorf("list server auth clients: %w", err)
		}
		if err := upstreamclients.CheckConfiguredChoice(offered, clientID, target.Context == ContextUserless); err != nil {
			return fmt.Errorf("check choice: %w", err)
		}
	}

	versionID, err := q.CreateProfileVersion(ctx, CreateProfileVersionParams{
		ProfileID:      profile.ID,
		OrganizationID: target.OrganizationID,
		ProjectID:      target.ProjectID,
		Version:        profile.Version + 1,
		Source:         SourceAdmin,
	})
	if err != nil {
		return fmt.Errorf("create execution profile version: %w", err)
	}
	if profile.Version > 0 {
		if err := q.CopyProfileVersionChoicesExcept(ctx, CopyProfileVersionChoicesExceptParams{
			ToVersionID:       versionID,
			FromVersionID:     profile.VersionID,
			OrganizationID:    target.OrganizationID,
			ProjectID:         target.ProjectID,
			ExceptMcpServerID: serverID,
		}); err != nil {
			return fmt.Errorf("copy execution profile choices: %w", err)
		}
	}
	if !input.Clear {
		n, err := q.RecordProfileChoice(ctx, RecordProfileChoiceParams{
			VersionID:      versionID,
			Source:         upstreamclients.SelectionSourceAdmin,
			OrganizationID: target.OrganizationID,
			AuthClientID:   clientID,
			McpServerID:    serverID,
		})
		if err != nil {
			return fmt.Errorf("record execution profile choice: %w", err)
		}
		if n != 1 {
			return ErrChoiceChanged
		}
	}
	if err := beforeCommit(ctx, tx, previous); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit execution profile change: %w", err)
	}
	return nil
}
