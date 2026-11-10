package orgencryption

import (
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
)

// UseCase names one consumer of organization encryption. Each use case gets
// its own DEK per organization, so rotating or compromising one consumer's key
// leaves the others alone.
type UseCase string

const (
	// UseCaseTailscaleGatewayState is the use case for Tailscale gateway state.
	UseCaseTailscaleGatewayState UseCase = "tailscale-gateway-state"

	// UseCaseIntegrationCredentials is the use case for credentials Speakeasy
	// holds for third-party integrations: Slack workspaces, device management
	// and compliance providers, and network ingress providers.
	UseCaseIntegrationCredentials UseCase = "integration-credentials"
)

// registeredUseCases lists every use case the package accepts. Add a consumer
// here before calling any entry point with its name.
var registeredUseCases = []UseCase{
	UseCaseTailscaleGatewayState,
	UseCaseIntegrationCredentials,
}

// ParseUseCase returns the registered use case named value.
func ParseUseCase(value string) (UseCase, error) {
	useCase := UseCase(value)
	if err := useCase.validate(); err != nil {
		return "", err
	}

	return useCase, nil
}

func (u UseCase) validate() error {
	if !slices.Contains(registeredUseCases, u) {
		return fmt.Errorf("%w: %q", ErrUnknownUseCase, string(u))
	}

	return nil
}

// associatedDataSeparator joins associated data fields. NUL cannot appear in
// an organization id, a registered use case or a column name.
const associatedDataSeparator = "\x00"

// valueAssociatedData is the associated data a value is sealed with. It binds
// the organization, the use case and the table column the ciphertext is
// stored in.
func valueAssociatedData(organizationID string, useCase UseCase, column string) []byte {
	return []byte(strings.Join([]string{"orgencryption/value/v1", organizationID, string(useCase), column}, associatedDataSeparator))
}

// wrapAssociatedData is the associated data a DEK is wrapped with in KMS. It
// does not name the KEK, so a switch re-wraps under the same associated data.
func wrapAssociatedData(organizationID string, dataKeyID uuid.UUID, useCase string) []byte {
	return []byte(strings.Join([]string{"orgencryption/dek/v1", organizationID, dataKeyID.String(), useCase}, associatedDataSeparator))
}
