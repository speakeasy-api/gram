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

var (
	errDelegationReauthRequired     = errors.New("hooks delegation enrollment must be refreshed")
	errDelegationUnavailable        = errors.New("hooks delegation service unavailable")
	errProofBoundEnrollmentRequired = errors.New("proof-bound hooks enrollment is required")
)

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
	return governedBinding{provider: provider, event: event, sessionID: strings.TrimSpace(base.Session.ID), observational: observational}, true
}

func (r *Relay) mintActingUserAssertion(ctx context.Context, credentials creds, binding governedBinding, idempotencyKey string) (string, error) {
	return mintActingUserAssertion(ctx, r.cfg.ServerURL, credentials, binding, idempotencyKey)
}

func mintActingUserAssertion(ctx context.Context, serverURL string, credentials creds, binding governedBinding, idempotencyKey string) (string, error) {
	assertion, _, err := mintActingUserAssertionWithExpiry(ctx, serverURL, credentials, binding, idempotencyKey)
	return assertion, err
}

func mintActingUserAssertionWithExpiry(ctx context.Context, serverURL string, credentials creds, binding governedBinding, idempotencyKey string) (string, time.Duration, error) {
	if credentials.Source != credCache || credentials.ContractVersion != delegation.ContractVersion || credentials.RefreshToken == "" || credentials.ProofPrivateKey == "" {
		return "", 0, errProofBoundEnrollmentRequired
	}
	privateKey, err := delegation.ParsePrivateKey(credentials.ProofPrivateKey)
	if err != nil {
		return "", 0, fmt.Errorf("%w: %w", errProofBoundEnrollmentRequired, err)
	}
	nonce, err := delegation.NewNonce()
	if err != nil {
		return "", 0, fmt.Errorf("%w: create nonce: %w", errDelegationUnavailable, err)
	}
	request := delegation.MintRequest{
		RefreshToken: credentials.RefreshToken, ContractVersion: delegation.ContractVersion,
		Provider: binding.provider, Event: binding.event, SessionID: binding.sessionID,
		IdempotencyKey: idempotencyKey, Observational: binding.observational, SignedAt: time.Now().Unix(),
		Nonce: nonce,
	}
	request.Signature, err = delegation.Sign(privateKey, request)
	if err != nil {
		return "", 0, fmt.Errorf("%w: sign request: %w", errDelegationUnavailable, err)
	}
	body, err := json.Marshal(request)
	if err != nil {
		return "", 0, fmt.Errorf("%w: encode request: %w", errDelegationUnavailable, err)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(serverURL, "/")+"/rpc/cliAuth.delegateHooksActingUser", bytes.NewReader(body))
	if err != nil {
		return "", 0, fmt.Errorf("%w: create request: %w", errDelegationUnavailable, err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	response, err := newRelayHTTPClient(perAttemptTime).Do(httpRequest)
	if err != nil {
		return "", 0, fmt.Errorf("%w: mint acting-user assertion: %w", errDelegationUnavailable, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
			return "", 0, fmt.Errorf("%w: HTTP %d", errDelegationReauthRequired, response.StatusCode)
		}
		if response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= http.StatusInternalServerError {
			return "", 0, fmt.Errorf("%w: mint acting-user assertion: HTTP %d", errDelegationUnavailable, response.StatusCode)
		}
		return "", 0, fmt.Errorf("%w: HTTP %d", errProofBoundEnrollmentRequired, response.StatusCode)
	}
	var result delegation.MintResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&result); err != nil {
		return "", 0, fmt.Errorf("%w: decode acting-user assertion: %w", errDelegationUnavailable, err)
	}
	if strings.TrimSpace(result.Assertion) == "" || result.ExpiresIn <= 0 {
		return "", 0, fmt.Errorf("%w: invalid mint response", errDelegationUnavailable)
	}
	return result.Assertion, time.Duration(result.ExpiresIn) * time.Second, nil
}
