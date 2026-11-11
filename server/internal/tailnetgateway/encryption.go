package tailnetgateway

import (
	"context"
	"errors"

	"github.com/speakeasy-api/gram/server/internal/orgencryption"
)

// EncryptionUseCase is the organization encryption use case every gateway
// secret is sealed under: node state and one-shot auth keys. Each value is
// also bound to its column, so one data key per organization serves both.
const EncryptionUseCase = orgencryption.UseCaseTailscaleGatewayState

// Columns the gateway's ciphertexts are bound to.
const (
	nodeStateColumn = "network_ingress_node_states.value_encrypted"
	authKeyColumn   = "network_ingress_nodes.auth_key_encrypted"
)

// KeyProvisioner makes sure an organization has a data key for a use case.
// *orgencryption.Provisioner implements it.
type KeyProvisioner interface {
	EnsureUseCase(ctx context.Context, organizationID string, useCase orgencryption.UseCase, actor orgencryption.Actor) error
}

// encryptionKeyUnavailable reports whether err means the organization's key
// cannot be used right now: its encryption is suspended, or its key is
// unreachable or revoked. For a customer-managed key the customer fixes it.
func encryptionKeyUnavailable(err error) bool {
	return errors.Is(err, orgencryption.ErrSuspended) || errors.Is(err, orgencryption.ErrKeyUnavailable)
}
