package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
	"github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// requireOrganizationAdmin resolves the session's auth context and checks the
// org:admin scope, the gate the scan target endpoints share with the fleet
// configuration endpoints.
func (s *Service) requireOrganizationAdmin(ctx context.Context) (*contextvalues.AuthContext, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{
		Scope:        authz.ScopeOrgAdmin,
		ResourceKind: "",
		ResourceID:   authCtx.ActiveOrganizationID,
		Dimensions:   nil,
	}); err != nil {
		return nil, err
	}
	return authCtx, nil
}

func (s *Service) ListAiScanTargets(ctx context.Context, _ *gen.ListAiScanTargetsPayload) (*gen.ListAiScanTargetsResult, error) {
	authCtx, err := s.requireOrganizationAdmin(ctx)
	if err != nil {
		return nil, err
	}
	list, err := aitargets.LoadOrganizationList(ctx, s.repo, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list ai scan targets").LogError(ctx, s.logger)
	}
	return mv.BuildAiScanTargetListView(list), nil
}

func (s *Service) UpsertAiScanTarget(ctx context.Context, payload *gen.UpsertAiScanTargetPayload) (*gen.AiScanTargetMutationResult, error) {
	authCtx, err := s.requireOrganizationAdmin(ctx)
	if err != nil {
		return nil, err
	}
	organizationID := authCtx.ActiveOrganizationID

	target := aitargets.Target{
		ID:            strings.TrimSpace(payload.ID),
		DisplayName:   strings.TrimSpace(payload.DisplayName),
		Category:      aitargets.Category(strings.TrimSpace(payload.Category)),
		Signatures:    signaturesFromPayload(payload.Signatures),
		VersionHint:   nil,
		GatewayClient: gatewayClientFromPayload(payload.GatewayClient),
		Enabled:       payload.Enabled,
	}
	if key := strings.TrimSpace(conv.PtrValOr(payload.VersionPlistKey, "")); key != "" {
		target.VersionHint = &aitargets.VersionHint{PlistKey: key}
	}
	if err := aitargets.ValidateTarget(target); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "%v", err)
	}
	// Built-in targets are system-supplied and read-only, the same contract
	// Detection Rules holds its built-ins to. The one change an organization
	// may make is switching one off, so a write under a built-in's id is
	// accepted only when it carries that built-in's definition unchanged.
	//
	// Enforced here and not only in the dashboard because the API is the
	// contract: a hand-rolled call must not be able to silently redefine what
	// every agent in the organization probes for.
	if builtin, isBuiltin := aitargets.DefaultByID(target.ID); isBuiltin && !aitargets.SameDefinition(builtin, target) {
		return nil, oops.E(oops.CodeBadRequest, nil, "%q is a built-in scan target and cannot be edited; it can only be enabled or disabled", target.ID)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin ai scan target update").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)
	if err := queries.AcquireAIScanCatalogLock(ctx, organizationID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "serialize ai scan target update").LogError(ctx, s.logger)
	}

	before, err := aiScanTargetBefore(ctx, queries, organizationID, target.ID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read existing ai scan target").LogError(ctx, s.logger)
	}
	if _, err := queries.UpsertAIScanTarget(ctx, aitargets.UpsertParams(organizationID, target)); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "save ai scan target").LogError(ctx, s.logger)
	}
	list, err := aitargets.LoadOrganizationList(ctx, queries, organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read updated ai scan targets").LogError(ctx, s.logger)
	}
	// The served set is bounded by what agents accept; the check runs under
	// the organization's lock so concurrent writers cannot slip past it.
	if err := aitargets.ValidateServed(list.Snapshot.Targets()); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "%v", err)
	}
	counter, err := queries.BumpAIScanCatalogVersion(ctx, organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "bump ai scan list version").LogError(ctx, s.logger)
	}
	entry, ok := list.Entry(target.ID)
	if !ok {
		return nil, oops.E(oops.CodeUnexpected, nil, "saved ai scan target %q missing from the organization's list", target.ID).LogError(ctx, s.logger)
	}

	actor := urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)
	after := entry.Target
	if before == nil {
		err = s.audit.LogAiScanTargetCreate(ctx, dbtx, audit.LogAiScanTargetCreateEvent{
			OrganizationID:            organizationID,
			Actor:                     actor,
			ActorDisplayName:          authCtx.Email,
			ActorSlug:                 nil,
			AiScanTargetURN:           urn.NewAiScanTarget(organizationID, after.ID),
			TargetDisplayName:         after.DisplayName,
			AiScanTargetSnapshotAfter: &after,
		})
	} else {
		err = s.audit.LogAiScanTargetUpdate(ctx, dbtx, audit.LogAiScanTargetUpdateEvent{
			OrganizationID:             organizationID,
			Actor:                      actor,
			ActorDisplayName:           authCtx.Email,
			ActorSlug:                  nil,
			AiScanTargetURN:            urn.NewAiScanTarget(organizationID, after.ID),
			TargetDisplayName:          after.DisplayName,
			AiScanTargetSnapshotBefore: before,
			AiScanTargetSnapshotAfter:  &after,
		})
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log ai scan target change").LogError(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit ai scan target update").LogError(ctx, s.logger)
	}

	return &gen.AiScanTargetMutationResult{
		ListVersion: int(aitargets.ListVersion(counter)),
		Target:      mv.BuildAiScanTargetView(entry),
	}, nil
}

