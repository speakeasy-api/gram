package platformmcp

import (
	"fmt"
	"net/url"
	"strings"
)

var ErrCatalogConfigurationRejected = fmt.Errorf("platform mcp catalog configuration rejected")

// CatalogConfigurationValues contains values supplied by an agent for one
// inspected catalogue entry. Its keys must be the server-issued field keys from
// CatalogDetails.Configuration. Secret values are rejected before they can be
// persisted, hashed, logged, or returned from a Platform MCP tool.
type CatalogConfigurationValues map[string]string

type resolvedCatalogConfiguration struct {
	remoteURL           string
	headers             []resolvedCatalogHeader
	pendingSecretFields []CatalogConfigurationField
	displayName         string
}

type resolvedCatalogHeader struct {
	name        string
	description string
	required    bool
	secret      bool
	value       string
}

func (d CatalogDetails) resolveConfiguration(values CatalogConfigurationValues) (resolvedCatalogConfiguration, error) {
	if d.remoteURLTemplate == "" {
		d.remoteURLTemplate = d.remoteURL
	}
	if d.remoteURLTemplate == "" {
		return resolvedCatalogConfiguration{}, ErrCatalogConfigurationRejected
	}
	declared := make(map[string]CatalogConfigurationField, len(d.Configuration))
	for _, field := range d.Configuration {
		if field.Key == "" || field.Name == "" {
			return resolvedCatalogConfiguration{}, ErrCatalogConfigurationRejected
		}
		declared[field.Key] = field
	}
	for key, value := range values {
		field, ok := declared[key]
		if !ok || field.Secret || strings.TrimSpace(value) == "" {
			return resolvedCatalogConfiguration{}, ErrCatalogConfigurationRejected
		}
	}

	resolved := resolvedCatalogConfiguration{}
	remoteURL := d.remoteURLTemplate
	for _, field := range d.Configuration {
		if field.Kind != "url_variable" {
			continue
		}
		value, supplied := values[field.Key]
		if !supplied {
			value = field.Default
		}
		if field.Secret {
			// A secret URL variable would put a secret into the persisted source
			// URL, which is neither agent-safe nor supported by the dashboard
			// source settings. Reject the entry before registration rather than
			// returning a dead-end secure-setup continuation.
			return resolvedCatalogConfiguration{}, ErrCatalogConfigurationRejected
		}
		if value == "" && field.Required {
			return resolvedCatalogConfiguration{}, ErrCatalogConfigurationRejected
		}
		if len(field.Choices) > 0 && !containsString(field.Choices, value) {
			return resolvedCatalogConfiguration{}, ErrCatalogConfigurationRejected
		}
		remoteURL = strings.ReplaceAll(remoteURL, "{"+field.Name+"}", url.PathEscape(value))
	}
	if hasUnresolvedRemoteTemplate(remoteURL) || !validCatalogRemoteTemplate(remoteURL) {
		return resolvedCatalogConfiguration{}, ErrCatalogConfigurationRejected
	}
	resolved.remoteURL = remoteURL

	for _, field := range d.Configuration {
		if field.Kind != "header" {
			continue
		}
		value := values[field.Key]
		if field.Secret {
			if field.Required {
				resolved.pendingSecretFields = append(resolved.pendingSecretFields, field)
			}
			// Create the server-declared empty secret field so the dashboard
			// continuation can collect it without the agent ever handling it.
			resolved.headers = append(resolved.headers, resolvedCatalogHeader{
				name:        field.Name,
				description: field.Description,
				required:    field.Required,
				secret:      true,
			})
			continue
		}
		if value == "" && field.Required {
			return resolvedCatalogConfiguration{}, ErrCatalogConfigurationRejected
		}
		if value != "" {
			resolved.headers = append(resolved.headers, resolvedCatalogHeader{
				name:        field.Name,
				description: field.Description,
				required:    field.Required,
				value:       value,
			})
		}
	}
	return resolved, nil
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
