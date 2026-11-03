package mcpservers

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
)

// credentialSource names one credential-source backend an MCP server can be
// in front of. Only these two kinds carry credential headers, so only they
// have typed auth clients derived from them.
type credentialSource struct {
	kind string
	id   uuid.UUID
}

// credentialSourcesOf collects the distinct credential sources among the given
// backend references, skipping the unset ones. A server write passes the
// references it moves between, so a backend switch yields both.
func credentialSourcesOf(remote, tunneled []uuid.NullUUID) []credentialSource {
	seen := map[credentialSource]bool{}
	out := make([]credentialSource, 0, len(remote)+len(tunneled))
	for kind, refs := range map[string][]uuid.NullUUID{
		authclientbackfill.SourceKindRemote:   remote,
		authclientbackfill.SourceKindTunneled: tunneled,
	} {
		for _, ref := range refs {
			source := credentialSource{kind: kind, id: ref.UUID}
			if !ref.Valid || seen[source] {
				continue
			}
			seen[source] = true
			out = append(out, source)
		}
	}
	slices.SortFunc(out, func(a, b credentialSource) int {
		if n := strings.Compare(a.kind, b.kind); n != 0 {
			return n
		}
		return bytes.Compare(a.id[:], b.id[:])
	})
	return out
}

// verifyLiveCredentialSources takes each source's row lock and settles its
// liveness and project ownership under it, for a write that is putting a
// server in front of the source. A caller that checked liveness before the
// lock would otherwise commit a server onto a source a concurrent deletion had
// just tombstoned. It also gives the auth client sync the lock it needs, ahead
// of any server lock, as every other writer of a credential source does.
func verifyLiveCredentialSources(ctx context.Context, tx pgx.Tx, projectID uuid.UUID, sources []credentialSource) error {
	for _, source := range sources {
		var err error
		switch source.kind {
		case authclientbackfill.SourceKindRemote:
			err = VerifyLiveRemoteMCPSourceInTransaction(ctx, tx, projectID, source.id)
		case authclientbackfill.SourceKindTunneled:
			err = VerifyLiveTunneledMCPSourceInTransaction(ctx, tx, projectID, source.id)
		default:
			err = fmt.Errorf("unknown credential source kind %q", source.kind)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// lockCredentialSources takes the source row locks the auth client sync needs,
// before this transaction locks the server row. A source's own writers lock it
// before touching its servers, so taking it first here keeps every path's lock
// order source-then-server.
func lockCredentialSources(ctx context.Context, tx pgx.Tx, organizationID string, sources []credentialSource) error {
	for _, source := range sources {
		var err error
		switch source.kind {
		case authclientbackfill.SourceKindRemote:
			err = authclientbackfill.LockRemoteSource(ctx, tx, organizationID, source.id)
		case authclientbackfill.SourceKindTunneled:
			err = authclientbackfill.LockTunneledSource(ctx, tx, organizationID, source.id)
		default:
			err = fmt.Errorf("unknown credential source kind %q", source.kind)
		}
		if err != nil {
			return fmt.Errorf("lock %s source for auth clients: %w", source.kind, err)
		}
	}
	return nil
}

// syncCredentialSources brings the typed auth clients, links and header copies
// derived from each source in line with its configuration.
func syncCredentialSources(ctx context.Context, tx pgx.Tx, organizationID string, sources []credentialSource) error {
	for _, source := range sources {
		var err error
		switch source.kind {
		case authclientbackfill.SourceKindRemote:
			err = authclientbackfill.SyncRemoteSource(ctx, tx, organizationID, source.id)
		case authclientbackfill.SourceKindTunneled:
			err = authclientbackfill.SyncTunneledSource(ctx, tx, organizationID, source.id)
		default:
			err = fmt.Errorf("unknown credential source kind %q", source.kind)
		}
		if err != nil {
			return fmt.Errorf("sync %s source auth clients: %w", source.kind, err)
		}
	}
	return nil
}
