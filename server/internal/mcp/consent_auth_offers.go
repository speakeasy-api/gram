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

// An absent snapshot entry is unknown, not an unauthenticated server.
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

// Discovery needs header presence and source metadata, so it can avoid decrypting secrets.
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

type gatewayOfferMetadata struct {
	headers       map[uuid.UUID][]proxy.ConfiguredHeader
	source        map[uuid.UUID][]proxy.ConfiguredHeader
	nonCredential map[uuid.UUID][]proxy.ConfiguredHeader
	selections    map[[2]uuid.UUID]upstreamclientsrepo.GetAuthClientSelectionRow
}

func (s *Service) loadGatewayOfferMetadata(ctx context.Context, organizationID string, members []metaMember, offers map[uuid.UUID]configuredAuthOffer) (gatewayOfferMetadata, error) {
	metadata := gatewayOfferMetadata{
		headers:       nil,
		source:        make(map[uuid.UUID][]proxy.ConfiguredHeader),
		nonCredential: make(map[uuid.UUID][]proxy.ConfiguredHeader),
		selections:    make(map[[2]uuid.UUID]upstreamclientsrepo.GetAuthClientSelectionRow),
	}
	var serverIDs, remoteIDs, clientIDs []uuid.UUID
	for _, member := range members {
		offer, known := offers[member.serverID]
		if !known || !member.remoteServerID.Valid || (offer.pending && len(offer.clients) == 0) {
			continue
		}
		serverIDs = append(serverIDs, member.serverID)
		remoteIDs = append(remoteIDs, member.remoteServerID.UUID)
		for _, client := range offer.clients {
			clientIDs = append(clientIDs, client.AuthClientID.UUID)
		}
	}
	if len(serverIDs) == 0 {
		return metadata, nil
	}
	var err error
	metadata.headers, err = s.gatewayAuthHeaderMetadata(ctx, organizationID, clientIDs)
	if err != nil {
		return metadata, err
	}
	rows, err := remotemcprepo.New(s.db).ListServerHeaderMetadata(ctx, remotemcprepo.ListServerHeaderMetadataParams{RemoteMcpServerIds: remoteIDs, OrganizationID: organizationID})
	if err != nil {
		return metadata, fmt.Errorf("list gateway source header metadata: %w", err)
	}
	for _, row := range rows {
		value := ""
		if row.HasStoredValue {
			value = "configured"
		}
		header := proxy.ConfiguredHeader{Name: row.Name, IsRequired: row.IsRequired, ValueFromRequestHeader: row.ValueFromRequestHeader.String, StaticValue: value}
		metadata.source[row.RemoteMcpServerID] = append(metadata.source[row.RemoteMcpServerID], header)
		if !proxy.IsCredentialHeader(row.Name, row.IsSecret, row.ValueFromRequestHeader.String != "") {
			metadata.nonCredential[row.RemoteMcpServerID] = append(metadata.nonCredential[row.RemoteMcpServerID], header)
		}
	}
	selections, err := upstreamclientsrepo.New(s.db).GetAuthClientSelection(ctx, upstreamclientsrepo.GetAuthClientSelectionParams{Proposed: true, ProposeAllClients: true, CredentialID: uuid.Nil, ProposedRegistrationID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, ProposedAuthClientID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, CredentialKind: upstreamclients.CredentialKindUserSession, McpServerIds: serverIDs, OrganizationID: organizationID})
	if err != nil {
		return metadata, fmt.Errorf("resolve gateway offer header sources: %w", err)
	}
	for _, row := range selections {
		metadata.selections[[2]uuid.UUID{row.McpServerID, row.AuthClientID.UUID}] = row
	}
	return metadata, nil
}

// Use the serving resolver's effective header source so discovery matches execution.
func gatewayOfferCapability(member metaMember, offer configuredAuthOffer, metadata gatewayOfferMetadata) bool {
	if member.tunneledServerID.Valid {
		supported, _ := offer.gatewayCapability(true, nil)
		return supported
	}
	for _, client := range offer.clients {
		single := configuredAuthOffer{clients: []upstreamclientsrepo.ListServerConsentAuthClientsRow{client}, pending: false}
		supported, oauth := single.gatewayCapability(false, nil)
		if !supported {
			continue
		}
		row, ok := metadata.selections[[2]uuid.UUID{member.serverID, client.AuthClientID.UUID}]
		if !ok {
			continue
		}
		resolution := decideAuthClient(row)
		effective := proxy.MergeAuthClientHeaders(proxy.HeaderPolicyRemote, metadata.nonCredential[member.remoteServerID.UUID], metadata.headers[client.AuthClientID.UUID])
		if resolution.outcome == authClientLegacy && !resolution.sendsClientHeaders() {
			effective = metadata.source[member.remoteServerID.UUID]
		}
		if supported, _ := remoteMemberAuth(effective, oauth); supported {
			return true
		}
	}
	return false
}
