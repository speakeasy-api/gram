// Package modelcatalog fetches and filters upstream model metadata without inference.
package modelcatalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

const maxResponseBytes = 16 << 20

// HTTPClient is the shared provider transport used to fetch public metadata.
type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

// Fetch retains the upstream envelope and model metadata, filtering only data.
// The caller supplies the same admission predicate used for inference.
func Fetch(ctx context.Context, client HTTPClient, baseURL string, allowed func(string) bool) (map[string]json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("build model catalog request: %w", err)
	}
	// OpenRouter's model catalog is public. Never forward a caller credential
	// or resolve/provision an inference key just to discover models.
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch model catalog: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("model catalog upstream status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read model catalog: %w", err)
	}
	if len(body) > maxResponseBytes {
		return nil, errors.New("model catalog exceeds response limit")
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode model catalog: %w", err)
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(envelope["data"], &entries); err != nil {
		return nil, fmt.Errorf("decode model catalog data: %w", err)
	}
	if entries == nil {
		return nil, errors.New("model catalog data is not an array")
	}
	filtered := make([]json.RawMessage, 0, len(entries))
	for _, entry := range entries {
		var model struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(entry, &model); err != nil {
			return nil, fmt.Errorf("decode model catalog entry: %w", err)
		}
		if model.ID == "" {
			return nil, errors.New("model catalog entry has no id")
		}
		if allowed(model.ID) {
			filtered = append(filtered, entry)
		}
	}
	data, err := json.Marshal(filtered)
	if err != nil {
		return nil, fmt.Errorf("encode filtered model catalog: %w", err)
	}
	envelope["data"] = data
	return envelope, nil
}
