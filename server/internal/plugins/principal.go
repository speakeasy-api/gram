package plugins

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	workosrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/workos/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	directoryAttributePrincipalPrefix = "directory_attribute"
	directoryGroupPrincipalPrefix     = "directory_group"
)

var ErrParsingDirectoryPrincipal = errors.New("parse directory principal")

type DirectoryAttribute struct {
	Key string

	Value string
}

type pluginAssignmentPrincipalType uint8

const (
	pluginAssignmentPrincipalStandard pluginAssignmentPrincipalType = iota
	pluginAssignmentPrincipalDirectoryGroup
	pluginAssignmentPrincipalDirectoryAttribute
)

type pluginAssignmentPrincipal struct {
	Type pluginAssignmentPrincipalType

	Identifier string
}

func (p pluginAssignmentPrincipal) String() string {
	switch p.Type {
	case pluginAssignmentPrincipalDirectoryGroup:
		return fmt.Sprintf("%s:%s", directoryGroupPrincipalPrefix, p.Identifier)
	case pluginAssignmentPrincipalDirectoryAttribute:
		return fmt.Sprintf("%s:%s", directoryAttributePrincipalPrefix, p.Identifier)
	default:
		return p.Identifier
	}
}

func DirectoryGroupPrincipal(id uuid.UUID) string {
	return fmt.Sprintf("%s:%s", directoryGroupPrincipalPrefix, id)
}

func DirectoryAttributePrincipal(key, value string) string {
	return fmt.Sprintf("%s:%s:%s", directoryAttributePrincipalPrefix, base64.RawURLEncoding.EncodeToString([]byte(key)), base64.RawURLEncoding.EncodeToString([]byte(value)))
}

func parseDirectoryGroupPrincipal(principal string) (uuid.UUID, error) {
	value, ok := strings.CutPrefix(principal, directoryGroupPrincipalPrefix+":")
	if !ok {
		return uuid.Nil, fmt.Errorf("%w: expected directory group prefix", ErrParsingDirectoryPrincipal)
	}

	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: group id: %w", ErrParsingDirectoryPrincipal, err)
	}

	return id, nil
}

func parseDirectoryAttributePrincipal(principal string) (DirectoryAttribute, error) {
	encoded, ok := strings.CutPrefix(principal, directoryAttributePrincipalPrefix+":")
	if !ok {
		return DirectoryAttribute{}, fmt.Errorf("%w: expected directory attribute prefix", ErrParsingDirectoryPrincipal)
	}
	key, value, ok := strings.Cut(encoded, ":")
	if !ok {
		return DirectoryAttribute{}, fmt.Errorf("%w: missing attribute value", ErrParsingDirectoryPrincipal)
	}
	decodedKey, err := base64.RawURLEncoding.DecodeString(key)
	if err != nil {
		return DirectoryAttribute{}, fmt.Errorf("%w: attribute key: %w", ErrParsingDirectoryPrincipal, err)
	}
	decodedValue, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return DirectoryAttribute{}, fmt.Errorf("%w: attribute value: %w", ErrParsingDirectoryPrincipal, err)
	}
	return DirectoryAttribute{Key: string(decodedKey), Value: string(decodedValue)}, nil
}

func ResolveDirectoryAudiencePrincipalsByEmails(ctx context.Context, db workosrepo.DBTX, organizationID string, emails []string) (map[string][]string, error) {
	normalized := make([]string, 0, len(emails))
	for _, email := range emails {
		email = conv.NormalizeEmail(email)
		if email == "" || slices.Contains(normalized, email) {
			continue
		}
		normalized = append(normalized, email)
	}
	if len(normalized) == 0 {
		return map[string][]string{}, nil
	}

	rows, err := workosrepo.New(db).ListActiveDirectoryGroupIDsByEmails(ctx, workosrepo.ListActiveDirectoryGroupIDsByEmailsParams{OrganizationID: organizationID, Emails: normalized})
	if err != nil {
		return nil, fmt.Errorf("list active directory groups: %w", err)
	}
	attributes, err := workosrepo.New(db).ListActiveDirectoryUserAttributesByEmails(ctx, workosrepo.ListActiveDirectoryUserAttributesByEmailsParams{OrganizationID: organizationID, Emails: normalized})
	if err != nil {
		return nil, fmt.Errorf("list active directory user attributes: %w", err)
	}

	principals := make(map[string][]string, len(normalized))
	for _, row := range rows {
		principals[row.Email] = append(principals[row.Email], DirectoryGroupPrincipal(row.DirectoryGroupID))
	}
	for _, attribute := range attributes {
		principals[attribute.Email] = append(principals[attribute.Email], DirectoryAttributePrincipal(attribute.AttributeKey, attribute.AttributeValue))
	}
	return principals, nil
}

func (s *Service) parsePluginAssignmentPrincipal(ctx context.Context, organizationID, raw string) (pluginAssignmentPrincipal, error) {
	switch {
	case raw == urn.PrincipalWildcard:
		return pluginAssignmentPrincipal{Type: pluginAssignmentPrincipalStandard, Identifier: raw}, nil
	case strings.HasPrefix(raw, directoryGroupPrincipalPrefix+":"):
		id, err := parseDirectoryGroupPrincipal(raw)
		if err != nil {
			return pluginAssignmentPrincipal{}, oops.E(oops.CodeBadRequest, err, "invalid directory group assignment: %s", raw)
		}
		return pluginAssignmentPrincipal{Type: pluginAssignmentPrincipalDirectoryGroup, Identifier: id.String()}, nil
	case strings.HasPrefix(raw, directoryAttributePrincipalPrefix+":"):
		_, err := parseDirectoryAttributePrincipal(raw)
		if err != nil {
			return pluginAssignmentPrincipal{}, oops.E(oops.CodeBadRequest, err, "invalid directory attribute assignment: %s", raw)
		}
		return pluginAssignmentPrincipal{Type: pluginAssignmentPrincipalDirectoryAttribute, Identifier: strings.TrimPrefix(raw, directoryAttributePrincipalPrefix+":")}, nil
	default:
		normalized := raw
		if addr, ok := strings.CutPrefix(raw, string(urn.PrincipalTypeEmail)+":"); ok {
			normalized = string(urn.PrincipalTypeEmail) + ":" + conv.NormalizeEmail(addr)
		}
		principal, err := urn.ParsePrincipal(normalized)
		if err != nil {
			return pluginAssignmentPrincipal{}, oops.E(oops.CodeBadRequest, err, "invalid principal URN: %s", raw)
		}
		if principal.Type != urn.PrincipalTypeRole {
			return pluginAssignmentPrincipal{Type: pluginAssignmentPrincipalStandard, Identifier: principal.String()}, nil
		}
		if err := authz.ValidatePrincipal(ctx, s.db, organizationID, principal); err != nil {
			if errors.Is(err, authz.ErrPrincipalInvalid) || errors.Is(err, authz.ErrPrincipalNotFound) {
				return pluginAssignmentPrincipal{}, oops.E(oops.CodeBadRequest, err, "invalid role principal URN: %s", raw)
			}
			return pluginAssignmentPrincipal{}, oops.E(oops.CodeUnexpected, err, "validate role principal URN: %s", raw).LogError(ctx, s.logger)
		}
		return pluginAssignmentPrincipal{Type: pluginAssignmentPrincipalStandard, Identifier: principal.String()}, nil
	}
}
