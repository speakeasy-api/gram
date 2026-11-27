package assistanttokens

import (
	"context"
	"errors"
	"fmt"

	redisCache "github.com/go-redis/cache/v9"

	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func delegationBindingKey(delegationID string) string {
	return "assistant-runtime-delegation:" + delegationID
}

// DelegateTurnCredential turns a turn's agent credential into a delegation
// that only actor, the runtime's workload identity, can exchange. The runtime
// never holds a credential that authenticates on its own. It returns "" for a
// turn with no person behind it, including a thread-scoped runtime token that
// is not a principal credential: the workload then acts with its own policy.
func (m *Manager) DelegateTurnCredential(ctx context.Context, turnCredential string, actor urn.Principal) (string, error) {
	authenticated, err := m.credentials.Validate(turnCredential)
	if errors.Is(err, principalcredential.ErrNotCredential) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("validate turn credential: %w", err)
	}
	if authenticated.Credential.Principal.Type != urn.PrincipalTypeAgent {
		return "", nil
	}
	var binding RuntimeBinding
	if err := m.runtimeBindings.Get(ctx, runtimeBindingKey(authenticated.ID), &binding); err != nil {
		return "", fmt.Errorf("load turn credential binding: %w", err)
	}
	raw, id, err := m.credentials.MintDelegation(authenticated.Credential, actor)
	if err != nil {
		return "", fmt.Errorf("mint turn delegation: %w", err)
	}
	if err := m.runtimeBindings.Set(ctx, delegationBindingKey(id), binding, principalcredential.Lifetime); err != nil {
		return "", fmt.Errorf("record turn delegation binding: %w", err)
	}
	return raw, nil
}

// ExchangeDelegation returns a fresh turn credential for a delegation that
// names actor, bound to the same assistant thread. Admission of the returned
// credential is unchanged: the agent, its owner and the user all bound it.
func (m *Manager) ExchangeDelegation(ctx context.Context, delegation string, actor urn.Principal) (string, error) {
	authenticated, mayAct, err := m.credentials.ValidateDelegation(delegation)
	if err != nil {
		return "", fmt.Errorf("validate delegation: %w", err)
	}
	if mayAct.String() != actor.String() {
		return "", fmt.Errorf("%w: delegation names another actor", principalcredential.ErrInvalid)
	}
	var binding RuntimeBinding
	err = m.runtimeBindings.Get(ctx, delegationBindingKey(authenticated.ID), &binding)
	switch {
	case errors.Is(err, redisCache.ErrCacheMiss):
		return "", fmt.Errorf("%w: delegation is not bound to an assistant thread", principalcredential.ErrInvalid)
	case err != nil:
		return "", fmt.Errorf("load delegation binding: %w", err)
	}
	return m.MintTurnCredential(ctx, authenticated.Credential, binding)
}
