// Proxy handling for the upstream credential a self remote session client
// holds for itself: one retry with a replacement when the upstream rejects
// it, and an answer naming the administrator when the rejection survives.

package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// clientCredentialRenewal records, for one proxied request, why replacing a
// rejected self client credential failed, so the final answer names that
// remedy.
type clientCredentialRenewal struct {
	// err is the classified replacement failure; nil when none was attempted
	// or it succeeded.
	err error
}

// preserveFailure carries a failed renewal through the member transport. A
// surviving 401 alone cannot distinguish a token endpoint outage from a
// credential that an administrator must repair.
func (r *clientCredentialRenewal) preserveFailure(p *proxy.Proxy) {
	if r == nil {
		return
	}

	intercept := p.UpstreamResponseInterceptor
	p.UpstreamResponseInterceptor = func(ctx context.Context, resp *http.Response) error {
		if intercept != nil {
			if err := intercept(ctx, resp); err != nil {
				return err
			}
		}
		if r.err != nil && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) {
			return fmt.Errorf("renew member client credential: %w", r.err)
		}
		return nil
	}
}

// renewClientCredentialOnRejection lets p, which presents upstream as its
// bearer, resend a request once with a replacement credential when the
// upstream answers 401. The upstream refused the request before acting on
// it, so a replay is safe for every method. It runs after any retryer p
// already has, such as a tunnel's route retry, and does nothing for a
// subject's credential, returning nil.
func (s *Service) renewClientCredentialOnRejection(p *proxy.Proxy, logger *slog.Logger, upstream remotesessions.UpstreamToken) *clientCredentialRenewal {
	if upstream.CredentialOwner != remotesessions.CredentialOwnerSelf || upstream.Token == "" {
		return nil
	}

	renewal := &clientCredentialRenewal{err: nil}

	renew := func(ctx context.Context, resp *http.Response) (*proxy.UpstreamResponseRetry, error) {
		if resp.StatusCode != http.StatusUnauthorized {
			return nil, nil
		}

		renewed, ok, err := s.remoteChallengeMgr.RenewClientCredential(ctx, upstream)
		switch {
		case err != nil:
			// The rejection stands, answered with the replacement's remedy.
			renewal.err = err
			logger.WarnContext(ctx, "replace upstream client credential after rejection",
				attr.SlogRemoteSessionClientID(upstream.RemoteSessionClientID.String()),
				attr.SlogError(err),
			)
			return nil, nil
		case !ok:
			return nil, nil
		}

		logger.InfoContext(ctx, "upstream rejected the client credential; retrying once with a replacement",
			attr.SlogRemoteSessionClientID(upstream.RemoteSessionClientID.String()),
		)

		return &proxy.UpstreamResponseRetry{RemoteURL: "", Headers: nil, AuthorizationOverride: renewed.Token}, nil
	}

	p.UpstreamResponseRetryer = proxy.ChainUpstreamResponseRetryers(p.UpstreamResponseRetryer, renew)

	return renewal
}

// rejectSurvivingClientCredentialRejection answers an upstream 401 or 403 to
// p's self client credential, after any replacement, instead of relaying it.
// A relayed rejection would carry a challenge for the caller to reauthorize,
// and nothing the caller authorizes repairs a credential the MCP server holds
// for itself. The answer names the administrator, or asks for a retry when
// renewal shows the replacement failed for a reason that clears on its own.
// It does nothing for a subject's credential.
func rejectSurvivingClientCredentialRejection(w http.ResponseWriter, p *proxy.Proxy, logger *slog.Logger, upstream remotesessions.UpstreamToken, renewal *clientCredentialRenewal) {
	if upstream.CredentialOwner != remotesessions.CredentialOwnerSelf || upstream.Token == "" {
		return
	}

	intercept := p.UpstreamResponseInterceptor
	p.UpstreamResponseInterceptor = func(ctx context.Context, resp *http.Response) error {
		if intercept != nil {
			if err := intercept(ctx, resp); err != nil {
				return err
			}
		}

		if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
			return nil
		}

		logger.WarnContext(ctx, "upstream rejected the MCP server's client credential",
			attr.SlogRemoteSessionClientID(upstream.RemoteSessionClientID.String()),
			attr.SlogHTTPResponseStatusCode(resp.StatusCode),
		)

		if renewal != nil && renewal.err != nil {
			return clientCredentialRoutingError(w, renewal.err)
		}
		return clientCredentialMisconfiguredError(fmt.Errorf("upstream rejected the client credential with status %d", resp.StatusCode))
	}
}

// clientCredentialRoutingError answers routing that selected a self client
// whose credential could not be obtained, with the remedy its classification
// names: retry an outage, or contact the administrator.
func clientCredentialRoutingError(w http.ResponseWriter, err error) error {
	if errors.Is(err, remotesessions.ErrRemoteSessionUnavailable) {
		return remoteSessionUnavailableError(w, err)
	}

	return clientCredentialMisconfiguredError(err)
}

// metaMemberClientCredentialError is the member-scoped answer for a gateway
// member whose self client credential could not be obtained.
func metaMemberClientCredentialError(member metaMember, err error) error {
	if errors.Is(err, remotesessions.ErrRemoteSessionUnavailable) {
		return &metaMemberError{message: fmt.Sprintf("server %q cannot reach its upstream authorization server right now; retry shortly", member.slug)}
	}

	return &metaMemberError{message: fmt.Sprintf("server %q has an upstream credential that is misconfigured or was rejected; contact the MCP server administrator", member.slug)}
}

// subjectConnectedClients returns the clients that do not hold their upstream
// credential for themselves, leaving clients untouched. The consent and
// connect flows show, connect, verify and refresh a subject's own
// connections, and nobody connects a self client: its credential is presented
// for every caller without one. A self client still claims its upstream, so
// resource derivation weighs every bound client.
func subjectConnectedClients(clients []remotesessions.Client) []remotesessions.Client {
	return slices.DeleteFunc(slices.Clone(clients), func(c remotesessions.Client) bool {
		return c.CredentialOwner == remotesessions.CredentialOwnerSelf
	})
}
