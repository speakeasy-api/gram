package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/speakeasy-api/agenthooks"

	"github.com/speakeasy-api/gram/hooks/delegation"
)

const actingIdentityFailureMessage = delegation.IdentityFailureMessage

var errDelegationReauthRequired = errors.New("hooks delegation enrollment must be refreshed")

type governedBinding struct {
	provider      string
	event         string
	sessionID     string
	observational bool
}

// governedHookBindingOf names the explicitly approved native event shapes.
// Backfilled prompts carry a proof-bound observational marker so they remain
// outside ai_access without trusting a caller-controlled transport header.
func governedHookBindingOf(typed any) (governedBinding, bool) {
	base := agenthooks.EventOf(typed)
	provider := adapterSlug(base.Provider)
	var event string
	switch typed.(type) {
	case *agenthooks.PromptEvent:
		if base.NativeName != delegation.EventUserPromptSubmit {
			return governedBinding{}, false
		}
		event = delegation.EventUserPromptSubmit
	case *agenthooks.ToolPreEvent:
		if base.NativeName != delegation.EventPreToolUse {
			return governedBinding{}, false
		}
		event = delegation.EventPreToolUse
	default:
		return governedBinding{}, false
	}
	if !delegation.Approved(provider, event) {
		return governedBinding{}, false
	}
	observational := false
	if prompt, ok := typed.(*agenthooks.PromptEvent); ok {
		observational = prompt.Backfilled
	}
	return governedBinding{provider: provider, event: event, sessionID: base.Session.ID, observational: observational}, true
}

func (r *Relay) mintActingUserAssertion(ctx context.Context, credentials creds, binding governedBinding, idempotencyKey string) (string, error) {
	return mintActingUserAssertion(ctx, r.cfg.ServerURL, credentials, binding, idempotencyKey)
}

func mintActingUserAssertion(ctx context.Context, serverURL string, credentials creds, binding governedBinding, idempotencyKey string) (string, error) {
	if credentials.Source != credCache || credentials.ContractVersion != delegation.ContractVersion || credentials.RefreshToken == "" || credentials.ProofPrivateKey == "" {
		return "", errors.New("proof-bound hooks enrollment is required")
	}
	privateKey, err := delegation.ParsePrivateKey(credentials.ProofPrivateKey)
	if err != nil {
		return "", err
	}
	nonce, err := delegation.NewNonce()
	if err != nil {
		return "", err
	}
	request := delegation.MintRequest{
		RefreshToken: credentials.RefreshToken, ContractVersion: delegation.ContractVersion,
		Provider: binding.provider, Event: binding.event, SessionID: binding.sessionID,
		IdempotencyKey: idempotencyKey, Observational: binding.observational, SignedAt: time.Now().Unix(),
		Nonce: nonce,
	}
	request.Signature, err = delegation.Sign(privateKey, request)
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(request)
	if err != nil {
		return "", err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(serverURL, "/")+"/rpc/cliAuth.delegateHooksActingUser", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	response, err := newRelayHTTPClient(perAttemptTime).Do(httpRequest)
	if err != nil {
		return "", fmt.Errorf("mint acting-user assertion: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
			return "", fmt.Errorf("%w: HTTP %d", errDelegationReauthRequired, response.StatusCode)
		}
		return "", fmt.Errorf("mint acting-user assertion: HTTP %d", response.StatusCode)
	}
	var result delegation.MintResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&result); err != nil {
		return "", fmt.Errorf("decode acting-user assertion: %w", err)
	}
	if strings.TrimSpace(result.Assertion) == "" {
		return "", errors.New("mint acting-user assertion: empty assertion")
	}
	return result.Assertion, nil
}
