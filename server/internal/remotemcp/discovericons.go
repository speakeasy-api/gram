package remotemcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	gen "github.com/speakeasy-api/gram/server/gen/remote_mcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// initializeIconsEnvelope captures just the serverInfo.icons slice of an MCP
// initialize response (SEP-973).
type initializeIconsEnvelope struct {
	Result struct {
		ServerInfo struct {
			Icons []struct {
				Src string `json:"src"`
			} `json:"icons"`
		} `json:"serverInfo"`
	} `json:"result"`
}

// DiscoverServerIcons probes the given remote MCP server URL with an
// initialize request and returns any icons the server advertises in its
// serverInfo. Probe failures of any kind — unreachable hosts, auth-required
// responses, invalid MCP replies — yield an empty list, not an error: icon
// discovery is best-effort by design.
func (s *Service) DiscoverServerIcons(ctx context.Context, payload *gen.DiscoverServerIconsPayload) (*gen.DiscoverServerIconsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	if err := validateURL(ctx, s.policy, payload.URL); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid url").LogError(ctx, logger)
	}

	probeCtx, cancel := context.WithTimeout(ctx, verifyURLTimeout)
	defer cancel()

	icons := DiscoverRemoteMcpIcons(probeCtx, s.policy, payload.URL)

	return &gen.DiscoverServerIconsResult{Icons: icons}, nil
}

// DiscoverRemoteMcpIcons issues the same canned initialize probe as
// [VerifyRemoteMcpURL] and parses serverInfo.icons out of the response.
// Relative icon sources are resolved against the server URL per SEP-973.
// Every failure mode returns an empty (never nil) slice.
func DiscoverRemoteMcpIcons(ctx context.Context, policy *guardian.Policy, rawURL string) []string {
	client := policy.Client()
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= verifyURLMaxRedirects {
			return fmt.Errorf("stopped after %d redirects", verifyURLMaxRedirects)
		}
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, strings.NewReader(verifyURLBody))
	if err != nil {
		return []string{}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.ContentLength = int64(len(verifyURLBody))

	resp, err := client.Do(req)
	if err != nil {
		return []string{}
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return []string{}
	}

	contentType := resp.Header.Get("Content-Type")
	mediaType := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))

	body := io.LimitReader(resp.Body, verifyURLMaxBodyBytes)

	var payload []byte
	switch mediaType {
	case "application/json":
		payload, err = io.ReadAll(body)
		if err != nil {
			return []string{}
		}
	case "text/event-stream":
		payload = firstSSEEventData(body)
	default:
		return []string{}
	}
	if len(payload) == 0 {
		return []string{}
	}

	var envelope initializeIconsEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return []string{}
	}

	base, err := url.Parse(rawURL)
	if err != nil {
		return []string{}
	}

	icons := make([]string, 0, len(envelope.Result.ServerInfo.Icons))
	for _, icon := range envelope.Result.ServerInfo.Icons {
		src := strings.TrimSpace(icon.Src)
		if src == "" {
			continue
		}
		ref, err := url.Parse(src)
		if err != nil {
			continue
		}
		resolved := base.ResolveReference(ref)
		// Only http(s) icon sources are usable by the ingestion path; this
		// also drops data: and javascript: URLs a hostile server could send.
		if resolved.Scheme != "http" && resolved.Scheme != "https" {
			continue
		}
		icons = append(icons, resolved.String())
	}
	return icons
}

// firstSSEEventData returns the concatenated data payload of the first SSE
// event in the stream, or nil if none is found.
func firstSSEEventData(r io.Reader) []byte {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), verifyURLMaxBodyBytes)

	// Scanner errors are deliberately ignored: a truncated or unreadable
	// stream yields empty/partial data, which the best-effort caller treats
	// the same as "no icons".
	var data []string
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if len(data) > 0 {
				break
			}
			continue
		}
		if value, ok := strings.CutPrefix(line, "data:"); ok {
			data = append(data, strings.TrimPrefix(value, " "))
		}
	}
	if len(data) == 0 {
		return nil
	}
	return []byte(strings.Join(data, "\n"))
}
