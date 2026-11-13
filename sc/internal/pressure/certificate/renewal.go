package certificate

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
)

const (
	// renewalCheckMin and renewalCheckMax bound how often a slot asks for a
	// certificate's renewal window, whatever Retry-After Let's Encrypt
	// sends.
	renewalCheckMin = time.Hour
	renewalCheckMax = 6 * time.Hour
)

// renewalWindows says when a certificate should be renewed.
type renewalWindows interface {
	// RenewAt returns when to renew leaf and when to ask again.
	RenewAt(ctx context.Context, leaf *x509.Certificate, now time.Time) (renewAt, checkAgain time.Time, err error)
}

// ariClient reads renewal windows from an ACME server's renewalInfo resource.
// The original sends through the server's outbound policy client; this port
// takes any *http.Client.
type ariClient struct {
	directoryURL string
	client       *http.Client

	// mu guards renewalInfoURL, read from the directory on first use.
	mu             sync.Mutex
	renewalInfoURL string
}

var _ renewalWindows = (*ariClient)(nil)

// renewalInfo is the ARI response.
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

// renewalPoint picks the moment in a window to renew leaf, derived from the
// serial number so every slot holding the certificate picks the same one.
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
