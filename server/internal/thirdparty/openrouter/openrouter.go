package openrouter

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/hashicorp/go-cleanhttp"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/internal/inv"
	"github.com/speakeasy-api/gram/internal/o11y"
	"github.com/speakeasy-api/gram/internal/thirdparty/openrouter/repo"
)

const OpenRouterBaseURL = "https://openrouter.ai/api"

type Provisioner interface {
	ProvisionAPIKey(ctx context.Context, orgID string) (string, error)
}

type OpenRouter struct {
	provisioningKey string
	env             string
	logger          *slog.Logger
	repo            *repo.Queries
	orClient        *http.Client
}

func New(logger *slog.Logger, db *pgxpool.Pool, env string, provisioningKey string) *OpenRouter {
	return &OpenRouter{
		provisioningKey: provisioningKey,
		env:             env,
		logger:          logger,
		repo:            repo.New(db),
		orClient:        cleanhttp.DefaultPooledClient(),
	}
}

func (o *OpenRouter) ProvisionAPIKey(ctx context.Context, orgID string) (string, error) {
	var openrouterKey string

	key, err := o.repo.GetOpenRouterAPIKey(ctx, orgID)
	switch {
	case errors.Is(err, sql.ErrNoRows), key.Key == "":
		keyResponse, err := o.createOpenRouterAPIKey(ctx, orgID)
		if err != nil {
			return "", err
		}

		_, err = o.repo.CreateOpenRouterAPIKey(ctx, repo.CreateOpenRouterAPIKeyParams{
			OrganizationID: orgID,
			Key:            keyResponse.Key,
			KeyHash:        keyResponse.Data.Hash,
		})
		if err != nil {
			return "", err
		}

		openrouterKey = keyResponse.Key

	case err != nil:
		return "", err

	default:
		openrouterKey = key.Key
	}

	if err := inv.Check("openrouter provisioning", "key is set", openrouterKey != ""); err != nil {
		return "", err
	}

	return openrouterKey, nil
}

type createKeyRequest struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	Limit *int64 `json:"limit,omitempty"`
}

type createKeyResponse struct {
	Data struct {
		Limit string `json:"limit"`
		Hash  string `json:"hash"`
	} `json:"data"`
	Key string `json:"key"`
}

func (o *OpenRouter) createOpenRouterAPIKey(ctx context.Context, orgID string) (*createKeyResponse, error) {
	requestBody := createKeyRequest{Name: fmt.Sprintf("gram-%s-%s", o.env, orgID), Label: fmt.Sprintf("%s (%s environment)", orgID, o.env), Limit: nil}

	bodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		o.logger.ErrorContext(ctx, "failed to marshal create openrouter key request body", slog.String("error", err.Error()))
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", OpenRouterBaseURL+"/v1/keys", bytes.NewReader(bodyBytes))
	if err != nil {
		o.logger.ErrorContext(ctx, "failed to create openrouter key HTTP request", slog.String("error", err.Error()))
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+o.provisioningKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.orClient.Do(req)
	if err != nil {
		o.logger.ErrorContext(ctx, "failed to send HTTP request", slog.String("error", err.Error()))
		return nil, err
	}

	defer o11y.NoLogDefer(func() error {
		return resp.Body.Close()
	})

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, errors.New("failed to create OpenRouter API key: " + resp.Status)
	}

	var response createKeyResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		o.logger.ErrorContext(ctx, "failed to decode create openrouter key response body", slog.String("error", err.Error()))
		return nil, err
	}

	if response.Key == "" {
		o.logger.ErrorContext(ctx, "missing key in OpenRouter response")
		return nil, errors.New("missing key in OpenRouter response")
	}

	return &response, nil
}
