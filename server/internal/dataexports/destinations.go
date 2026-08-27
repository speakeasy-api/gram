package dataexports

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/net/http/httpguts"

	gen "github.com/speakeasy-api/gram/server/gen/data_exports"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/dataexports/repo"
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
	if parsed.Host == "" {
		return "", oops.E(oops.CodeInvalid, nil, "endpoint_url must include a host")
	}
	if parsed.User != nil {
		return "", oops.E(oops.CodeInvalid, nil, "endpoint_url must not include userinfo")
	}
	if strings.Contains(raw, "#") {
		return "", oops.E(oops.CodeInvalid, nil, "endpoint_url must not include a fragment")
	}

	return parsed.String(), nil
}

func normalizeHeaderInputs(inputs []*gen.OtelDestinationHeaderInput, existing map[string]string) (map[string]string, error) {
	headers := make(map[string]string, len(inputs))
	seen := make(map[string]struct{}, len(inputs))
	for _, input := range inputs {
		if input == nil {
			return nil, oops.E(oops.CodeInvalid, nil, "header entry cannot be null")
		}

		name := strings.TrimSpace(input.Name)
		if !httpguts.ValidHeaderFieldName(name) {
			return nil, oops.E(oops.CodeInvalid, nil, "invalid header name %q", name)
		}
		folded := strings.ToLower(name)
		if _, exists := seen[folded]; exists {
			return nil, oops.E(oops.CodeInvalid, nil, "duplicate header name %q", name)
		}
		seen[folded] = struct{}{}

		if input.Value != nil {
			headers[name] = *input.Value
			continue
		}

		value, exists := valueForHeader(existing, name)
		if !exists {
			return nil, oops.E(oops.CodeInvalid, nil, "header value is required for new header %q", name)
		}
		headers[name] = value
	}

	return headers, nil
}

func valueForHeader(headers map[string]string, name string) (string, bool) {
	for existingName, value := range headers {
		if strings.EqualFold(existingName, name) {
			return value, true
		}
	}
	return "", false
}

func (s *Service) encryptHeaders(headers map[string]string) (pgtype.Text, error) {
	if len(headers) == 0 {
		return pgtype.Text{String: "", Valid: false}, nil
	}

	plaintext, err := json.Marshal(headers)
	if err != nil {
		return pgtype.Text{}, fmt.Errorf("marshal destination headers: %w", err)
	}
	ciphertext, err := s.encryption.Encrypt(plaintext)
	if err != nil {
		return pgtype.Text{}, fmt.Errorf("encrypt destination headers: %w", err)
	}
	return pgtype.Text{String: ciphertext, Valid: true}, nil
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

func (s *Service) destinationSnapshot(row repo.OtelDestination) (*audit.OtelDestinationSnapshot, error) {
	headers, err := s.decryptHeaders(row.HeadersEncrypted)
	if err != nil {
		return nil, err
	}
	policy, err := sensitiveDataFromRow(row.SensitiveData)
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)

	return &audit.OtelDestinationSnapshot{
		EndpointURL:   row.EndpointUrl,
		HeaderNames:   names,
		SensitiveData: string(policy),
	}, nil
}
