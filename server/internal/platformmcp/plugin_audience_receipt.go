package platformmcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const maxPluginAssignmentReceiptPayloadBytes = 16 << 10

type PluginAssignmentMutationTransaction func(context.Context, pgx.Tx) (SetPluginAssignmentsReceiptResult, error)

type PluginAssignmentMutationReceiptStore struct {
	db  *pgxpool.Pool
	now func() time.Time
}

func NewPluginAssignmentMutationReceiptStore(db *pgxpool.Pool) *PluginAssignmentMutationReceiptStore {
	return &PluginAssignmentMutationReceiptStore{db: db, now: time.Now}
}

func (s *PluginAssignmentMutationReceiptStore) Execute(ctx context.Context, principal Principal, project ResolvedProject, idempotencyKey string, normalized normalizedSetPluginAssignments, mutate PluginAssignmentMutationTransaction) (OperationReceipt, error) {
	if s == nil || s.db == nil || s.now == nil || mutate == nil || principal.OrganizationID == "" || principal.UserID == "" || project.ID == uuid.Nil || project.Slug == "" || idempotencyKey == "" || len(idempotencyKey) > 128 {
		return OperationReceipt{}, pluginAssignmentMutationInvalid("The plugin assignment receipt request is invalid.")
	}
	inputHash, err := pluginAssignmentMutationInputHash(normalized)
	if err != nil {
		return OperationReceipt{}, pluginAssignmentMutationInvalid("The plugin assignment request could not be normalized.")
	}
	return executeMutationReceipt(ctx, mutationReceiptExecution[SetPluginAssignmentsReceiptResult]{
		DB: s.db, Now: s.now, Principal: principal, Project: project, Operation: operationSetPluginAssignments, IdempotencyKey: idempotencyKey, InputHash: inputHash, Label: "plugin assignment",
		Invalid: func(cause error) error {
			return &PluginAssignmentMutationError{Code: "invalid_request", Message: "The plugin assignment caller identity is invalid.", Cause: fmt.Errorf("%w: %w", ErrPluginAssignmentMutationInvalid, cause)}
		},
		Conflict:       pluginAssignmentMutationConflict,
		Unavailable:    pluginAssignmentMutationUnavailable,
		ValidateReplay: validPluginAssignmentReceiptPayload,
		EncodeResult: func(result SetPluginAssignmentsReceiptResult) ([]byte, error) {
			return encodePluginAssignmentReceiptResult(result)
		},
		Mutate: mutate,
	})
}

func pluginAssignmentMutationInputHash(normalized normalizedSetPluginAssignments) (string, error) {
	if normalized.ProjectID == "" || normalized.Plugin == "" || normalized.ExpectedAssignmentVersion == "" {
		return "", ErrPluginAssignmentMutationInvalid
	}
	payload, err := json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("encode normalized plugin assignment input: %w", err)
	}
	digest := sha256.Sum256(append([]byte("platform-mcp-plugin-assignment-v1\x00"), payload...))
	return hex.EncodeToString(digest[:]), nil
}

func encodePluginAssignmentReceiptResult(result SetPluginAssignmentsReceiptResult) (json.RawMessage, error) {
	if !validPluginAssignmentReceiptResult(result) {
		return nil, pluginAssignmentMutationUnavailable(errors.New("unsafe plugin assignment receipt result"))
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("encode plugin assignment receipt result: %w", err)
	}
	if len(payload) == 0 || len(payload) > maxPluginAssignmentReceiptPayloadBytes {
		return nil, pluginAssignmentMutationUnavailable(errors.New("plugin assignment receipt result is too large"))
	}
	return payload, nil
}

func validPluginAssignmentReceiptPayload(payload []byte) bool {
	var result SetPluginAssignmentsReceiptResult
	return len(payload) <= maxPluginAssignmentReceiptPayloadBytes && json.Unmarshal(payload, &result) == nil && validPluginAssignmentReceiptResult(result)
}

func validPluginAssignmentReceiptResult(result SetPluginAssignmentsReceiptResult) bool {
	if uuid.Validate(result.ProjectID) != nil || uuid.Validate(result.Plugin.ID) != nil || result.Plugin.Name == "" || result.Plugin.Slug == "" || result.AssignmentVersion == "" || result.ResultCategory != "updated" || len(result.Assignments) > maxPluginMembers {
		return false
	}
	if result.Plugin.Publication != PluginPublicationPublished && result.Plugin.Publication != PluginPublicationUnpublished && result.Plugin.Publication != PluginPublicationNoRepository {
		return false
	}
	for _, assignment := range result.Assignments {
		if assignment.DisplayName == "" || (assignment.Kind != "everyone" && assignment.Kind != "role" && assignment.Kind != "directory_group" && assignment.Kind != "directory_attribute") {
			return false
		}
	}
	return true
}
