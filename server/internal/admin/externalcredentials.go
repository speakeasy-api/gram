package admin

import (
	"context"
	"fmt"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	adminecgen "github.com/speakeasy-api/gram/server/gen/admin_external_credentials"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/externalcredentials"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// SetExternalCredentialService supplies the platform-tier external credential
// handlers. Without it every platform credential method reports unavailable.
func (s *Service) SetExternalCredentialService(service *externalcredentials.Service) {
	s.externalCredentials = service
}

// requirePlatformCredentials rejects requests that reach a platform credential
// handler without the verified staff identity the admin transport installs, or
// on a server with no external credential service configured. The handlers
// behind it additionally require the staff email to belong to a live platform
// administrator (users.admin); admin app access alone is not enough.
func (s *Service) requirePlatformCredentials(ctx context.Context) error {
	a, ok := contextvalues.GetAdminAuthContext(ctx)
	if !ok || a == nil || a.SessionID == "" || a.OIDCSubject == "" {
		return oops.C(oops.CodeUnauthorized)
	}
	if s.externalCredentials == nil {
		return oops.C(oops.CodeUnavailable)
	}
	return nil
}

func (s *Service) ListPlatformExternalCredentials(ctx context.Context, payload *gen.ListPlatformExternalCredentialsPayload) (*gen.ListExternalCredentialsResult, error) {
	if err := s.requirePlatformCredentials(ctx); err != nil {
		return nil, err
	}

	result, err := s.externalCredentials.ListPlatformExternalCredentials(ctx, &adminecgen.ListPlatformExternalCredentialsPayload{SessionToken: nil, Provider: payload.Provider})
	if err != nil {
		return nil, fmt.Errorf("admin ListPlatformExternalCredentials: %w", err)
	}

	credentials := make([]*gen.ExternalCredentialSummary, 0, len(result.Credentials))
	for _, c := range result.Credentials {
		credentials = append(credentials, &gen.ExternalCredentialSummary{
			ID:             c.ID,
			OrganizationID: c.OrganizationID,
			Provider:       c.Provider,
			Name:           c.Name,
			CreatedAt:      c.CreatedAt,
			UpdatedAt:      c.UpdatedAt,
		})
	}
	return &gen.ListExternalCredentialsResult{Credentials: credentials}, nil
}

func (s *Service) GetGcpIamPlatformCredential(ctx context.Context, payload *gen.GetGcpIamPlatformCredentialPayload) (*gen.GcpIamCredential, error) {
	if err := s.requirePlatformCredentials(ctx); err != nil {
		return nil, err
	}

	result, err := s.externalCredentials.GetGcpIamPlatformCredential(ctx, &adminecgen.GetGcpIamPlatformCredentialPayload{SessionToken: nil, ID: payload.ID})
	if err != nil {
		return nil, fmt.Errorf("admin GetGcpIamPlatformCredential: %w", err)
	}
	return convertGcpIamPlatformCredential(result), nil
}

func (s *Service) CreateGcpIamPlatformCredential(ctx context.Context, payload *gen.CreateGcpIamPlatformCredentialPayload) (*gen.GcpIamCredential, error) {
	if err := s.requirePlatformCredentials(ctx); err != nil {
		return nil, err
	}

	result, err := s.externalCredentials.CreateGcpIamPlatformCredential(ctx, &adminecgen.CreateGcpIamPlatformCredentialPayload{
		SessionToken:              nil,
		Name:                      payload.Name,
		ImpersonateServiceAccount: payload.ImpersonateServiceAccount,
		WifPoolID:                 payload.WifPoolID,
		WifProviderID:             payload.WifProviderID,
		WifProjectNumber:          payload.WifProjectNumber,
	})
	if err != nil {
		return nil, fmt.Errorf("admin CreateGcpIamPlatformCredential: %w", err)
	}
	return convertGcpIamPlatformCredential(result), nil
}

func (s *Service) UpdateGcpIamPlatformCredential(ctx context.Context, payload *gen.UpdateGcpIamPlatformCredentialPayload) (*gen.GcpIamCredential, error) {
	if err := s.requirePlatformCredentials(ctx); err != nil {
		return nil, err
	}

	result, err := s.externalCredentials.UpdateGcpIamPlatformCredential(ctx, &adminecgen.UpdateGcpIamPlatformCredentialPayload{
		SessionToken:              nil,
		ID:                        payload.ID,
		Name:                      payload.Name,
		ImpersonateServiceAccount: payload.ImpersonateServiceAccount,
		WifPoolID:                 payload.WifPoolID,
		WifProviderID:             payload.WifProviderID,
		WifProjectNumber:          payload.WifProjectNumber,
	})
	if err != nil {
		return nil, fmt.Errorf("admin UpdateGcpIamPlatformCredential: %w", err)
	}
	return convertGcpIamPlatformCredential(result), nil
}

func (s *Service) VerifyGcpIamPlatformCredential(ctx context.Context, payload *gen.VerifyGcpIamPlatformCredentialPayload) (*gen.VerifyPlatformCredentialResult, error) {
	if err := s.requirePlatformCredentials(ctx); err != nil {
		return nil, err
	}

	result, err := s.externalCredentials.VerifyGcpIamPlatformCredential(ctx, &adminecgen.VerifyGcpIamPlatformCredentialPayload{SessionToken: nil, ID: payload.ID})
	if err != nil {
		return nil, fmt.Errorf("admin VerifyGcpIamPlatformCredential: %w", err)
	}
	return &gen.VerifyPlatformCredentialResult{
		Verified:       result.Verified,
		Principal:      result.Principal,
		IdentitySource: result.IdentitySource,
		Detail:         result.Detail,
	}, nil
}

func (s *Service) DeleteGcpIamPlatformCredential(ctx context.Context, payload *gen.DeleteGcpIamPlatformCredentialPayload) error {
	if err := s.requirePlatformCredentials(ctx); err != nil {
		return err
	}

	if err := s.externalCredentials.DeleteGcpIamPlatformCredential(ctx, &adminecgen.DeleteGcpIamPlatformCredentialPayload{SessionToken: nil, ID: payload.ID}); err != nil {
		return fmt.Errorf("admin DeleteGcpIamPlatformCredential: %w", err)
	}
	return nil
}

func convertGcpIamPlatformCredential(c *adminecgen.GcpIamCredential) *gen.GcpIamCredential {
	return &gen.GcpIamCredential{
		ID:                        c.ID,
		OrganizationID:            c.OrganizationID,
		Provider:                  c.Provider,
		Name:                      c.Name,
		CreatedAt:                 c.CreatedAt,
		UpdatedAt:                 c.UpdatedAt,
		ImpersonateServiceAccount: c.ImpersonateServiceAccount,
		WifPoolID:                 c.WifPoolID,
		WifProviderID:             c.WifProviderID,
		WifProjectNumber:          c.WifProjectNumber,
	}
}
