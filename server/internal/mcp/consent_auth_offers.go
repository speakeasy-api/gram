package mcp

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	authclientsrepo "github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
)

// configuredAuthOffer is configuration, not permission or credential readiness.
// An absent snapshot entry is unknown, never a declaration of no authentication.
// Pending setup retains legacy discovery eligibility. Consent and the serving
// resolver remain the authorities for authorizing and selecting a credential.
type configuredAuthOffer struct {
	clients []upstreamclientsrepo.ListServerConsentAuthClientsRow
	pending bool
}

func (s *Service) configuredAuthOffers(ctx context.Context, organizationID string, ids []uuid.UUID) (map[uuid.UUID]configuredAuthOffer, error) {
	offers := make(map[uuid.UUID]configuredAuthOffer, len(ids))
	if len(ids) == 0 {
		return offers, nil
	}
	rows, err := upstreamclientsrepo.New(s.db).ListServerConsentAuthClients(ctx, upstreamclientsrepo.ListServerConsentAuthClientsParams{OrganizationID: organizationID, McpServerIds: ids})
	if err != nil {
		return nil, fmt.Errorf("list configured auth offers: %w", err)
	}
	for _, row := range rows {
		offer := offers[row.McpServerID]
		offer.pending = offer.pending || row.UpstreamAuthPending
		if row.AuthClientID.Valid {
			offer.clients = append(offer.clients, row)
		}
		offers[row.McpServerID] = offer
	}
	return offers, nil
}

func (o configuredAuthOffer) personalRegistrations() []uuid.UUID {
	var ids []uuid.UUID
	for _, client := range o.clients {
		if client.RemoteSessionClientID.Valid && (client.ClientType.String == authz.AuthClientTypeRemoteSession || client.ClientType.String == authz.AuthClientTypeEMA) {
			ids = append(ids, client.RemoteSessionClientID.UUID)
		}
	}
	return ids
}

// gatewayCapability checks only whether the gateway can supply this kind of
// authentication. It neither picks a client nor claims that setup is complete.
func (o configuredAuthOffer) gatewayCapability(tunneled bool, headers map[uuid.UUID][]proxy.ConfiguredHeader) (supported, oauth bool) {
	if len(o.clients) == 0 {
		return true, false
	}
	for _, client := range o.clients {
		clientOAuth := false
		switch client.ClientType.String {
		case authz.AuthClientTypeRemoteSession, authz.AuthClientTypeServiceAccount, authz.AuthClientTypeEMA:
			clientOAuth = true
		case authz.AuthClientTypeStaticCredential, authz.AuthClientTypePassThrough:
			if tunneled {
				continue
			}
		default:
			continue
		}
		if !tunneled {
			clientHeaders := headers[client.AuthClientID.UUID]
			if usable, _ := remoteMemberAuth(clientHeaders, clientOAuth); !usable {
				continue
			}
		}
		supported = true
		oauth = oauth || clientOAuth
	}
	return supported, oauth
}

// Only presence and source metadata participate in discovery. Never decrypt a
// credential before its permission and readiness are checked by the resolver.
func (s *Service) gatewayAuthHeaderMetadata(ctx context.Context, organizationID string, ids []uuid.UUID) (map[uuid.UUID][]proxy.ConfiguredHeader, error) {
	rows, err := authclientsrepo.New(s.db).ListAuthClientHeaders(ctx, authclientsrepo.ListAuthClientHeadersParams{OrganizationID: organizationID, AuthClientIds: ids})
	if err != nil {
		return nil, fmt.Errorf("list gateway auth header metadata: %w", err)
	}
	headers := make(map[uuid.UUID][]proxy.ConfiguredHeader)
	for _, row := range rows {
		value := ""
		if row.HasStoredValue {
			value = "configured"
		}
		headers[row.AuthClientID] = append(headers[row.AuthClientID], proxy.ConfiguredHeader{Name: row.Name, IsRequired: row.IsRequired, ValueFromRequestHeader: row.ValueFromRequestHeader.String, StaticValue: value})
	}
	return headers, nil
}

// Match the serving resolver's effective header source, without permission
// checks, acquiring tokens, or reading decrypted credentials. A setup gap is
// not a discovery veto; consent explains it and serving still refuses it.
func (s *Service) gatewayOfferCapability(ctx context.Context, organizationID string, member metaMember, offer configuredAuthOffer, headers map[uuid.UUID][]proxy.ConfiguredHeader) (bool, error) {
	if member.tunneledServerID.Valid {
		supported, _ := offer.gatewayCapability(true, nil)
		return supported, nil
	}
	var source, nonCredential []proxy.ConfiguredHeader
	if member.remoteServerID.Valid {
		rows, err := remotemcprepo.New(s.db).ListServerHeaderMetadata(ctx, remotemcprepo.ListServerHeaderMetadataParams{RemoteMcpServerID: member.remoteServerID.UUID, OrganizationID: organizationID})
		if err != nil {
			return false, fmt.Errorf("list gateway source header metadata: %w", err)
		}
		for _, row := range rows {
			value := ""
			if row.HasStoredValue {
				value = "configured"
			}
			header := proxy.ConfiguredHeader{Name: row.Name, IsRequired: row.IsRequired, ValueFromRequestHeader: row.ValueFromRequestHeader.String, StaticValue: value}
			source = append(source, header)
			if !proxy.IsCredentialHeader(row.Name, row.IsSecret, row.ValueFromRequestHeader.String != "") {
				nonCredential = append(nonCredential, header)
			}
		}
	}
	if len(offer.clients) == 0 {
		supported, _ := remoteMemberAuth(nonCredential, false)
		return supported, nil
	}
	for _, client := range offer.clients {
		single := configuredAuthOffer{clients: []upstreamclientsrepo.ListServerConsentAuthClientsRow{client}, pending: false}
		supported, oauth := single.gatewayCapability(false, nil)
		if !supported {
			continue
		}
		// Reuse the authoritative resolver's metadata decision rather than copy
		// its administrator/backfill, stale-copy, and identity-chaining rules.
		rows, err := upstreamclientsrepo.New(s.db).GetAuthClientSelection(ctx, upstreamclientsrepo.GetAuthClientSelectionParams{Proposed: true, CredentialID: uuid.Nil, ProposedRegistrationID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, ProposedAuthClientID: client.AuthClientID, CredentialKind: upstreamclients.CredentialKindUserSession, McpServerIds: []uuid.UUID{member.serverID}, OrganizationID: organizationID})
		if err != nil {
			return false, fmt.Errorf("resolve gateway offer header source: %w", err)
		}
		if len(rows) != 1 {
			continue
		}
		resolution := decideAuthClient(rows[0])
		effective := proxy.MergeAuthClientHeaders(proxy.HeaderPolicyRemote, nonCredential, headers[client.AuthClientID.UUID])
		if resolution.outcome == authClientLegacy && !resolution.sendsClientHeaders() {
			effective = source
		}
		if supported, _ := remoteMemberAuth(effective, oauth); supported {
			return true, nil
		}
	}
	return false, nil
}
