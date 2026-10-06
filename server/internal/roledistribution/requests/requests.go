// Package requests publishes transactional role distribution work without depending on its processor.
package requests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	roledistributionv1 "github.com/speakeasy-api/gram/infra/gen/gram/role_distribution/v1"
	"github.com/speakeasy-api/gram/server/internal/outbox"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/roledistribution/repo"
)

// Request identifies exactly one setup, global fanout, or organization bootstrap.
type Request struct {
	Cursor                  string `json:"cursor"`
	OrganizationID          string `json:"organization_id"`
	RoleURN                 string `json:"role_urn"`
	GlobalRoleID            string `json:"global_role_id"`
	BootstrapOrganizationID string `json:"bootstrap_organization_id"`
}

// Publish records the request in the source transaction. Global fanout events have
// no organization scope; the outbox intentionally permits an empty organization ID.
func Publish(ctx context.Context, tx pgx.Tx, request Request) error {
	targets := 0
	for _, target := range []string{request.RoleURN, request.GlobalRoleID, request.BootstrapOrganizationID} {
		if target != "" {
			targets++
		}
	}
	if (request.RoleURN != "" && request.Cursor != "") || targets != 1 || (request.RoleURN != "" && request.OrganizationID == "") || (request.RoleURN == "" && request.OrganizationID != "") {
		return fmt.Errorf("role distribution request requires exactly one target and an organization only for a setup")
	}
	orgID := request.OrganizationID
	if request.BootstrapOrganizationID != "" {
		orgID = request.BootstrapOrganizationID
	}
	_, err := outbox.Publish(ctx, tx, orgID, outbox.Message{PublicID: uuid.Nil, Attributes: nil, Proto: roledistributionv1.RoleDistributionSetupRequestedV1_builder{
		OrganizationId: new(request.OrganizationID), RoleUrn: new(request.RoleURN), GlobalRoleId: new(request.GlobalRoleID), BootstrapOrganizationId: new(request.BootstrapOrganizationID), Cursor: new(request.Cursor),
	}.Build()})
	if err != nil {
		return fmt.Errorf("publish role distribution request: %w", err)
	}
	return nil
}

// PublishAll publishes only the rows returned by the source statement's INSERT RETURNING clauses.
func PublishAll(ctx context.Context, tx pgx.Tx, data []byte) error {
	var requests []Request
	if err := json.Unmarshal(data, &requests); err != nil {
		return fmt.Errorf("decode role distribution requests: %w", err)
	}
	for _, request := range requests {
		if err := Publish(ctx, tx, request); err != nil {
			return err
		}
	}
	return nil
}

// LockOrganization serializes attempts and bounded expansion with staff toggles,
// including an enable when no feature row exists yet. Take this before row locks.
func LockOrganization(ctx context.Context, tx pgx.Tx, organizationID string) error {
	if err := repo.New(tx).LockOrganization(ctx, organizationID); err != nil {
		return fmt.Errorf("lock organization role distribution: %w", err)
	}
	return nil
}

// ResumeOrganization starts one explicit enumeration pass, not a retry loop.
// The caller holds LockOrganization and commits the feature change together with
// the outbox event. Setup reuses plugins and assignments without a completion ledger.
func ResumeOrganization(ctx context.Context, tx pgx.Tx, organizationID string) error {
	return Publish(ctx, tx, Request{OrganizationID: "", RoleURN: "", GlobalRoleID: "", BootstrapOrganizationID: organizationID, Cursor: ""})
}

// PublishFirstProject starts one organization pass when projectID is the
// organization's only active project. Setup skips projectless organizations,
// so this is the trigger that distributes roles created before any project.
// Concurrent first projects may each publish; setup reuses plugins and assignments.
func PublishFirstProject(ctx context.Context, tx pgx.Tx, organizationID string, projectID uuid.UUID) error {
	_, err := projectsrepo.New(tx).LockOtherActiveProject(ctx, projectsrepo.LockOtherActiveProjectParams{OrganizationID: organizationID, ProjectID: projectID})
	if err == nil {
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("check first organization project: %w", err)
	}
	return ResumeOrganization(ctx, tx, organizationID)
}
