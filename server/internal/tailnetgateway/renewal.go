package tailnetgateway

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"tailscale.com/envknob"

	"github.com/speakeasy-api/gram/server/internal/guardian"
)

// The gateway, not tsnet, decides when a connection's certificate is renewed.
// Left to tsnet, each slot cached its own renewal time once per certificate,
// so every slot renewed once per cycle, outside the issuance lease, and none
// noticed Let's Encrypt asking for an early renewal, as it does ahead of a
// mass revocation. A slot holding the certificate instead asks Let's Encrypt
// for its renewal window (ACME Renewal Information, ARI) at the interval
// Let's Encrypt names, renews at one point in that window that every slot of
// the connection agrees on, and does so under the issuance lease after
// re-reading the store, so the connection renews once per cycle.
const (
	// letsEncryptDirectoryURL is Let's Encrypt's production ACME directory,
	// which tsnet issues from unless TS_DEBUG_ACME_DIRECTORY_URL overrides it.
	letsEncryptDirectoryURL = "https://acme-v02.api.letsencrypt.org/directory"

	// renewalCheckMin and renewalCheckMax bound how often a slot asks for a
	// certificate's renewal window, whatever Retry-After Let's Encrypt
	// sends. Let's Encrypt usually suggests about six hours; a window moved
	// earlier is seen within renewalCheckMax, while revocation notices give
	// days.
	renewalCheckMin = time.Hour
	renewalCheckMax = 6 * time.Hour

	// renewalFetchTimeout bounds one renewal window request.
	renewalFetchTimeout = 10 * time.Second

	// certificateRequestConcurrency bounds the certificate requests, window
	// requests and renewals a pod runs at once, so a pod starting hundreds
	// of connections does not send them all to Let's Encrypt and tsnet
	// together. A node whose turn has not come tries again at its next poll.
	certificateRequestConcurrency = 8
)

// renewalWindows says when a certificate should be renewed.
type renewalWindows interface {
	// RenewAt returns when to renew leaf and when to ask again.
	RenewAt(ctx context.Context, leaf *x509.Certificate, now time.Time) (renewAt, checkAgain time.Time, err error)
}

// ariClient reads renewal windows from an ACME server's renewalInfo resource.
// The resource needs no account, so one client serves every node in a pod.
type ariClient struct {
	directoryURL string
	client       *guardian.HTTPClient

	// mu guards renewalInfoURL, read from the directory on first use.
	mu             sync.Mutex
	renewalInfoURL string
}

var _ renewalWindows = (*ariClient)(nil)

// newARIClient reads windows from the directory tsnet issues from, through
// the default outbound policy.
func newARIClient() *ariClient {
	directoryURL := envknob.String("TS_DEBUG_ACME_DIRECTORY_URL")
	if directoryURL == "" {
		directoryURL = letsEncryptDirectoryURL
	}
	client := guardian.NewDefaultPolicy(otel.GetTracerProvider()).PooledClient()
	client.Timeout = renewalFetchTimeout
	return &ariClient{directoryURL: directoryURL, client: client, mu: sync.Mutex{}, renewalInfoURL: ""}
}

// renewalInfo is the ARI response: the window Let's Encrypt suggests renewing
// in, and optionally why it was moved.
type renewalInfo struct {
	SuggestedWindow struct {
		Start time.Time `json:"start"`
		End   time.Time `json:"end"`
	} `json:"suggestedWindow"`
	ExplanationURL string `json:"explanationURL"`
}

func (c *ariClient) RenewAt(ctx context.Context, leaf *x509.Certificate, now time.Time) (time.Time, time.Time, error) {
	base, err := c.renewalInfoBase(ctx)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	var info renewalInfo
	header, err := c.getJSON(ctx, strings.TrimSuffix(base, "/")+"/"+ariCertificateID(leaf), &info)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("fetch certificate renewal window: %w", err)
	}
	start, end := info.SuggestedWindow.Start, info.SuggestedWindow.End
	if start.IsZero() || end.Before(start) {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid certificate renewal window %s to %s", start, end)
	}
	return renewalPoint(start, end, leaf), now.Add(renewalCheckInterval(header.Get("Retry-After"), now)), nil
}

func (c *ariClient) renewalInfoBase(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.renewalInfoURL != "" {
		return c.renewalInfoURL, nil
	}
	var directory struct {
		RenewalInfo string `json:"renewalInfo"`
	}
	if _, err := c.getJSON(ctx, c.directoryURL, &directory); err != nil {
		return "", fmt.Errorf("fetch ACME directory: %w", err)
	}
	if directory.RenewalInfo == "" {
		return "", errors.New("ACME directory offers no renewal information")
	}
	c.renewalInfoURL = directory.RenewalInfo
	return c.renewalInfoURL, nil
}

func (c *ariClient) getJSON(ctx context.Context, url string, into any) (http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	res, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %s", res.Status)
	}
	if err := json.NewDecoder(res.Body).Decode(into); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return res.Header, nil
}

// ariCertificateID names a certificate to the renewalInfo resource: its
// authority key identifier and serial number (RFC 9773).
func ariCertificateID(leaf *x509.Certificate) string {
	return base64.RawURLEncoding.EncodeToString(leaf.AuthorityKeyId) + "." + base64.RawURLEncoding.EncodeToString(leaf.SerialNumber.Bytes())
}

// renewalPoint picks the moment in a window to renew leaf. Let's Encrypt asks
// clients to spread renewals across the window; deriving the point from the
// serial number does that while every slot of a connection, holding the same
// certificate, picks the same moment.
func renewalPoint(start, end time.Time, leaf *x509.Certificate) time.Time {
	width := end.Sub(start)
	if width <= 0 {
		return start
	}
	h := fnv.New64a()
	_, _ = h.Write(leaf.SerialNumber.Bytes())
	return start.Add(time.Duration(h.Sum64() % uint64(width))) //nolint:gosec // the remainder is below width, a positive int64
}

// renewalCheckInterval is how long to wait before asking for a window again:
// Retry-After, given in seconds or as a date, within renewalCheckMin and
// renewalCheckMax, and renewalCheckMax when absent.
func renewalCheckInterval(retryAfter string, now time.Time) time.Duration {
	wait := renewalCheckMax
	if seconds, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil {
		wait = time.Duration(seconds) * time.Second
	} else if at, err := http.ParseTime(retryAfter); err == nil {
		wait = at.Sub(now)
	}
	return min(max(wait, renewalCheckMin), renewalCheckMax)
}

// fallbackRenewAt is when to renew leaf when its window cannot be read: two
// thirds through its lifetime, as Let's Encrypt recommends.
func fallbackRenewAt(leaf *x509.Certificate) time.Time {
	return leaf.NotBefore.Add(leaf.NotAfter.Sub(leaf.NotBefore) * 2 / 3)
}

// jittered spreads d by up to a tenth either way, so the nodes a pod started
// together do not refresh in the same poll every hour.
func jittered(d time.Duration) time.Duration {
	return d - d/10 + rand.N(d/5+1) //nolint:gosec // spreading load, not security
}
