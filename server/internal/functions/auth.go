package functions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"goa.design/goa/v3/security"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/constants"
	deprepo "github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type TokenRequestV1 struct {
	ID      string `json:"id"`
	Exp     int64  `json:"exp"`
	Subject string `json:"sub"`
}

func TokenV1(enc *encryption.Client, req TokenRequestV1) (string, error) {
	bs, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal v1 token: %w", err)
	}

	encBs, err := enc.Encrypt(bs)
	if err != nil {
		return "", fmt.Errorf("encrypt v1 token: %w", err)
	}

	return fmt.Sprintf("v01.%s", encBs), nil
}

type CallToolPayload struct {
	ToolName    string            `json:"name"`
	Input       json.RawMessage   `json:"input"`
	Environment map[string]string `json:"environment,omitempty,omitzero"`
}

type ReadResourcePayload struct {
	URI         string            `json:"uri"`
	Input       json.RawMessage   `json:"input"`
	Environment map[string]string `json:"environment,omitempty,omitzero"`
}

func jwtAuth(ctx context.Context, logger *slog.Logger, db deprepo.DBTX, enc *encryption.Client, token string, scheme *security.JWTScheme) (context.Context, error) {
	logger = logger.With(attr.SlogComponent("functions-auth"))

	if scheme == nil || scheme.Name != constants.FunctionTokenSecurityScheme {
		return ctx, oops.E(oops.CodeUnauthorized, nil, "auth scheme not supported")
	}

	dr := deprepo.New(db)

	t, err := jwt.Parse(token, func(t *jwt.Token) (any, error) {
		if t.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, errors.New("unsupported signing method")
		}

		projectID, deploymentID, functionID, err := parseRunnerJWTSubjectClaim(t)
		if err != nil {
			return nil, fmt.Errorf("parse subject claim: %w", err)
		}

		rows, err := dr.GetFunctionCredentialsBatch(ctx, deprepo.GetFunctionCredentialsBatchParams{
			ProjectID:    projectID,
			DeploymentID: deploymentID,
			FunctionIds:  []uuid.UUID{functionID},
		})
		if err != nil {
			return nil, fmt.Errorf("get function credentials: %w", err)
		}
		if len(rows) == 0 {
			return nil, fmt.Errorf("function credentials not found")
		}

		unsealed, err := enc.Decrypt(string(rows[0].EncryptionKey.Reveal()))
		if err != nil {
			return nil, fmt.Errorf("unseal function encryption key: %w", err)
		}

		return []byte(unsealed), nil
	}, jwt.WithExpirationRequired(), jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil {
		err = fmt.Errorf("parse runner jwt: %w", err)
		return nil, oops.E(oops.CodeUnauthorized, err, "%s", oops.CodeUnauthorized.UserMessage()).Log(ctx, logger)
	}

	if !t.Valid {
		err = fmt.Errorf("invalid runner jwt claims")
		return nil, oops.E(oops.CodeUnauthorized, err, "%s", oops.CodeUnauthorized.UserMessage()).Log(ctx, logger)
	}

	exp, err := t.Claims.GetExpirationTime()
	if err != nil {
		err = fmt.Errorf("get token exp claim: %w", err)
		return nil, oops.E(oops.CodeUnauthorized, err, "%s", oops.CodeUnauthorized.UserMessage()).Log(ctx, logger)
	}
	if exp.After(time.Now().Add(time.Hour)) {
		logger.WarnContext(ctx, "runner jwt exp claim is greater than an hour")
	}

	projectID, deploymentID, functionID, err := parseRunnerJWTSubjectClaim(t)
	if err != nil {
		err = fmt.Errorf("parse validated subject claim: %w", err)
		return nil, oops.E(oops.CodeUnauthorized, err, "%s", oops.CodeUnauthorized.UserMessage()).Log(ctx, logger)
	}

	authCtx := &RunnerAuthContext{
		ProjectID:    projectID,
		DeploymentID: deploymentID,
		FunctionID:   functionID,
	}
	if err := authCtx.Validate(); err != nil {
		err = fmt.Errorf("validate runner auth context: %w", err)
		return nil, oops.E(oops.CodeUnauthorized, err, "%s", oops.CodeUnauthorized.UserMessage()).Log(ctx, logger)
	}

	return PushRunnerAuthContext(ctx, authCtx), nil
}

func parseRunnerJWTSubjectClaim(t *jwt.Token) (projectID, deploymentID, functionID uuid.UUID, err error) {
	sub, err := t.Claims.GetSubject()
	if err != nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, fmt.Errorf("get token subject: %w", err)
	}

	parts := strings.Split(sub, ":")
	if len(parts) != 3 {
		return uuid.Nil, uuid.Nil, uuid.Nil, fmt.Errorf("invalid token subject format")
	}

	projectID, err = uuid.Parse(parts[0])
	if err != nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, fmt.Errorf("parse subject project ID: %s: %w", parts[0], err)
	}
	deploymentID, err = uuid.Parse(parts[1])
	if err != nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, fmt.Errorf("parse subject deployment ID: %s: %w", parts[1], err)
	}
	functionID, err = uuid.Parse(parts[2])
	if err != nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, fmt.Errorf("parse subject function ID: %s: %w", parts[2], err)
	}

	return projectID, deploymentID, functionID, nil
}
