package admin

import (
	"context"
	"fmt"
	gen "github.com/speakeasy-api/gram/server/gen/admin"
	adminrsgen "github.com/speakeasy-api/gram/server/gen/admin_remote_sessions"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// SetRemoteSessionService supplies only global-operation dependencies.
func (s *Service) SetRemoteSessionService(service *remotesessions.Service) {
	s.remoteSessions = service
}
func (s *Service) CreateGlobalIssuer(ctx context.Context, payload *gen.CreateGlobalIssuerPayload) (*types.RemoteSessionIssuer, error) {
	a, ok := contextvalues.GetAdminAuthContext(ctx)
	if !ok || a == nil || a.SessionID == "" || a.OIDCSubject == "" {
		err := oops.C(oops.CodeUnauthorized)
		return nil, err
	}
	if s.remoteSessions == nil {
		err := oops.C(oops.CodeUnavailable)
		return nil, err
	}
	v, err := s.remoteSessions.CreateGlobalIssuer(ctx, &adminrsgen.CreateGlobalIssuerPayload{SessionToken: nil, Slug: payload.Slug, Issuer: payload.Issuer, Name: payload.Name, LogoAssetID: payload.LogoAssetID, ClientSetupDocumentationURL: payload.ClientSetupDocumentationURL, AuthorizationEndpoint: payload.AuthorizationEndpoint, TokenEndpoint: payload.TokenEndpoint, RevocationEndpoint: payload.RevocationEndpoint, RegistrationEndpoint: payload.RegistrationEndpoint, JwksURI: payload.JwksURI, ServiceDocumentation: payload.ServiceDocumentation, OpPolicyURI: payload.OpPolicyURI, OpTosURI: payload.OpTosURI, ScopesSupported: payload.ScopesSupported, GrantTypesSupported: payload.GrantTypesSupported, ResponseTypesSupported: payload.ResponseTypesSupported, TokenEndpointAuthMethodsSupported: payload.TokenEndpointAuthMethodsSupported, CodeChallengeMethodsSupported: payload.CodeChallengeMethodsSupported, Oidc: payload.Oidc, Passthrough: payload.Passthrough, ClientIDMetadataDocumentSupported: payload.ClientIDMetadataDocumentSupported, UserinfoEndpoint: payload.UserinfoEndpoint, IntrospectionEndpoint: payload.IntrospectionEndpoint, IntrospectionEndpointAuthMethodsSupported: payload.IntrospectionEndpointAuthMethodsSupported, IDTokenSigningAlgValuesSupported: payload.IDTokenSigningAlgValuesSupported, ClaimsSupported: payload.ClaimsSupported, BackchannelLogoutSupported: payload.BackchannelLogoutSupported, AuthorizationResponseIssParameterSupported: payload.AuthorizationResponseIssParameterSupported, ScopeOverride: payload.ScopeOverride, ResourceIndicatorSupported: payload.ResourceIndicatorSupported})
	if err != nil {
		return nil, fmt.Errorf("admin CreateGlobalIssuer: %w", err)
	}
	return v, nil
}
func (s *Service) GetGlobalIssuerDuplicatePreflight(ctx context.Context, payload *gen.GetGlobalIssuerDuplicatePreflightPayload) (*types.RemoteSessionIssuerDuplicatePreflight, error) {
	a, ok := contextvalues.GetAdminAuthContext(ctx)
	if !ok || a == nil || a.SessionID == "" || a.OIDCSubject == "" {
		err := oops.C(oops.CodeUnauthorized)
		return nil, err
	}
	if s.remoteSessions == nil {
		err := oops.C(oops.CodeUnavailable)
		return nil, err
	}
	v, err := s.remoteSessions.GetGlobalIssuerDuplicatePreflight(ctx, &adminrsgen.GetGlobalIssuerDuplicatePreflightPayload{SessionToken: nil, Issuer: payload.Issuer})
	if err != nil {
		return nil, fmt.Errorf("admin GetGlobalIssuerDuplicatePreflight: %w", err)
	}
	return v, nil
}
func (s *Service) ListGlobalIssuers(ctx context.Context, payload *gen.ListGlobalIssuersPayload) (*gen.ListGlobalRemoteSessionIssuersResult, error) {
	a, ok := contextvalues.GetAdminAuthContext(ctx)
	if !ok || a == nil || a.SessionID == "" || a.OIDCSubject == "" {
		err := oops.C(oops.CodeUnauthorized)
		return nil, err
	}
	if s.remoteSessions == nil {
		err := oops.C(oops.CodeUnavailable)
		return nil, err
	}
	v, err := s.remoteSessions.ListGlobalIssuers(ctx, &adminrsgen.ListGlobalIssuersPayload{SessionToken: nil, Cursor: payload.Cursor, Limit: payload.Limit})
	if err != nil {
		return nil, fmt.Errorf("admin ListGlobalIssuers: %w", err)
	}
	return convertListGlobalRemoteSessionIssuersResult(v), nil
}
func (s *Service) GetGlobalIssuer(ctx context.Context, payload *gen.GetGlobalIssuerPayload) (*gen.GlobalRemoteSessionIssuer, error) {
	a, ok := contextvalues.GetAdminAuthContext(ctx)
	if !ok || a == nil || a.SessionID == "" || a.OIDCSubject == "" {
		err := oops.C(oops.CodeUnauthorized)
		return nil, err
	}
	if s.remoteSessions == nil {
		err := oops.C(oops.CodeUnavailable)
		return nil, err
	}
	v, err := s.remoteSessions.GetGlobalIssuer(ctx, &adminrsgen.GetGlobalIssuerPayload{SessionToken: nil, ID: payload.ID})
	if err != nil {
		return nil, fmt.Errorf("admin GetGlobalIssuer: %w", err)
	}
	return convertGlobalRemoteSessionIssuer(v), nil
}
func (s *Service) UpdateGlobalIssuer(ctx context.Context, payload *gen.UpdateGlobalIssuerPayload) (*types.RemoteSessionIssuer, error) {
	a, ok := contextvalues.GetAdminAuthContext(ctx)
	if !ok || a == nil || a.SessionID == "" || a.OIDCSubject == "" {
		err := oops.C(oops.CodeUnauthorized)
		return nil, err
	}
	if s.remoteSessions == nil {
		err := oops.C(oops.CodeUnavailable)
		return nil, err
	}
	v, err := s.remoteSessions.UpdateGlobalIssuer(ctx, &adminrsgen.UpdateGlobalIssuerPayload{SessionToken: nil, ID: payload.ID, Slug: payload.Slug, Issuer: payload.Issuer, Name: payload.Name, LogoAssetID: payload.LogoAssetID, ClientSetupDocumentationURL: payload.ClientSetupDocumentationURL, AuthorizationEndpoint: payload.AuthorizationEndpoint, TokenEndpoint: payload.TokenEndpoint, RevocationEndpoint: payload.RevocationEndpoint, RegistrationEndpoint: payload.RegistrationEndpoint, JwksURI: payload.JwksURI, ServiceDocumentation: payload.ServiceDocumentation, OpPolicyURI: payload.OpPolicyURI, OpTosURI: payload.OpTosURI, ScopesSupported: payload.ScopesSupported, GrantTypesSupported: payload.GrantTypesSupported, ResponseTypesSupported: payload.ResponseTypesSupported, TokenEndpointAuthMethodsSupported: payload.TokenEndpointAuthMethodsSupported, CodeChallengeMethodsSupported: payload.CodeChallengeMethodsSupported, Oidc: payload.Oidc, Passthrough: payload.Passthrough, ClientIDMetadataDocumentSupported: payload.ClientIDMetadataDocumentSupported, UserinfoEndpoint: payload.UserinfoEndpoint, IntrospectionEndpoint: payload.IntrospectionEndpoint, IntrospectionEndpointAuthMethodsSupported: payload.IntrospectionEndpointAuthMethodsSupported, IDTokenSigningAlgValuesSupported: payload.IDTokenSigningAlgValuesSupported, ClaimsSupported: payload.ClaimsSupported, BackchannelLogoutSupported: payload.BackchannelLogoutSupported, AuthorizationResponseIssParameterSupported: payload.AuthorizationResponseIssParameterSupported, ScopeOverride: payload.ScopeOverride, ResourceIndicatorSupported: payload.ResourceIndicatorSupported})
	if err != nil {
		return nil, fmt.Errorf("admin UpdateGlobalIssuer: %w", err)
	}
	return v, nil
}
func (s *Service) DeleteGlobalIssuer(ctx context.Context, payload *gen.DeleteGlobalIssuerPayload) error {
	a, ok := contextvalues.GetAdminAuthContext(ctx)
	if !ok || a == nil || a.SessionID == "" || a.OIDCSubject == "" {
		err := oops.C(oops.CodeUnauthorized)
		return err
	}
	if s.remoteSessions == nil {
		err := oops.C(oops.CodeUnavailable)
		return err
	}
	if err := s.remoteSessions.DeleteGlobalIssuer(ctx, &adminrsgen.DeleteGlobalIssuerPayload{SessionToken: nil, ID: payload.ID}); err != nil {
		return fmt.Errorf("admin DeleteGlobalIssuer: %w", err)
	}
	return nil
}
func (s *Service) FetchGlobalIssuerMetadata(ctx context.Context, payload *gen.FetchGlobalIssuerMetadataPayload) (*types.RemoteSessionIssuerDraft, error) {
	a, ok := contextvalues.GetAdminAuthContext(ctx)
	if !ok || a == nil || a.SessionID == "" || a.OIDCSubject == "" {
		err := oops.C(oops.CodeUnauthorized)
		return nil, err
	}
	if s.remoteSessions == nil {
		err := oops.C(oops.CodeUnavailable)
		return nil, err
	}
	v, err := s.remoteSessions.FetchGlobalIssuerMetadata(ctx, &adminrsgen.FetchGlobalIssuerMetadataPayload{SessionToken: nil, Issuer: payload.Issuer})
	if err != nil {
		return nil, fmt.Errorf("admin FetchGlobalIssuerMetadata: %w", err)
	}
	return v, nil
}
func (s *Service) RefreshGlobalIssuerMetadata(ctx context.Context, payload *gen.RefreshGlobalIssuerMetadataPayload) (*types.RemoteSessionIssuerRefresh, error) {
	a, ok := contextvalues.GetAdminAuthContext(ctx)
	if !ok || a == nil || a.SessionID == "" || a.OIDCSubject == "" {
		err := oops.C(oops.CodeUnauthorized)
		return nil, err
	}
	if s.remoteSessions == nil {
		err := oops.C(oops.CodeUnavailable)
		return nil, err
	}
	v, err := s.remoteSessions.RefreshGlobalIssuerMetadata(ctx, &adminrsgen.RefreshGlobalIssuerMetadataPayload{SessionToken: nil, ID: payload.ID})
	if err != nil {
		return nil, fmt.Errorf("admin RefreshGlobalIssuerMetadata: %w", err)
	}
	return v, nil
}
func (s *Service) ListGlobalIssuerConvergenceCandidates(ctx context.Context, payload *gen.ListGlobalIssuerConvergenceCandidatesPayload) (*gen.ListIssuerConvergenceCandidatesResult, error) {
	a, ok := contextvalues.GetAdminAuthContext(ctx)
	if !ok || a == nil || a.SessionID == "" || a.OIDCSubject == "" {
		err := oops.C(oops.CodeUnauthorized)
		return nil, err
	}
	if s.remoteSessions == nil {
		err := oops.C(oops.CodeUnavailable)
		return nil, err
	}
	v, err := s.remoteSessions.ListGlobalIssuerConvergenceCandidates(ctx, &adminrsgen.ListGlobalIssuerConvergenceCandidatesPayload{SessionToken: nil, TargetID: payload.TargetID, Cursor: payload.Cursor, Limit: payload.Limit})
	if err != nil {
		return nil, fmt.Errorf("admin ListGlobalIssuerConvergenceCandidates: %w", err)
	}
	return convertListIssuerConvergenceCandidatesResult(v), nil
}
func (s *Service) GetGlobalIssuerMigratePreflight(ctx context.Context, payload *gen.GetGlobalIssuerMigratePreflightPayload) (*gen.IssuerMigratePreflight, error) {
	a, ok := contextvalues.GetAdminAuthContext(ctx)
	if !ok || a == nil || a.SessionID == "" || a.OIDCSubject == "" {
		err := oops.C(oops.CodeUnauthorized)
		return nil, err
	}
	if s.remoteSessions == nil {
		err := oops.C(oops.CodeUnavailable)
		return nil, err
	}
	v, err := s.remoteSessions.GetGlobalIssuerMigratePreflight(ctx, &adminrsgen.GetGlobalIssuerMigratePreflightPayload{SessionToken: nil, SourceID: payload.SourceID, TargetID: payload.TargetID})
	if err != nil {
		return nil, fmt.Errorf("admin GetGlobalIssuerMigratePreflight: %w", err)
	}
	return convertIssuerMigratePreflight(v), nil
}
func (s *Service) MigrateToGlobalIssuer(ctx context.Context, payload *gen.MigrateToGlobalIssuerPayload) (*gen.MigrateRemoteSessionIssuerResult, error) {
	a, ok := contextvalues.GetAdminAuthContext(ctx)
	if !ok || a == nil || a.SessionID == "" || a.OIDCSubject == "" {
		err := oops.C(oops.CodeUnauthorized)
		return nil, err
	}
	if s.remoteSessions == nil {
		err := oops.C(oops.CodeUnavailable)
		return nil, err
	}
	v, err := s.remoteSessions.MigrateToGlobalIssuer(ctx, &adminrsgen.MigrateToGlobalIssuerPayload{SessionToken: nil, SourceID: payload.SourceID, TargetID: payload.TargetID})
	if err != nil {
		return nil, fmt.Errorf("admin MigrateToGlobalIssuer: %w", err)
	}
	return convertMigrateRemoteSessionIssuerResult(v), nil
}
func convertGlobalRemoteSessionIssuer(v *adminrsgen.GlobalRemoteSessionIssuer) *gen.GlobalRemoteSessionIssuer {
	if v == nil {
		return nil
	}
	out := &gen.GlobalRemoteSessionIssuer{Issuer: v.Issuer, GlobalClientCount: v.GlobalClientCount, TenantClientCount: v.TenantClientCount}
	return out
}

func convertListGlobalRemoteSessionIssuersResult(v *adminrsgen.ListGlobalRemoteSessionIssuersResult) *gen.ListGlobalRemoteSessionIssuersResult {
	if v == nil {
		return nil
	}
	out := &gen.ListGlobalRemoteSessionIssuersResult{Items: make([]*gen.GlobalRemoteSessionIssuer, len(v.Items)), NextCursor: v.NextCursor}
	for i, item := range v.Items {
		out.Items[i] = convertGlobalRemoteSessionIssuer(item)
	}
	return out
}

func convertIssuerConvergenceCandidate(v *adminrsgen.IssuerConvergenceCandidate) *gen.IssuerConvergenceCandidate {
	if v == nil {
		return nil
	}
	out := &gen.IssuerConvergenceCandidate{Issuer: v.Issuer, OrganizationID: v.OrganizationID, OrganizationName: v.OrganizationName, ClientCount: v.ClientCount, EndpointMismatches: v.EndpointMismatches, Warnings: v.Warnings}
	return out
}

func convertListIssuerConvergenceCandidatesResult(v *adminrsgen.ListIssuerConvergenceCandidatesResult) *gen.ListIssuerConvergenceCandidatesResult {
	if v == nil {
		return nil
	}
	out := &gen.ListIssuerConvergenceCandidatesResult{Items: make([]*gen.IssuerConvergenceCandidate, len(v.Items)), NextCursor: v.NextCursor}
	for i, item := range v.Items {
		out.Items[i] = convertIssuerConvergenceCandidate(item)
	}
	return out
}

func convertIssuerMigratePreflight(v *adminrsgen.IssuerMigratePreflight) *gen.IssuerMigratePreflight {
	if v == nil {
		return nil
	}
	out := &gen.IssuerMigratePreflight{ClientCount: v.ClientCount, McpServerNames: v.McpServerNames, EndpointMismatches: v.EndpointMismatches, ConflictingMcpServerNames: v.ConflictingMcpServerNames, Warnings: v.Warnings, CanMigrate: v.CanMigrate, TargetTenantClientCount: v.TargetTenantClientCount}
	return out
}

func convertMigrateRemoteSessionIssuerResult(v *adminrsgen.MigrateRemoteSessionIssuerResult) *gen.MigrateRemoteSessionIssuerResult {
	if v == nil {
		return nil
	}
	out := &gen.MigrateRemoteSessionIssuerResult{Issuer: v.Issuer, ClientsMigrated: v.ClientsMigrated, SourceDeleted: v.SourceDeleted}
	return out
}
