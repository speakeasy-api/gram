package platformmcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/builtinmcp"
	"github.com/speakeasy-api/gram/server/internal/metamcp"
	metarepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type GetBuiltinGatewayMembersInput struct {
	ProjectID string `json:"project_id"`
	GatewayID string `json:"gateway_id"`
}
type ConfigureBuiltinGatewayMemberInput struct {
	ProjectID       string  `json:"project_id"`
	GatewayID       string  `json:"gateway_id"`
	Key             string  `json:"key"`
	Present         bool    `json:"present" jsonschema:"true adds or updates; false removes this built-in membership only"`
	EnvironmentID   *string `json:"environment_id,omitempty" jsonschema:"same-project environment override; omitted clears the membership override"`
	SortOrder       int     `json:"sort_order" jsonschema:"nonnegative position"`
	ExpectedVersion string  `json:"expected_version" jsonschema:"version from get_builtin_gateway_members"`
	Confirmed       bool    `json:"confirmed"`
}
type BuiltinGatewayMember struct {
	ID            string  `json:"id"`
	Key           string  `json:"key"`
	EnvironmentID *string `json:"environment_id,omitempty"`
	SortOrder     int32   `json:"sort_order"`
}
type BuiltinGatewayMembers struct {
	ProjectID string                 `json:"project_id"`
	GatewayID string                 `json:"gateway_id"`
	Version   string                 `json:"version"`
	Members   []BuiltinGatewayMember `json:"members"`
}

func readBuiltinGatewayMembers(ctx context.Context, tx pgx.Tx, project, gateway uuid.UUID) (BuiltinGatewayMembers, error) {
	rows, err := metarepo.New(tx).ListBuiltinMetaMCPMembers(ctx, metarepo.ListBuiltinMetaMCPMembersParams{ProjectID: uuid.NullUUID{UUID: project, Valid: true}, MetaMcpServerID: uuid.NullUUID{UUID: gateway, Valid: true}})
	if err != nil {
		return BuiltinGatewayMembers{}, fmt.Errorf("built-in gateway configuration: %w", err)
	}
	result := BuiltinGatewayMembers{ProjectID: project.String(), GatewayID: gateway.String(), Version: "", Members: make([]BuiltinGatewayMember, 0, len(rows))}
	for _, row := range rows {
		var environment *string
		if row.EnvironmentID.Valid {
			value := row.EnvironmentID.UUID.String()
			environment = &value
		}
		result.Members = append(result.Members, BuiltinGatewayMember{ID: row.ID.String(), Key: row.BuiltinKey, EnvironmentID: environment, SortOrder: row.SortOrder})
	}
	// Bind the snapshot to its exact project, gateway and configured members.
	encoded, err := json.Marshal(result)
	if err != nil {
		return BuiltinGatewayMembers{}, fmt.Errorf("built-in gateway configuration: %w", err)
	}
	sum := sha256.Sum256(encoded)
	result.Version = hex.EncodeToString(sum[:])
	return result, nil
}
func (s *GatewayService) builtinGatewayMembers(ctx context.Context, principal Principal, input GetBuiltinGatewayMembersInput, change *ConfigureBuiltinGatewayMemberInput) (BuiltinGatewayMembers, error) {
	if !s.valid() {
		return BuiltinGatewayMembers{}, ErrUnavailable
	}
	project, err := builtinMCPProject(principal, input.ProjectID)
	if err != nil {
		return BuiltinGatewayMembers{}, fmt.Errorf("built-in gateway configuration: %w", err)
	}
	gateway, err := uuid.Parse(input.GatewayID)
	if err != nil || gateway == uuid.Nil {
		return BuiltinGatewayMembers{}, errBuiltinMCPInvalid
	}
	if change != nil && (!change.Confirmed || change.ExpectedVersion == "" || change.SortOrder < 0 || change.SortOrder > math.MaxInt32) {
		return BuiltinGatewayMembers{}, errBuiltinMCPInvalid
	}
	if err := builtinmcp.RequireAdmin(ctx, s.db, principal.OrganizationID); err != nil {
		return BuiltinGatewayMembers{}, fmt.Errorf("built-in gateway configuration: %w", err)
	}
	if _, err := s.resolveProject(ctx, principal, project, gateway); err != nil {
		return BuiltinGatewayMembers{}, fmt.Errorf("built-in gateway configuration: %w", err)
	}
	if change != nil {
		if err := s.charge(ctx, principal); err != nil {
			return BuiltinGatewayMembers{}, fmt.Errorf("built-in gateway configuration: %w", err)
		}
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return BuiltinGatewayMembers{}, fmt.Errorf("built-in gateway configuration: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	actor := gatewayActor(principal, project)
	meta, err := s.writer.LockGateway(ctx, tx, actor, gateway)
	if errors.Is(err, metamcp.ErrGatewayNotFound) {
		return BuiltinGatewayMembers{}, oops.C(oops.CodeNotFound)
	}
	if err != nil {
		return BuiltinGatewayMembers{}, fmt.Errorf("built-in gateway configuration: %w", err)
	}
	current, err := readBuiltinGatewayMembers(ctx, tx, project, gateway)
	if err != nil || change == nil {
		return current, err
	}
	if current.Version != change.ExpectedVersion {
		return BuiltinGatewayMembers{}, oops.C(oops.CodeConflict)
	}
	if _, ok := builtinmcp.Lookup(change.Key); !ok {
		return BuiltinGatewayMembers{}, errBuiltinMCPInvalid
	}
	if change.Present {
		environment := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
		if change.EnvironmentID != nil {
			id, err := uuid.Parse(*change.EnvironmentID)
			if err != nil || id == uuid.Nil {
				return BuiltinGatewayMembers{}, errBuiltinMCPInvalid
			}
			environment = uuid.NullUUID{UUID: id, Valid: true}
		}
		// Bounds are checked before the transaction above.
		//nolint:gosec // sort_order is validated between zero and math.MaxInt32.
		if _, err := s.writer.SetBuiltinMember(ctx, tx, actor, meta, change.Key, environment, int32(change.SortOrder)); err != nil {
			return BuiltinGatewayMembers{}, fmt.Errorf("built-in gateway configuration: %w", err)
		}
	} else {
		for _, member := range current.Members {
			if member.Key == change.Key {
				id, err := uuid.Parse(member.ID)
				if err != nil {
					return BuiltinGatewayMembers{}, fmt.Errorf("built-in gateway configuration: %w", err)
				}
				if err := s.writer.RemoveBuiltinMember(ctx, tx, actor, meta, id); err != nil {
					return BuiltinGatewayMembers{}, fmt.Errorf("built-in gateway configuration: %w", err)
				}
			}
		}
	}
	result, err := readBuiltinGatewayMembers(ctx, tx, project, gateway)
	if err != nil {
		return BuiltinGatewayMembers{}, fmt.Errorf("built-in gateway configuration: %w", err)
	}
	if _, err := s.publication.ProjectWithOutcome(ctx, tx, principal.OrganizationID, project, principal.UserID); err != nil {
		return BuiltinGatewayMembers{}, fmt.Errorf("built-in gateway configuration: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return BuiltinGatewayMembers{}, fmt.Errorf("built-in gateway configuration: %w", err)
	}
	metamcp.AfterMembershipCommit(ctx, s.logger, s.db, s.publisher, actor, meta)
	return result, nil
}
