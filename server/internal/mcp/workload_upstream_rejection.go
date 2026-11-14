package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"unicode"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcp/tunnelrouting"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// maxUpstreamReasonRunes bounds the upstream's error_description quoted back
// to a workload. Provider descriptions are a short sentence; the bound keeps an
// unexpectedly long or hostile one from filling the error.
const maxUpstreamReasonRunes = 200

// maxUpstreamReasonBodyBytes bounds how much of a rejecting upstream's body is
// read for its reason. Provider error bodies are a short JSON object or line of
// text well under this; reading more would only buffer what is discarded.
const maxUpstreamReasonBodyBytes = 1 << 10 // 1 KiB

// rejectWorkloadUpstreamCredentialRejection answers an upstream that rejects
// the credential a workload session borrows from its owner, instead of
// relaying the 401 or 403. A relayed rejection carries this server's
// challenge, which tells the workload its Speakeasy token is bad: the workload
// exchanges a new one and retries into the same rejection. The answer names
// the server and the upstream's reason, and carries no credential material.
// It does nothing for other callers, for the server's own credential, or for a
// tunnel gateway's own refusal.
func rejectWorkloadUpstreamCredentialRejection(ctx context.Context, p *proxy.Proxy, logger *slog.Logger, upstream remotesessions.UpstreamToken, serverID, serverName string) {
	caller := resolvedCaller(ctx)
	if caller == nil || caller.Kind != urn.SessionSubjectKindWorkload {
		return
	}
	if upstream.CredentialOwner == remotesessions.CredentialOwnerSelf || upstream.Token == "" {
		return
	}

	intercept := p.UpstreamResponseInterceptor
	p.UpstreamResponseInterceptor = func(ctx context.Context, resp *http.Response) error {
		if intercept != nil {
			if err := intercept(ctx, resp); err != nil {
				return err
			}
		}

		challenges := resp.Header.Values("WWW-Authenticate")
		if !remotemcp.CredentialRejected(resp.StatusCode, challenges) || resp.Header.Get(tunnelrouting.ErrorHeader) != "" {
			return nil
		}

		code, description := remotemcp.ChallengeError(challenges)
		if strings.TrimSpace(description) == "" {
			description = upstreamBodyReason(resp)
		}
		logger.WarnContext(ctx, "upstream rejected the credential behind a workload session",
			attr.SlogMcpServerID(serverID),
			attr.SlogHTTPResponseStatusCode(resp.StatusCode),
			attr.SlogOAuthError(redactCredential(code, upstream.Token)),
			attr.SlogOAuthErrorDescription(redactCredential(description, upstream.Token)),
		)

		return oops.E(oops.CodeFailedPrecondition,
			fmt.Errorf("workload owner's credential refused with status %d: %w", resp.StatusCode, remotemcp.ErrUpstreamCredentialRejected),
			"%s", workloadUpstreamRejectionMessage(serverName, resp.StatusCode, redactCredential(code, upstream.Token), redactCredential(description, upstream.Token)))
	}
}

// upstreamErrorBody is the reason fields a provider's JSON error body may
// carry, in the order a reason is taken from them. Each is raw because a
// provider may send an object where another sends a string.
type upstreamErrorBody struct {
	// ErrorDescription is the OAuth-style human-readable reason.
	ErrorDescription json.RawMessage `json:"error_description"`

	// Message is the reason most REST APIs send.
	Message json.RawMessage `json:"message"`

	// Error is an error code or, for some providers, the reason itself.
	Error json.RawMessage `json:"error"`
}

// upstreamBodyReason reads a short reason from the start of resp's body: the
// first non-empty string of error_description, message, then error from a
// JSON body, otherwise the first line of text. An event stream is not read,
// since its first bytes are a frame, not a reason. Consuming the body is safe
// because the rejection replaces the relay.
func upstreamBodyReason(resp *http.Response) string {
	if resp.Body == nil {
		return ""
	}
	if mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type")); err == nil && mediaType == "text/event-stream" {
		return ""
	}

	head, err := io.ReadAll(io.LimitReader(resp.Body, maxUpstreamReasonBodyBytes))
	if err != nil && len(head) == 0 {
		return ""
	}
	head = bytes.TrimSpace(head)

	var body upstreamErrorBody
	if json.Unmarshal(head, &body) == nil {
		for _, field := range []json.RawMessage{body.ErrorDescription, body.Message, body.Error} {
			var reason string
			if json.Unmarshal(field, &reason) == nil && strings.TrimSpace(reason) != "" {
				return reason
			}
		}
		return ""
	}
	if bytes.HasPrefix(head, []byte("{")) {
		// JSON cut short by the read bound has no reason to take whole.
		return ""
	}

	line, _, _ := bufio.NewReader(bytes.NewReader(head)).ReadLine()
	return string(line)
}

// redactCredential removes credential from an upstream-supplied reason, so an
// upstream that echoes the bearer it was sent cannot hand it to the workload.
func redactCredential(reason, credential string) string {
	if credential == "" {
		return reason
	}
	return strings.ReplaceAll(reason, credential, "[redacted]")
}

// mcpServerDisplayName names server the way its owner sees it: its name, else
// its slug, else its id.
func mcpServerDisplayName(server *mcpserversrepo.McpServer) string {
	switch {
	case server.Name.Valid && server.Name.String != "":
		return server.Name.String
	case server.Slug.Valid && server.Slug.String != "":
		return server.Slug.String
	default:
		return server.ID.String()
	}
}

// workloadUpstreamRejectionMessage is what a workload reads when the upstream
// rejects the credential its owner connected for serverName.
func workloadUpstreamRejectionMessage(serverName string, status int, code, description string) string {
	reason := fmt.Sprintf("HTTP %d", status)
	if code = sanitizeUpstreamReason(code); code != "" {
		reason += ", " + code
	}
	if description = sanitizeUpstreamReason(description); description != "" {
		reason += ": " + description
	}

	return fmt.Sprintf("The upstream rejected the credential this workload uses for MCP server %q (%s). "+
		"The workload's Speakeasy token is still valid, so exchanging a new one will not help. "+
		"The credential's owner must reconnect %q in Speakeasy.", serverName, reason, serverName)
}

// sanitizeUpstreamReason drops control characters from an upstream-supplied
// reason and truncates it to maxUpstreamReasonRunes.
func sanitizeUpstreamReason(reason string) string {
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.TrimSpace(reason))

	if runes := []rune(cleaned); len(runes) > maxUpstreamReasonRunes {
		return string(runes[:maxUpstreamReasonRunes]) + "…"
	}
	return cleaned
}
