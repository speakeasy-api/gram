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

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const maxAccessRoleAssignmentReceiptPayloadBytes = 16 << 10

type AccessRoleAssignmentReceiptResult struct {
	MaskedIdentity string   `json:"masked_identity"`
	Roles          []string `json:"roles"`
	Version        string   `json:"version"`
	AssignedRole   string   `json:"assigned_role"`
	ResultCategory string   `json:"result_category"`
	Reconciliation string   `json:"reconciliation"`
}

type AccessRoleAssignmentTransaction func(context.Context, pgx.Tx) (AccessRoleAssignmentReceiptResult, error)

type AccessRoleAssignmentReceiptStore struct {
	db  *pgxpool.Pool
	now func() time.Time
}

func NewAccessRoleAssignmentReceiptStore(db *pgxpool.Pool) *AccessRoleAssignmentReceiptStore {
	return &AccessRoleAssignmentReceiptStore{db: db, now: time.Now}
}

func (s *AccessRoleAssignmentReceiptStore) Execute(ctx context.Context, principal Principal, project ResolvedProject, idempotencyKey string, normalized normalizedAccessRoleAssignment, mutate AccessRoleAssignmentTransaction) (OperationReceipt, error) {
	if s == nil || s.db == nil || s.now == nil || mutate == nil || idempotencyKey == "" || len(idempotencyKey) > 128 {
		return OperationReceipt{}, accessRoleMutationInvalid("The access role assignment receipt request is invalid.")
	}
	payload, err := json.Marshal(normalized)
	if err != nil {
		return OperationReceipt{}, accessRoleMutationInvalid("The access role assignment could not be normalized.")
	}
	digest := sha256.Sum256(append([]byte("platform-mcp-access-role-assignment-v1\x00"), payload...))
	return executeMutationReceipt(ctx, mutationReceiptExecution[AccessRoleAssignmentReceiptResult]{
		DB: s.db, Now: s.now, Principal: principal, Project: project, Operation: operationAssignMCPAccessRole,
		IdempotencyKey: idempotencyKey, InputHash: hex.EncodeToString(digest[:]), Label: "access role assignment",
		Invalid: func(cause error) error {
			return &AccessRoleMutationError{Code: "invalid_request", Message: "The access role assignment caller identity is invalid.", Cause: fmt.Errorf("%w: %w", ErrAccessRoleMutationInvalid, cause)}
		},
		Conflict: accessRoleMutationConflict, Unavailable: accessRoleMutationUnavailable,
		ValidateReplay: validAccessRoleAssignmentReceiptPayload,
		EncodeResult:   encodeAccessRoleAssignmentReceipt,
		Mutate:         mutate,
	})
}

func encodeAccessRoleAssignmentReceipt(result AccessRoleAssignmentReceiptResult) ([]byte, error) {
	if !validAccessRoleAssignmentReceiptResult(result) {
		return nil, accessRoleMutationUnavailable(errors.New("unsafe access role assignment receipt result"))
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return nil, accessRoleMutationUnavailable(err)
	}
	if len(payload) == 0 || len(payload) > maxAccessRoleAssignmentReceiptPayloadBytes {
		return nil, accessRoleMutationUnavailable(errors.New("access role assignment receipt payload exceeds size limit"))
	}
	return payload, nil
}

func decodeAccessRoleAssignmentReceipt(payload []byte) (AccessRoleAssignmentReceiptResult, error) {
	var result AccessRoleAssignmentReceiptResult
	if !validAccessRoleAssignmentReceiptPayload(payload) {
		return result, accessRoleMutationUnavailable(errors.New("invalid access role assignment replay payload"))
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return AccessRoleAssignmentReceiptResult{}, accessRoleMutationUnavailable(err)
	}
	return result, nil
}

func validAccessRoleAssignmentReceiptPayload(payload []byte) bool {
	if len(payload) == 0 || len(payload) > maxAccessRoleAssignmentReceiptPayloadBytes {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var result AccessRoleAssignmentReceiptResult
	if decoder.Decode(&result) != nil || !validAccessRoleAssignmentReceiptResult(result) {
		return false
	}
	return decoder.Decode(&struct{}{}) == io.EOF
}

func validAccessRoleAssignmentReceiptResult(result AccessRoleAssignmentReceiptResult) bool {
	if strings.TrimSpace(result.MaskedIdentity) == "" || strings.TrimSpace(result.AssignedRole) == "" || !validAccessRoleVersion(result.Version) || result.Roles == nil {
		return false
	}
	if result.ResultCategory != "assigned" && result.ResultCategory != "already_assigned" {
		return false
	}
	return result.Reconciliation == "pending" || result.Reconciliation == "complete"
}
