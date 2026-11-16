package admin

import (
	"context"
	"strings"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

func (s *Service) EnableUpstreamAuthEnforcement(ctx context.Context, p *gen.EnableUpstreamAuthEnforcementPayload) (*gen.AdminUpstreamOAuthMigrationStatus, error) {
	auth, ok := contextvalues.GetAdminAuthContext(ctx)
	if !ok || auth == nil || auth.OIDCSubject == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	organizationID, err := s.canonicalAdminOrganizationID(ctx, p.OrganizationID)
	if err != nil {
		return nil, err
	}
	if !p.ShadowParityConfirmed || !p.CrossOrganizationDenialCommunicated {
		return nil, oops.E(oops.CodeInvalid, nil, "confirm the shadow evaluation agrees and the cross-organization denial was communicated before enabling enforcement")
	}
	status, err := s.upstreamOAuthMigrationStatus(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	if blockers := status.Enforcement.Blockers; len(blockers) > 0 {
		return nil, oops.E(oops.CodeInvalid, nil, "enforcement can't be enabled yet: %s", strings.Join(blockers, " "))
	}
	actor, displayName, _ := adminActor(ctx)
	if err := productfeatures.NewMutator(s.productFeatures, s.audit).SetFeature(ctx, organizationID, productfeatures.FeatureUpstreamAuthEnforced, true, productfeatures.MutationActor{Principal: actor, DisplayName: displayName, Slug: nil}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "enable upstream auth enforcement").LogError(ctx, s.logger)
	}
	if !status.Enforcement.Enabled {
		s.logger.InfoContext(ctx, "upstream auth enforcement enabled",
			attr.SlogAuditAction("upstream_auth_enforcement:enable"),
			attr.SlogAuditSubject("organization"),
			attr.SlogAuditSubjectID(organizationID),
			attr.SlogAdminOIDCSubject(auth.OIDCSubject),
		)
	}
	return s.upstreamOAuthMigrationStatus(ctx, organizationID)
}

func (s *Service) DisableUpstreamAuthEnforcement(ctx context.Context, p *gen.DisableUpstreamAuthEnforcementPayload) (*gen.AdminUpstreamOAuthMigrationStatus, error) {
	auth, ok := contextvalues.GetAdminAuthContext(ctx)
	if !ok || auth == nil || auth.OIDCSubject == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	organizationID, err := s.canonicalAdminOrganizationID(ctx, p.OrganizationID)
	if err != nil {
		return nil, err
	}
	enabled, err := s.productFeatures.IsFeatureEnabledUncached(ctx, organizationID, productfeatures.FeatureUpstreamAuthEnforced)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read upstream auth enforcement").LogError(ctx, s.logger)
	}
	actor, displayName, _ := adminActor(ctx)
	if err := productfeatures.NewMutator(s.productFeatures, s.audit).SetFeature(ctx, organizationID, productfeatures.FeatureUpstreamAuthEnforced, false, productfeatures.MutationActor{Principal: actor, DisplayName: displayName, Slug: nil}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "disable upstream auth enforcement").LogError(ctx, s.logger)
	}
	if enabled {
		s.logger.InfoContext(ctx, "upstream auth enforcement disabled",
			attr.SlogAuditAction("upstream_auth_enforcement:disable"),
			attr.SlogAuditSubject("organization"),
			attr.SlogAuditSubjectID(organizationID),
			attr.SlogAdminOIDCSubject(auth.OIDCSubject),
		)
	}
	return s.upstreamOAuthMigrationStatus(ctx, organizationID)
}

// upstreamAuthEnforcement reads whether enforcement is on for the
// organization and what keeps it from being turned on, from its migration
// status. Choices left ambiguous and profile gaps do not block it: those
// credentials and turns fail with an actionable error under enforcement,
// never falling back to another client.
func (s *Service) upstreamAuthEnforcement(ctx context.Context, status *gen.AdminUpstreamOAuthMigrationStatus) *gen.AdminUpstreamAuthEnforcement {
	blockers := []string{}
	if !status.Migrated {
		blockers = append(blockers, "The upstream OAuth migration is incomplete.")
	}
	switch a := status.AuthClients; {
	case a == nil:
		blockers = append(blockers, "Typed auth clients couldn't be checked.")
	case a.InProgress:
		blockers = append(blockers, "A typed auth client run is in progress.")
	default:
		if !a.Complete {
			blockers = append(blockers, "Typed auth clients are not all recorded.")
		}
		// The selection and grant counts are optional in the status, so a
		// count that is missing is not taken as zero.
		switch {
		case a.PendingSelectionMappings == nil:
			blockers = append(blockers, "Credential choices couldn't be checked.")
		case *a.PendingSelectionMappings > 0:
			blockers = append(blockers, "Credential choices still need their auth client named.")
		}
		switch {
		case a.PendingGrants == nil:
			blockers = append(blockers, "Default auth permissions couldn't be checked.")
		case *a.PendingGrants > 0:
			blockers = append(blockers, "Default auth permissions are not all granted.")
		}
	}
	switch p := status.ExecutionProfiles; {
	case p == nil:
		blockers = append(blockers, "Assistant profiles couldn't be checked.")
	case p.PendingVersions > 0:
		blockers = append(blockers, "Assistant profiles are not all recorded.")
	}
	enabled, err := s.productFeatures.IsFeatureEnabledUncached(ctx, status.OrganizationID, productfeatures.FeatureUpstreamAuthEnforced)
	if err != nil {
		// The status, and a run that already committed, still answer; the
		// blocker keeps enforcement from being turned on meanwhile.
		s.logger.ErrorContext(ctx, "read upstream auth enforcement", attr.SlogOrganizationID(status.OrganizationID), attr.SlogError(err))
		blockers = append(blockers, "Whether enforcement is on couldn't be read.")
	}
	return &gen.AdminUpstreamAuthEnforcement{Enabled: enabled, Ready: len(blockers) == 0, Blockers: blockers}
}