func (s *Service) DeleteAiScanTarget(ctx context.Context, payload *gen.DeleteAiScanTargetPayload) (*gen.DeleteAiScanTargetResult, error) {
	authCtx, err := s.requireOrganizationAdmin(ctx)
	if err != nil {
		return nil, err
	}
	organizationID := authCtx.ActiveOrganizationID
	id := strings.TrimSpace(payload.ID)

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin ai scan target delete").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)
	if err := queries.AcquireAIScanCatalogLock(ctx, organizationID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "serialize ai scan target delete").LogError(ctx, s.logger)
	}

	key := repo.GetAIScanTargetForUpdateParams{OrganizationID: organizationID, ID: id}
	row, err := queries.GetAIScanTargetForUpdate(ctx, key)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, nil, "ai scan target %q is not one this organization added or customized", id)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "read ai scan target").LogError(ctx, s.logger)
	}
	if _, err := queries.DeleteAIScanTarget(ctx, repo.DeleteAIScanTargetParams{OrganizationID: organizationID, ID: id}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "delete ai scan target").LogError(ctx, s.logger)
	}
	// Dropping a customization serves the default again, which can grow the
	// served set, so it is checked like any other write.
	list, err := aitargets.LoadOrganizationList(ctx, queries, organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read updated ai scan targets").LogError(ctx, s.logger)
	}
	if err := aitargets.ValidateServed(list.Snapshot.Targets()); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "%v", err)
	}
	counter, err := queries.BumpAIScanCatalogVersion(ctx, organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "bump ai scan list version").LogError(ctx, s.logger)
	}

	before := aitargets.EntryFromRow(row).Target
	if err := s.audit.LogAiScanTargetDelete(ctx, dbtx, audit.LogAiScanTargetDeleteEvent{
		OrganizationID:             organizationID,
		Actor:                      urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:           authCtx.Email,
		ActorSlug:                  nil,
		AiScanTargetURN:            urn.NewAiScanTarget(organizationID, before.ID),
		TargetDisplayName:          before.DisplayName,
		AiScanTargetSnapshotBefore: &before,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log ai scan target delete").LogError(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit ai scan target delete").LogError(ctx, s.logger)
	}

	return &gen.DeleteAiScanTargetResult{ListVersion: int(aitargets.ListVersion(counter))}, nil
}

// aiScanTargetBefore is what the organization's list held for id ahead of a
// write: its own row, locked for the transaction; else the default the write
// customizes; else nothing.
func aiScanTargetBefore(ctx context.Context, queries *repo.Queries, organizationID string, id string) (*aitargets.Target, error) {
	row, err := queries.GetAIScanTargetForUpdate(ctx, repo.GetAIScanTargetForUpdateParams{OrganizationID: organizationID, ID: id})
	switch {
	case err == nil:
		target := aitargets.EntryFromRow(row).Target
		return &target, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return nil, fmt.Errorf("lock ai scan target %q: %w", id, err)
	}
	for _, target := range aitargets.Defaults() {
		if target.ID == id {
			return &target, nil
		}
	}
	return nil, nil
}

// gatewayClientFromPayload keeps empty matcher lists nil rather than empty, so
// a target nobody has linked to a gateway caller stays out of the served-list
// fingerprint entirely.
func gatewayClientFromPayload(gateway *gen.AiScanTargetGatewayClient) aitargets.GatewayClient {
	if gateway == nil {
		return aitargets.GatewayClient{CIMDVendorKeys: nil, OAuthClientIDs: nil, ClientInfoNames: nil}
	}
	return aitargets.GatewayClient{
		CIMDVendorKeys:  nilIfEmpty(trimAll(gateway.CimdVendorKeys)),
		OAuthClientIDs:  nilIfEmpty(trimAll(gateway.OauthClientIds)),
		ClientInfoNames: nilIfEmpty(trimAll(gateway.ClientInfoNames)),
	}
}

func nilIfEmpty(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return values
}

func signaturesFromPayload(signatures *gen.AiScanTargetSignatures) aitargets.Signatures {
	if signatures == nil {
		return aitargets.Signatures{BundleIDs: []string{}, Binaries: []string{}, ConfigDirs: []string{}, ProcessNames: []string{}}
	}
	return aitargets.Signatures{
		BundleIDs:    trimAll(signatures.BundleIds),
		Binaries:     trimAll(signatures.Binaries),
		ConfigDirs:   trimAll(signatures.ConfigDirs),
		ProcessNames: trimAll(signatures.ProcessNames),
	}
}

// trimAll trims entries and drops blank ones.
func trimAll(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
