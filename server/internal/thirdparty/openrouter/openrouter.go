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

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/internal/thirdparty/openrouter/repo"
)

const OpenRouterBaseURL = "https://openrouter.ai/api"

type OpenRouter struct {
	provisioningKey string
	apiKey          string
	env             string
	logger          *slog.Logger
	repo            *repo.Queries
}

func New(logger *slog.Logger, db *pgxpool.Pool, apiKey, provisioningKey, env string) (*OpenRouter, error) {
	// We only support direct key usage in local development
	if env == "local" {
		if apiKey == "" {
			return nil, errors.New("an OpenRouter API key is required in local development")
		}

		return &OpenRouter{
			apiKey:          apiKey,
			env:             env,
			logger:          logger,
			provisioningKey: "",
			repo:            repo.New(db),
		}, nil
	}
	// Keys are provisioned per org in non prod environments
	return &OpenRouter{
		provisioningKey: provisioningKey,
		env:             env,
		logger:          logger,
		apiKey:          "",
		repo:            repo.New(db),
	}, nil
}

func (o *OpenRouter) GetAPIKey(ctx context.Context, orgID string) (string, error) {
	if o.env == "local" {
		return o.apiKey, nil
	}
	var openrouterKey string
	if key, err := o.repo.GetOpenRouterAPIKey(ctx, orgID); err != nil || key.Key == "" {
		switch {
		case errors.Is(err, sql.ErrNoRows), key.Key == "": // we need to create a new key
			keyResponse, err := o.CreateOpenRouterAPIKey(ctx, orgID)
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
		}
	} else {
		openrouterKey = key.Key
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

func (o *OpenRouter) CreateOpenRouterAPIKey(ctx context.Context, orgID string) (*createKeyResponse, error) {
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

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		o.logger.ErrorContext(ctx, "failed to send HTTP request", slog.String("error", err.Error()))
		return nil, err
	}

	//nolint:errcheck // unnecessary error check
	defer resp.Body.Close()

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
