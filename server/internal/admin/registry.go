package admin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func (s *Service) registryError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	var invalid *mcpregistry.InvalidError
	switch {
	case errors.As(err, &invalid):
		messages := make([]string, 0, len(invalid.Issues))
		for _, issue := range invalid.Issues {
			messages = append(messages, issue.Path+": "+issue.Message)
		}
		return oops.E(oops.CodeInvalid, nil, "%s", strings.Join(messages, "; "))
	case errors.Is(err, mcpregistry.ErrInvalidToken), errors.Is(err, mcpregistry.ErrInvalidCursor), errors.Is(err, mcpregistry.ErrInvalidListOptions):
		return oops.E(oops.CodeBadRequest, nil, "%s", err.Error())
	case errors.Is(err, mcpregistry.ErrNotFound):
		return oops.E(oops.CodeNotFound, nil, "registry entry not found")
	case errors.Is(err, mcpregistry.ErrConflict), errors.Is(err, mcpregistry.ErrStageAStructure):
		return oops.E(oops.CodeConflict, nil, "registry mutation conflict")
	default:
		s.logger.ErrorContext(ctx, "registry operation failed", attr.SlogError(err))
		return oops.E(oops.CodeUnexpected, err, "registry operation failed")
	}
}
func registryIssues(issues []mcpregistry.Issue) []*gen.AdminRegistryIssue {
	result := make([]*gen.AdminRegistryIssue, 0, len(issues))
	for _, issue := range issues {
		result = append(result, &gen.AdminRegistryIssue{Path: issue.Path, Message: issue.Message})
	}
	return result
}
func (s *Service) registryEntry(e mcpregistry.Entry) *gen.AdminRegistryEntry {
	return &gen.AdminRegistryEntry{ID: e.ID.String(), DataJSON: string(e.Data), Published: e.Published, CreatedAt: e.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: mcpregistry.Token(e), Issues: registryIssues(s.registry.Validate(e.Data))}
}
func (s *Service) ListRegistryEntries(ctx context.Context, p *gen.ListRegistryEntriesPayload) (*gen.AdminRegistryPage, error) {
	page, err := s.registry.List(ctx, mcpregistry.ListOptions{Query: conv.PtrValOrEmpty(p.Query, ""), Published: p.Published, Cursor: conv.PtrValOrEmpty(p.Cursor, ""), Limit: conv.PtrValOrEmpty(p.Limit, 0)})
	if err != nil {
		return nil, s.registryError(ctx, err)
	}
	entries := make([]*gen.AdminRegistrySummary, 0, len(page.Entries))
	for _, e := range page.Entries {
		entries = append(entries, &gen.AdminRegistrySummary{ID: e.ID.String(), Name: e.Name, Published: e.Published, UpdatedAt: e.UpdatedAt, Issues: registryIssues(e.Issues)})
	}
	return &gen.AdminRegistryPage{Entries: entries, NextCursor: conv.PtrEmpty(page.NextCursor)}, nil
}
func (s *Service) GetRegistryEntry(ctx context.Context, p *gen.GetRegistryEntryPayload) (*gen.AdminRegistryEntry, error) {
	id, err := uuid.Parse(p.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid registry entry id")
	}
	e, err := s.registry.Get(ctx, id)
	if err != nil {
		return nil, s.registryError(ctx, err)
	}
	return s.registryEntry(e), nil
}
func (s *Service) registryMutationResult(ctx context.Context, action string, e mcpregistry.Entry, err error) (*gen.AdminRegistryEntry, error) {
	actor, _, _ := adminActor(ctx)
	if err != nil {
		s.logger.WarnContext(ctx, "registry mutation failed", attr.SlogAuditAction(action), attr.SlogAuthorizationActorID(actor.String()))
		return nil, s.registryError(ctx, err)
	}
	s.logger.InfoContext(ctx, "registry mutation succeeded", attr.SlogAuditAction(action), attr.SlogAuthorizationActorID(actor.String()), attr.SlogRegistryEntryID(e.ID.String()), attr.SlogRegistryUpdatedAt(mcpregistry.Token(e)))
	return s.registryEntry(e), nil
}
func (s *Service) CreateRegistryEntry(ctx context.Context, p *gen.CreateRegistryEntryPayload) (*gen.AdminRegistryEntry, error) {
	e, err := s.registry.Create(ctx, json.RawMessage(p.DataJSON))
	return s.registryMutationResult(ctx, "create", e, err)
}
func (s *Service) SaveRegistryEntry(ctx context.Context, p *gen.SaveRegistryEntryPayload) (*gen.AdminRegistryEntry, error) {
	id, err := uuid.Parse(p.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid registry entry id")
	}
	if _, err := mcpregistry.ParseToken(p.UpdatedAt); err != nil {
		return nil, s.registryError(ctx, err)
	}
	e, err := s.registry.Save(ctx, id, p.UpdatedAt, json.RawMessage(p.DataJSON))
	return s.registryMutationResult(ctx, "save", e, err)
}
func (s *Service) SetRegistryEntryPublished(ctx context.Context, p *gen.SetRegistryEntryPublishedPayload) (*gen.AdminRegistryEntry, error) {
	id, err := uuid.Parse(p.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid registry entry id")
	}
	if _, err := mcpregistry.ParseToken(p.UpdatedAt); err != nil {
		return nil, s.registryError(ctx, err)
	}
	e, err := s.registry.SetPublished(ctx, id, p.UpdatedAt, p.Published)
	return s.registryMutationResult(ctx, "set_published", e, err)
}
