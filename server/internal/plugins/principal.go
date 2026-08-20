package plugins

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/database"
	"github.com/speakeasy-api/gram/server/internal/directory"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

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
	return p.Identifier
}

func ResolveDirectoryAudiencePrincipalsByEmails(ctx context.Context, db database.DBTX, organizationID string, emails []string) (map[string][]string, error) {
	associations, err := directory.NewService(db).ResolveUserAssociationsByEmails(ctx, organizationID, emails)
	if err != nil {
		return nil, fmt.Errorf("resolve directory user associations: %w", err)
	}

	principals := make(map[string][]string, len(associations))
	for email, association := range associations {
		for _, groupID := range association.GroupIDs {
			principals[email] = append(principals[email], directory.GroupPrincipal(groupID))
		}
		for _, attribute := range association.Attributes {
			principals[email] = append(principals[email], directory.AttributePrincipal(attribute.Key, attribute.Value))
		}
	}
	return principals, nil
}

func (s *Service) parsePluginAssignmentPrincipal(ctx context.Context, organizationID, raw string) (pluginAssignmentPrincipal, error) {
	switch {
	case raw == urn.PrincipalWildcard:
		return pluginAssignmentPrincipal{Type: pluginAssignmentPrincipalStandard, Identifier: raw}, nil
	case directory.IsGroupPrincipal(raw):
		id, err := directory.ParseGroupPrincipal(raw)
		if err != nil {
			return pluginAssignmentPrincipal{}, oops.E(oops.CodeBadRequest, err, "invalid directory group assignment: %s", raw)
		}
		return pluginAssignmentPrincipal{Type: pluginAssignmentPrincipalDirectoryGroup, Identifier: directory.GroupPrincipal(id)}, nil
	case directory.IsAttributePrincipal(raw):
		attribute, err := directory.ParseAttributePrincipal(raw)
		if err != nil {
			return pluginAssignmentPrincipal{}, oops.E(oops.CodeBadRequest, err, "invalid directory attribute assignment: %s", raw)
		}
		return pluginAssignmentPrincipal{
			Type:       pluginAssignmentPrincipalDirectoryAttribute,
			Identifier: directory.AttributePrincipal(attribute.Key, attribute.Value),
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
