package example

import (
	"context"
	"errors"
	"time"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"

	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/fga"
)

// Option C: OpenFGA. The application still owns layers (credential, agent,
// owner), condition facts, and keeping tuples in sync with domain writes.

type FGAService struct {
	fga *fga.Backend
}

var errForbidden = errors.New("forbidden")

// layers mirrors the multi-policy rule: one Check per required subject.
func layers(agentID, keyID, ownerUserID string) []string {
	return []string{"api_key:" + keyID, "agent:" + agentID, "user:" + ownerUserID}
}

func (s *FGAService) CallTool(ctx context.Context, agentID, keyID, ownerUserID, serverID, tool, disposition string) error {
	ok, err := s.fga.CheckLayers(ctx, layers(agentID, keyID, ownerUserID), "connect", "mcp_server:"+serverID,
		fga.Facts{Tool: tool, Disposition: disposition, Now: time.Now()})
	if err != nil {
		return err // datastore or engine failure: fail closed
	}
	if !ok {
		return errForbidden
	}
	return nil
}

// ListChats: ListObjects returns unsorted, capped ids; every chat must exist
// as tuples (author/agent) for this to work. The ids then feed a SQL query.
func (s *FGAService) ListChats(ctx context.Context, userID string) ([]string, error) {
	return s.fga.ListObjects(ctx, "user:"+userID, "read", "chat", fga.Facts{Now: time.Now()})
}

// RemoveFromGroup: a directory change is a domain write *and* tuple writes.
// They cannot share a transaction; production needs an outbox + replay.
func (s *FGAService) RemoveFromGroup(ctx context.Context, userID, groupID string) error {
	// 1. domain write (WorkOS sync row) — in our Postgres transaction
	// 2. outbox → OpenFGA, after commit:
	return s.fga.Delete(ctx,
		&openfgav1.TupleKeyWithoutCondition{Object: "group:" + groupID, Relation: "member", User: "user:" + userID},
		&openfgav1.TupleKeyWithoutCondition{Object: "user:" + userID, Relation: "member_group", User: "group:" + groupID})
}
