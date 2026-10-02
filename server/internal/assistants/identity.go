package assistants

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
)

var errAssistantCreateConflict = errors.New("assistant creation key conflicts with a previous request")

// assistantCreateFingerprint versions the complete normalized create request.
// Attachment order is not part of the request identity.
type assistantCreateFingerprint struct {
	Version        int                            `json:"Version"`
	Name           string                         `json:"Name"`
	Model          string                         `json:"Model"`
	Instructions   string                         `json:"Instructions"`
	Toolsets       []*types.AssistantToolsetRef   `json:"Toolsets"`
	MCPServers     []*types.AssistantMCPServerRef `json:"MCPServers"`
	WarmTTLSeconds int                            `json:"WarmTTLSeconds"`
	MaxConcurrency int                            `json:"MaxConcurrency"`
	Status         string                         `json:"Status"`
}

func assistantCreateRequestKey(organizationID string, projectID uuid.UUID, actorID, key string) (string, error) {
	if key == "" || len(key) > 256 {
		return "", assistantValidationError("idempotency key must contain 1 to 256 bytes")
	}
	encoded, err := json.Marshal([]string{"assistant.create.v1", organizationID, projectID.String(), actorID, key})
	if err != nil {
		return "", fmt.Errorf("encode assistant create key: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func assistantCreateRequestHash(request assistantCreateFingerprint) (string, error) {
	request.Version = 1
	request.Toolsets = slices.DeleteFunc(slices.Clone(request.Toolsets), func(ref *types.AssistantToolsetRef) bool { return ref == nil })
	request.MCPServers = slices.DeleteFunc(slices.Clone(request.MCPServers), func(ref *types.AssistantMCPServerRef) bool { return ref == nil })
	for i, ref := range request.Toolsets {
		normalized := *ref
		normalized.EnvironmentSlug = conv.PtrEmpty(conv.PtrValOr(ref.EnvironmentSlug, ""))
		request.Toolsets[i] = &normalized
	}
	for i, ref := range request.MCPServers {
		normalized := *ref
		normalized.EnvironmentSlug = conv.PtrEmpty(conv.PtrValOr(ref.EnvironmentSlug, ""))
		normalized.EndpointSlug = conv.PtrEmpty(conv.PtrValOr(ref.EndpointSlug, ""))
		request.MCPServers[i] = &normalized
	}
	if request.Toolsets == nil {
		request.Toolsets = []*types.AssistantToolsetRef{}
	}
	if request.MCPServers == nil {
		request.MCPServers = []*types.AssistantMCPServerRef{}
	}
	slices.SortFunc(request.Toolsets, func(a, b *types.AssistantToolsetRef) int {
		if n := strings.Compare(a.ToolsetSlug, b.ToolsetSlug); n != 0 {
			return n
		}
		return strings.Compare(conv.PtrValOr(a.EnvironmentSlug, ""), conv.PtrValOr(b.EnvironmentSlug, ""))
	})
	slices.SortFunc(request.MCPServers, func(a, b *types.AssistantMCPServerRef) int {
		if n := strings.Compare(a.McpServerSlug, b.McpServerSlug); n != 0 {
			return n
		}
		if n := strings.Compare(conv.PtrValOr(a.EnvironmentSlug, ""), conv.PtrValOr(b.EnvironmentSlug, "")); n != 0 {
			return n
		}
		return strings.Compare(conv.PtrValOr(a.EndpointSlug, ""), conv.PtrValOr(b.EndpointSlug, ""))
	})
	// Goa attachment references lack JSON tags; v1 deliberately preserves their field names.
	encoded, err := json.Marshal(request) //nolint:musttag // Generated attachment types cannot be annotated here.
	if err != nil {
		return "", fmt.Errorf("encode assistant create request: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func (s *ServiceCore) hydrateAssistantIdentityStates(ctx context.Context, projectID uuid.UUID, records []assistantRecord) error {
	ids := make([]uuid.UUID, 0, len(records))
	for i := range records {
		records[i].IdentityState = string(assistantidentity.NeverConfigured)
		records[i].AgentID = nil
		records[i].IdentityGeneration = nil
		ids = append(ids, records[i].ID)
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := assistantrepo.New(s.db).ListAssistantIdentityStates(ctx, assistantrepo.ListAssistantIdentityStatesParams{ProjectID: projectID, AssistantIds: ids})
	if err != nil {
		return fmt.Errorf("load assistant identity states: %w", err)
	}
	indexes := make(map[uuid.UUID]int, len(records))
	for i := range records {
		indexes[records[i].ID] = i
	}
	for _, row := range rows {
		i, ok := indexes[row.OriginalAssistantID]
		if !ok {
			continue
		}
		records[i].IdentityGeneration = conv.PtrEmpty(row.Generation)
		records[i].IdentityState = string(assistantidentity.Tombstoned)
		if !row.Tombstoned {
			records[i].IdentityState = string(assistantidentity.Active)
			records[i].AgentID = conv.PtrEmpty(row.OriginalAgentID.String())
		}
	}
	return nil
}

func (s *ServiceCore) hydrateAssistantIdentityState(ctx context.Context, projectID uuid.UUID, record *assistantRecord) error {
	records := []assistantRecord{*record}
	if err := s.hydrateAssistantIdentityStates(ctx, projectID, records); err != nil {
		return err
	}
	*record = records[0]
	return nil
}

// UpgradeAssistantIdentity is opt-in for a legacy assistant. Its creator is
// retained; the authenticated actor only authorizes provisioning now.
func (s *ServiceCore) UpgradeAssistantIdentity(ctx context.Context, organizationID string, projectID, assistantID uuid.UUID, actorUserID string) (assistantRecord, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return assistantRecord{}, fmt.Errorf("begin assistant identity upgrade: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := triggerrepo.New(tx).LockTriggerProject(ctx, projectID); err != nil {
		return assistantRecord{}, fmt.Errorf("lock assistant identity project: %w", err)
	}

	row, err := assistantrepo.New(tx).LockAssistantIdentityAnchor(ctx, assistantrepo.LockAssistantIdentityAnchorParams{ProjectID: projectID, AssistantID: assistantID})
	if err != nil {
		return assistantRecord{}, fmt.Errorf("lock assistant for identity upgrade: %w", err)
	}
	if row.OrganizationID != organizationID {
		return assistantRecord{}, pgx.ErrNoRows
	}
	if _, err = assistantidentity.Upgrade(ctx, tx, assistantidentity.ProvisionParams{OrganizationID: organizationID, ProjectID: projectID, AssistantID: assistantID, ActorUserID: actorUserID}); err != nil {
		return assistantRecord{}, fmt.Errorf("assistant identity Upgrade: %w", err)
	}
	if _, err = s.ensureDashboardRootTx(ctx, tx, organizationID, projectID, assistantID, row.Name); err != nil {
		return assistantRecord{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return assistantRecord{}, fmt.Errorf("commit assistant identity upgrade: %w", err)
	}
	return s.GetAssistant(ctx, projectID, assistantID)
}
