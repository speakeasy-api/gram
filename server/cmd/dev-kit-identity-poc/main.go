// Command dev-kit-identity-poc verifies a metadata ID token with the production
// workload verifier. It does not provision resources or write identity records.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	assertion "github.com/speakeasy-api/gram/server/internal/usersessions/assertion"
	"github.com/speakeasy-api/gram/server/internal/usersessions/assertion/workload"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
	"github.com/speakeasy-api/gram/server/internal/usersessions/replay"
)

const googleIssuer = "https://accounts.google.com"
const googleJWKS = "https://www.googleapis.com/oauth2/v3/certs"

type keys struct{ set jose.JSONWebKeySet }

func (k keys) VerificationKeyForAlgorithm(_ context.Context, _ jwks.Source, kid string, algorithm jose.SignatureAlgorithm) (*jose.JSONWebKey, error) {
	if algorithm != jose.RS256 {
		return nil, errors.New("expected RS256")
	}
	for _, key := range k.set.Keys {
		if key.KeyID == kid && key.IsPublic() && key.Use == "sig" && key.Algorithm == string(algorithm) {
			return &key, nil
		}
	}
	return nil, errors.New("signing key not found")
}

// Process-local replay state is sufficient only for this single-process probe.
type guard struct{ seen map[replay.Key]bool }

func (g *guard) MaxHold() time.Duration { return assertion.ReplayHoldFor(time.Hour) }
func (g *guard) Reserve(_ context.Context, key replay.Key, _ time.Time) (bool, error) {
	old := g.seen[key]
	g.seen[key] = true
	return !old, nil
}

type report struct {
	Issuer                string    `json:"issuer"`
	Subject               string    `json:"subject"`
	Audience              string    `json:"audience"`
	ExpiresAt             time.Time `json:"expires_at"`
	CachedTokenAccepted   bool      `json:"cached_token_accepted"`
	WrongSubjectRejected  bool      `json:"wrong_subject_rejected"`
	WrongAudienceRejected bool      `json:"wrong_audience_rejected"`
}

func verify(ctx context.Context, raw, subject, audience string, publicKeys keys) (report, error) {
	var out report
	if subject == "" || audience == "" {
		return out, errors.New("expected subject and audience required")
	}
	source, err := jwks.NewRemoteSource(googleJWKS)
	if err != nil {
		return out, err
	}
	verifier, err := workload.NewVerifier(publicKeys, &guard{seen: map[replay.Key]bool{}})
	if err != nil {
		return out, err
	}
	expected := workload.Expectation{Issuer: googleIssuer, Subject: subject, KeySource: source, ReplayIssuer: "kit-identity-poc", ReplayParty: "google", Audiences: []string{audience}, MaxLifetime: time.Hour}
	result, err := verifier.Verify(ctx, raw, expected)
	if err != nil {
		return out, fmt.Errorf("workload verification failed: %s", workload.ReasonOf(err))
	}
	// The production verifier already checked the signature and claims.
	token, err := jwt.ParseSigned(raw, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil {
		return out, errors.New("invalid signed token")
	}
	var claims jwt.Claims
	if err = token.UnsafeClaimsWithoutVerification(&claims); err != nil {
		return out, errors.New("invalid verified claims")
	}
	again, err := verifier.Verify(ctx, raw, expected)
	if err != nil {
		return out, errors.New("cached-token verification failed")
	}
	bad := expected
	bad.Subject = subject + "-different"
	_, err = verifier.Verify(ctx, raw, bad)
	wrongSubject := err != nil
	bad = expected
	bad.Audiences = []string{audience + "/different"}
	_, err = verifier.Verify(ctx, raw, bad)
	wrongAudience := err != nil
	if !wrongSubject || !wrongAudience {
		return out, errors.New("negative identity checks failed")
	}
	return report{Issuer: claims.Issuer, Subject: claims.Subject, Audience: audience, ExpiresAt: result.ExpiresAt, CachedTokenAccepted: again.ReusedAssertion, WrongSubjectRejected: wrongSubject, WrongAudienceRejected: wrongAudience}, nil
}
func run() error {
	subject := flag.String("subject", "", "expected IAM service account numeric unique ID (not email)")
	audience := flag.String("audience", "", "expected audience used in the metadata request")
	flag.Parse()
	if *subject == "" || *audience == "" {
		return errors.New("-subject and -audience are required")
	}
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, 8193))
	if err != nil {
		return errors.New("read token stdin")
	}
	if len(raw) > 8192 {
		return errors.New("token exceeds size limit")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, googleJWKS, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("JWKS redirect rejected") }}
	res, err := client.Do(req)
	if err != nil {
		return errors.New("Google JWKS fetch failed")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("Google JWKS HTTP %d", res.StatusCode)
	}
	var publicKeys keys
	if err = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&publicKeys.set); err != nil {
		return errors.New("invalid Google JWKS")
	}
	out, err := verify(ctx, strings.TrimSpace(string(raw)), *subject, *audience, publicKeys)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(out)
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
