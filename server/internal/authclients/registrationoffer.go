package authclients

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// RegistrationOffer names an OAuth registration to offer on one MCP server.
type RegistrationOffer struct {
	// OrganizationID is the server's organization.
	OrganizationID string

	// ProjectID is the server's project.
	ProjectID uuid.UUID

	// MCPServerID is the server that offers the registration.
	MCPServerID uuid.UUID

	// RegistrationID is the remote session client callers sign in through.
	RegistrationID uuid.UUID

	// ProviderName names the new auth client after the registration's Remote
	// Identity Provider. When empty the client takes its type's default name.
	ProviderName string

	// Actor is recorded on the audit events and, for a user, as the client's
	// creator.
	Actor urn.Principal

	// ActorDisplayName is the actor's display name, if known.
	ActorDisplayName *string
}

// OfferRegistrationOnServer offers an OAuth registration on one MCP server as
// an administrator-owned auth client, inside the caller's transaction. It
// writes nothing else: the registration gets no binding to a user session
// issuer and the server no upstream client association, so the offer reaches
// this server alone and follows neither. A server that already offers the
// registration is left as it is.
//
// It performs no authorization. The caller holds the server's row lock and
// the auth client writer scope. A refusal is an *AttachRefusedError.
func OfferRegistrationOnServer(ctx context.Context, tx pgx.Tx, auditLogger *audit.Logger, offer RegistrationOffer) error {
	queries := repo.New(tx)
	offered, err := queries.ServerOffersRegistration(ctx, repo.ServerOffersRegistrationParams{
		McpServerID:           offer.MCPServerID,
		ProjectID:             offer.ProjectID,
		OrganizationID:        offer.OrganizationID,
		RemoteSessionClientID: offer.RegistrationID,
	})
	if err != nil {
		return fmt.Errorf("check mcp server offers registration: %w", err)
	}
	if offered {
		return nil
	}

	registration, err := queries.GetAuthClientRegistration(ctx, repo.GetAuthClientRegistrationParams{
		ProjectID:      uuid.NullUUID{UUID: offer.ProjectID, Valid: true},
		OrganizationID: offer.OrganizationID,
		ID:             offer.RegistrationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return &AttachRefusedError{Code: AttachNotFound, Message: "OAuth registration not found in the server's project or organization"}
	}
	if err != nil {
		return fmt.Errorf("get oauth registration: %w", err)
	}

	var clientType string
	switch registration.CredentialOwner {
	case registrationOwnerSubject:
		clientType = authz.AuthClientTypeRemoteSession
	case registrationOwnerSelf:
		clientType = authz.AuthClientTypeServiceAccount
	default:
		return &AttachRefusedError{Code: AttachInvalid, Message: "this OAuth registration records no credential owner, so no auth client can be created for it"}
	}
	if clientType == authz.AuthClientTypeServiceAccount && !registration.ProjectID.Valid && !registration.OrganizationID.Valid {
		return &AttachRefusedError{Code: AttachInvalid, Message: "a service account cannot use a platform-wide OAuth registration; register the service account in your organization"}
	}

	name := offer.ProviderName
	if name == "" {
		name = authclientbackfill.DefaultName(clientType)
	}
	createdBy := ""
	if offer.Actor.Type == urn.PrincipalTypeUser {
		createdBy = offer.Actor.ID
	}
	// Scoped like the registration: a project registration's client belongs
	// to that project, an organization or platform one's to the organization.
	created, err := queries.CreateAuthClient(ctx, repo.CreateAuthClientParams{
		OrganizationID:        offer.OrganizationID,
		ProjectID:             registration.ProjectID,
		ClientType:            clientType,
		Name:                  name,
		CreatedByUserID:       conv.ToPGTextEmpty(createdBy),
		RemoteSessionClientID: uuid.NullUUID{UUID: offer.RegistrationID, Valid: true},
		ClientAttachmentScope: registration.AttachmentScope,
	})
	if err != nil {
		return fmt.Errorf("create auth client: %w", err)
	}

	server, err := attachTx(ctx, queries, offer.OrganizationID, created, offer.MCPServerID)
	if err != nil {
		return err
	}

	views, err := clientViews(ctx, queries, offer.OrganizationID, []repo.McpAuthClient{created})
	if err != nil {
		return fmt.Errorf("build auth client view: %w", err)
	}
	if err := auditLogger.LogAuthClientCreate(ctx, tx, audit.LogAuthClientUpdateEvent{
		OrganizationID:           offer.OrganizationID,
		ProjectID:                created.ProjectID.UUID,
		Actor:                    offer.Actor,
		ActorDisplayName:         offer.ActorDisplayName,
		ActorSlug:                nil,
		AuthClientURN:            urn.NewAuthClient(created.ID),
		AuthClientName:           created.Name,
		AuthClientSnapshotBefore: nil,
		AuthClientSnapshotAfter:  views[0],
	}); err != nil {
		return fmt.Errorf("log auth client create: %w", err)
	}
	return logAttach(ctx, tx, auditLogger, offer.OrganizationID, offer.Actor, offer.ActorDisplayName, created, server)
}
