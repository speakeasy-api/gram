package dataexports

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/net/http/httpguts"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type sensitiveData string

const (
	sensitiveDataExclude sensitiveData = "exclude"
	sensitiveDataInclude sensitiveData = "include"
)

func parseSensitiveData(value string) (sensitiveData, error) {
	policy := sensitiveData(value)
	switch policy {
	case sensitiveDataExclude, sensitiveDataInclude:
		return policy, nil
	default:
		return "", fmt.Errorf("unsupported sensitive-data policy %q", value)
	}
}

func sensitiveDataFromRow(value pgtype.Text) (sensitiveData, error) {
	if !value.Valid {
		return sensitiveDataExclude, nil
	}
	return parseSensitiveData(value.String)
}

func validateDestinationName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", oops.E(oops.CodeInvalid, nil, "name is required")
	}
	return name, nil
}

func validateDestinationURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", oops.E(oops.CodeInvalid, nil, "endpoint_url is required")
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return "", oops.E(oops.CodeInvalid, err, "endpoint_url is not a valid URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", oops.E(oops.CodeInvalid, nil, "endpoint_url must use http or https")
	}
	if parsed.Hostname() == "" {
		return "", oops.E(oops.CodeInvalid, nil, "endpoint_url must include a host")
	}
	if parsed.User != nil {
		return "", oops.E(oops.CodeInvalid, nil, "endpoint_url must not include userinfo")
	}
	if strings.Contains(raw, "#") {
		return "", oops.E(oops.CodeInvalid, nil, "endpoint_url must not include a fragment")
	}
	if parsed.RawQuery != "" || parsed.ForceQuery {
		return "", oops.E(oops.CodeInvalid, nil, "endpoint_url must not include a query")
	}

	return parsed.String(), nil
}

type destinationHeaderInput struct {
	name     string
	value    string
	hasValue bool
	valid    bool
}

func normalizeHeaderInputs(inputs []destinationHeaderInput, existing map[string]string) (map[string]string, error) {
	headers := make(map[string]string, len(inputs))
	seen := make(map[string]struct{}, len(inputs))
	existingByFoldedName := make(map[string]string, len(existing))
	for name, value := range existing {
		existingByFoldedName[strings.ToLower(name)] = value
	}
	for _, input := range inputs {
		if !input.valid {
			return nil, oops.E(oops.CodeInvalid, nil, "header entry cannot be null")
		}

		name := strings.TrimSpace(input.name)
		if !httpguts.ValidHeaderFieldName(name) {
			return nil, oops.E(oops.CodeInvalid, nil, "invalid header name %q", name)
		}
		folded := strings.ToLower(name)
		if _, exists := seen[folded]; exists {
			return nil, oops.E(oops.CodeInvalid, nil, "duplicate header name %q", name)
		}
		seen[folded] = struct{}{}

		if input.hasValue {
			if !httpguts.ValidHeaderFieldValue(input.value) {
				return nil, oops.E(oops.CodeInvalid, nil, "invalid value for header %q", name)
			}
			headers[name] = input.value
			continue
		}

		value, exists := existingByFoldedName[folded]
		if !exists {
			return nil, oops.E(oops.CodeInvalid, nil, "header value is required for new header %q", name)
		}
		headers[name] = value
	}

	return headers, nil
}

func (s *Service) encryptHeaders(headers map[string]string) (pgtype.Text, error) {
	if len(headers) == 0 {
		return conv.ToPGTextEmpty(""), nil
	}

	plaintext, err := json.Marshal(headers)
	if err != nil {
		return pgtype.Text{}, fmt.Errorf("marshal destination headers: %w", err)
	}
	ciphertext, err := s.encryption.Encrypt(plaintext)
	if err != nil {
		return pgtype.Text{}, fmt.Errorf("encrypt destination headers: %w", err)
	}
	return conv.ToPGText(ciphertext), nil
}

func (s *Service) decryptHeaders(stored pgtype.Text) (map[string]string, error) {
	if !stored.Valid || stored.String == "" {
		return map[string]string{}, nil
	}

	plaintext, err := s.encryption.Decrypt(stored.String)
	if err != nil {
		return nil, fmt.Errorf("decrypt destination headers: %w", err)
	}

	var headers map[string]string
	if err := json.Unmarshal([]byte(plaintext), &headers); err != nil {
		return nil, fmt.Errorf("decode destination headers: %w", err)
	}
	if headers == nil {
		return map[string]string{}, nil
	}
	return headers, nil
}

func destinationSnapshot(name, endpointURL string, headers map[string]string, policy sensitiveData) *audit.OtelDestinationSnapshot {
	headersSnapshot := make([]audit.OtelDestinationHeaderSnapshot, 0, len(headers))
	for name, value := range headers {
		headersSnapshot = append(headersSnapshot, audit.OtelDestinationHeaderSnapshot{
			Name:     name,
			HasValue: value != "",
		})
	}
	sort.Slice(headersSnapshot, func(i, j int) bool {
		return headersSnapshot[i].Name < headersSnapshot[j].Name
	})

	return &audit.OtelDestinationSnapshot{
		Name:          name,
		EndpointURL:   endpointURL,
		Headers:       headersSnapshot,
		SensitiveData: string(policy),
	}
}
