package mcp

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

// derivedMemberAuth reports whether a derived gateway can authenticate the
// member, and whether that takes the upstream OAuth token a person gets by
// connecting it through the gateway, by memberNeedsSignIn. It checks
// capability, not whether a person has connected yet: an OAuth member stays
// supported so they can connect it.
func (s *Service) derivedMemberAuth(ctx context.Context, member metaMember) (supported bool, needsOAuth bool, err error) {
	oauth := memberNeedsSignIn(member)
	if !member.remoteServerID.Valid {
		return true, oauth, nil
	}
	rows, err := remotemcp.NewHeaders(s.logger, s.db, s.enc).ListHeaders(ctx, member.remoteServerID.UUID, false)
	if err != nil {
		return false, false, fmt.Errorf("load member headers: %w", err)
	}
	headers := make([]proxy.ConfiguredHeader, 0, len(rows))
	for _, row := range rows {
		headers = append(headers, proxy.ConfiguredHeader{
			Name:                   row.Name,
			StaticValue:            row.Value.String,
			ValueFromRequestHeader: row.ValueFromRequestHeader.String,
			IsRequired:             row.IsRequired,
			IsSecret:               row.IsSecret,
		})
	}
	supported, needsOAuth = remoteMemberAuth(headers, oauth)
	return supported, needsOAuth, nil
}

// remoteMemberAuth applies derivedMemberAuth to a remote member's configured
// headers. A gateway's synthesized request carries no caller headers, so a
// required header must resolve on its own, unless it is Authorization on an
// OAuth member, whose upstream token replaces it. A member with an upstream
// sign-in and no static credential is assumed to need that sign-in.
func remoteMemberAuth(headers []proxy.ConfiguredHeader, oauth bool) (supported bool, needsOAuth bool) {
	static := false
	for _, header := range headers {
		switch {
		case !header.IsRequired:
		case headerResolvesWithoutCaller(header):
			static = true
		case oauth && strings.EqualFold(header.Name, "Authorization"):
			needsOAuth = true
		default:
			return false, false
		}
	}
	return true, oauth && (needsOAuth || !static)
}

func headerResolvesWithoutCaller(header proxy.ConfiguredHeader) bool {
	if err := proxy.CheckRemoteHeader(header); err != nil {
		return false
	}
	var req http.Request
	req.Header = make(http.Header)
	value, err := header.Resolve(&req)
	return err == nil && value != "" && proxy.ValidateHeaderValue(value) == nil
}
