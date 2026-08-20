package plugins

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/database"
	"github.com/speakeasy-api/gram/server/internal/directory"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	directoryAttributePrincipalPrefix = "directory_attribute"
	directoryGroupPrincipalPrefix     = "directory_group"
)

var ErrParsingDirectoryPrincipal = errors.New("parse directory principal")

type DirectoryAttribute = directory.AttributeValue

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

func ResolveDirectoryAudiencePrincipalsByEmails(ctx context.Context, db database.DBTX, organizationID string, emails []string) (map[string][]string, error) {
	associations, err := directory.NewService(db).ResolveUserAssociationsByEmails(ctx, organizationID, emails)
	if err != nil {
		return nil, fmt.Errorf("resolve directory user associations: %w", err)
	}

	principals := make(map[string][]string, len(associations))
	for email, association := range associations {
		for _, groupID := range association.GroupIDs {
			principals[email] = append(principals[email], DirectoryGroupPrincipal(groupID))
		}
		for _, attribute := range association.Attributes {
			principals[email] = append(principals[email], DirectoryAttributePrincipal(attribute.Key, attribute.Value))
		}
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
		attribute, err := parseDirectoryAttributePrincipal(raw)
		if err != nil {
			return pluginAssignmentPrincipal{}, oops.E(oops.CodeBadRequest, err, "invalid directory attribute assignment: %s", raw)
		}
		return pluginAssignmentPrincipal{
			Type:       pluginAssignmentPrincipalDirectoryAttribute,
			Identifier: strings.TrimPrefix(DirectoryAttributePrincipal(attribute.Key, attribute.Value), directoryAttributePrincipalPrefix+":"),
		}, nil
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
