package platformmcp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const maxAccessRoleMutationReceiptPayloadBytes = 16 << 10

type AccessRoleMutationReceiptResult struct {
	RoleID         string            `json:"role_id"`
	RoleSlug       string            `json:"role_slug"`
	Name           string            `json:"name"`
	Description    string            `json:"description,omitempty"`
	Version        string            `json:"version"`
	MCPAccess      MCPConnectSummary `json:"mcp_access"`
	ResultCategory string            `json:"result_category"`
	Reconciliation string            `json:"reconciliation"`
}

type AccessRoleMutationTransaction func(context.Context, pgx.Tx) (AccessRoleMutationReceiptResult, error)

type AccessRoleMutationReceiptStore struct {
	db  *pgxpool.Pool
	now func() time.Time
}

func NewAccessRoleMutationReceiptStore(db *pgxpool.Pool) *AccessRoleMutationReceiptStore {
	return &AccessRoleMutationReceiptStore{db: db, now: time.Now}
}

func (s *AccessRoleMutationReceiptStore) ExecuteCreate(ctx context.Context, principal Principal, project ResolvedProject, idempotencyKey string, normalized normalizedCreateMCPAccessRole, mutate AccessRoleMutationTransaction) (OperationReceipt, error) {
	return s.execute(ctx, principal, project, operationCreateMCPAccessRole, idempotencyKey, normalized, mutate)
}

func (s *AccessRoleMutationReceiptStore) ExecuteUpdate(ctx context.Context, principal Principal, project ResolvedProject, idempotencyKey string, normalized normalizedUpdateMCPAccessRole, mutate AccessRoleMutationTransaction) (OperationReceipt, error) {
	return s.execute(ctx, principal, project, operationUpdateMCPAccessRole, idempotencyKey, normalized, mutate)
}

func (s *AccessRoleMutationReceiptStore) execute(ctx context.Context, principal Principal, project ResolvedProject, operation, idempotencyKey string, normalized any, mutate AccessRoleMutationTransaction) (OperationReceipt, error) {
	if s == nil || s.db == nil || s.now == nil || mutate == nil || !accessRoleMutationOperation(operation) || idempotencyKey == "" || len(idempotencyKey) > 128 {
		return OperationReceipt{}, accessRoleMutationInvalid("The access role receipt request is invalid.")
	}
	inputHash, err := accessRoleMutationInputHash(operation, normalized)
	if err != nil {
		return OperationReceipt{}, accessRoleMutationInvalid("The access role mutation could not be normalized.")
	}
	return executeMutationReceipt(ctx, mutationReceiptExecution[AccessRoleMutationReceiptResult]{
		DB: s.db, Now: s.now, Principal: principal, Project: project, Operation: operation,
		IdempotencyKey: idempotencyKey, InputHash: inputHash, Label: "access role",
		Invalid: func(cause error) error {
			return &AccessRoleMutationError{Code: "invalid_request", Message: "The access role mutation caller identity is invalid.", Cause: fmt.Errorf("%w: %w", ErrAccessRoleMutationInvalid, cause)}
		},
		Conflict:    accessRoleMutationConflict,
		Unavailable: accessRoleMutationUnavailable,
		ValidateReplay: func(payload []byte) bool {
			return validAccessRoleMutationReceiptPayload(operation, payload)
		},
		EncodeResult: func(result AccessRoleMutationReceiptResult) ([]byte, error) {
			return encodeAccessRoleMutationReceipt(operation, result)
		},
		Mutate: mutate,
	})
}

func accessRoleMutationInputHash(operation string, normalized any) (string, error) {
	if !accessRoleMutationOperation(operation) || normalized == nil {
		return "", ErrAccessRoleMutationInvalid
	}
	payload, err := json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("encode normalized access role mutation input: %w", err)
	}
	digest := sha256.Sum256(append([]byte("platform-mcp-access-role-mutation-v1\x00"+operation+"\x00"), payload...))
	return hex.EncodeToString(digest[:]), nil
}

func accessRoleMutationOperation(operation string) bool {
	return operation == operationCreateMCPAccessRole || operation == operationUpdateMCPAccessRole
}

func encodeAccessRoleMutationReceipt(operation string, result AccessRoleMutationReceiptResult) ([]byte, error) {
	if !validAccessRoleMutationReceiptResult(operation, result) {
		return nil, accessRoleMutationUnavailable(errors.New("unsafe access role mutation receipt result"))
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("encode access role mutation receipt: %w", err)
	}
	if len(payload) == 0 || len(payload) > maxAccessRoleMutationReceiptPayloadBytes {
		return nil, accessRoleMutationUnavailable(errors.New("access role mutation receipt result is too large"))
	}
	return payload, nil
}

func decodeAccessRoleMutationReceipt(operation string, payload []byte) (AccessRoleMutationReceiptResult, error) {
	var result AccessRoleMutationReceiptResult
	if !validAccessRoleMutationReceiptPayload(operation, payload) {
		return result, accessRoleMutationUnavailable(errors.New("invalid access role mutation replay payload"))
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return AccessRoleMutationReceiptResult{}, accessRoleMutationUnavailable(err)
	}
	return result, nil
}

func validAccessRoleMutationReceiptPayload(operation string, payload []byte) bool {
	if len(payload) == 0 || len(payload) > maxAccessRoleMutationReceiptPayloadBytes {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var result AccessRoleMutationReceiptResult
	if decoder.Decode(&result) != nil || !validAccessRoleMutationReceiptResult(operation, result) {
		return false
	}
	return decoder.Decode(&struct{}{}) == io.EOF
}

func validAccessRoleMutationReceiptResult(operation string, result AccessRoleMutationReceiptResult) bool {
	if uuid.Validate(result.RoleID) != nil || result.RoleSlug == "" || strings.TrimSpace(result.Name) == "" || !validAccessRoleVersion(result.Version) {
		return false
	}
	expectedCategory := ""
	switch operation {
	case operationCreateMCPAccessRole:
		expectedCategory = "created"
	case operationUpdateMCPAccessRole:
		expectedCategory = "updated"
	default:
		return false
	}
	if result.ResultCategory != expectedCategory {
		return false
	}
	return result.Reconciliation == "complete" || result.Reconciliation == "pending"
}
