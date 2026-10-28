// Command dev-agent-identity-poc verifies a metadata ID token with the production
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
	"regexp"
	"strings"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	assertion "github.com/speakeasy-api/gram/server/internal/usersessions/assertion"
	"github.com/speakeasy-api/gram/server/internal/usersessions/assertion/workload"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
	"github.com/speakeasy-api/gram/server/internal/usersessions/replay"
)

// These inputs come from independently inspected project/cluster configuration.
type identityConfig struct{ Project, Organization, Location, Cluster, Namespace, ServiceAccount string }

func (c identityConfig) expected() (issuer, subject string, err error) {
	if !regexp.MustCompile(`^[0-9]+$`).MatchString(c.Project) || (c.Organization != "" && !regexp.MustCompile(`^[0-9]+$`).MatchString(c.Organization)) {
		return "", "", errors.New("numeric project/organization IDs required")
	}
	for _, part := range []string{c.Location, c.Cluster, c.Namespace, c.ServiceAccount} {
		if !regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`).MatchString(part) {
			return "", "", errors.New("invalid cluster identity component")
		}
	}
	scope, id, prefix := "projects", c.Project, "proj"
	if c.Organization != "" {
		scope, id, prefix = "organizations", c.Organization, "org"
	}
	domain := "agents.global." + prefix + "-" + id + ".system.id.goog"
	issuer = "https://sts.googleapis.com/v1/" + scope + "/" + id + "/locations/global/workloadIdentityPools/" + domain
	subject = "spiffe://" + domain + "/resources/container/projects/" + c.Project + "/locations/" + c.Location + "/clusters/" + c.Cluster + "/ns/" + c.Namespace + "/sa/" + c.ServiceAccount
	return issuer, subject, nil
}

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
	Audience              []string  `json:"audience"`
	ExpiresAt             time.Time `json:"expires_at"`
	WrongSubjectRejected  bool      `json:"wrong_subject_rejected"`
	WrongAudienceRejected bool      `json:"wrong_audience_rejected"`
}

func verify(ctx context.Context, raw, issuer, subject, audience string, publicKeys keys) (report, error) {
	var out report
	if subject == "" || audience == "" {
		return out, errors.New("expected subject and audience required")
	}
	source, err := jwks.NewRemoteSource(issuer + "/openid/jwks")
	if err != nil {
		return out, fmt.Errorf("create Google JWKS source: %w", err)
	}
	verifier, err := workload.NewVerifier(publicKeys, &guard{seen: map[replay.Key]bool{}})
	if err != nil {
		return out, fmt.Errorf("create workload verifier: %w", err)
	}
	expected := workload.Expectation{Issuer: issuer, Subject: subject, KeySource: source, ReplayIssuer: "agent-identity-poc", ReplayParty: "google", Audiences: []string{audience}, MaxLifetime: time.Hour, AllowAssertionReuse: false}
	result, err := verifier.Verify(ctx, raw, expected)
	if err != nil {
		return out, fmt.Errorf("workload verification failed: %s", workload.ReasonOf(err))
	}
	// The production verifier already checked the signature and claims.
	token, err := jwt.ParseSigned(raw, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil {
		return out, errors.New("invalid signed token")
	}
	var claims struct {
		jwt.Claims
		Confirmation json.RawMessage `json:"cnf"`
	}
	if err = token.UnsafeClaimsWithoutVerification(&claims); err != nil {
		return out, errors.New("invalid verified claims")
	}
	if len(claims.Confirmation) != 0 {
		return out, errors.New("certificate-bound token unsupported; request unbound token")
	}
	if claims.Expiry == nil || !claims.Expiry.Time().After(time.Now()) {
		return out, errors.New("token expired")
	}
	bad := expected
	bad.Subject = subject + "-different"
	_, err = verifier.Verify(ctx, raw, bad)
	wrongSubject := workload.ReasonOf(err) == workload.ReasonSubjectMismatch
	bad = expected
	bad.Audiences = []string{audience + "/different"}
	_, err = verifier.Verify(ctx, raw, bad)
	wrongAudience := workload.ReasonOf(err) == workload.ReasonAudienceMismatch
	if !wrongSubject || !wrongAudience {
		return out, errors.New("negative identity checks failed")
	}
	return report{Issuer: claims.Issuer, Subject: claims.Subject, Audience: claims.Audience, ExpiresAt: result.ExpiresAt, WrongSubjectRejected: wrongSubject, WrongAudienceRejected: wrongAudience}, nil
}
func run() error {
	var c identityConfig
	flag.StringVar(&c.Project, "project-number", "", "cluster project number")
	flag.StringVar(&c.Organization, "organization-id", "", "organization ancestor ID; empty only for standalone project")
	flag.StringVar(&c.Location, "location", "", "control plane region or zone")
	flag.StringVar(&c.Cluster, "cluster", "", "cluster name")
	flag.StringVar(&c.Namespace, "namespace", "", "dedicated namespace")
	flag.StringVar(&c.ServiceAccount, "service-account", "native-agent-probe", "dedicated Kubernetes service account")
	audience := flag.String("audience", "", "expected audience")
	standalone := flag.Bool("standalone-project", false, "confirm project has no organization ancestor")
	flag.Parse()
	if *audience == "" || (*standalone == (c.Organization != "")) {
		return errors.New("audience and exactly one of organization-id or standalone-project required")
	}
	issuer, subject, err := c.expected()
	if err != nil {
		return err
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, issuer+"/openid/jwks", nil)
	if err != nil {
		return fmt.Errorf("create Google JWKS request: %w", err)
	}
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("JWKS redirect rejected") }}
	res, err := client.Do(req)
	if err != nil {
		return errors.New("fetch Google JWKS failed")
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch Google JWKS: HTTP %d", res.StatusCode)
	}
	var publicKeys keys
	if err = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&publicKeys.set); err != nil {
		return errors.New("invalid Google JWKS")
	}
	out, err := verify(ctx, strings.TrimSpace(string(raw)), issuer, subject, *audience, publicKeys)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		return fmt.Errorf("encode verification report: %w", err)
	}
	return nil
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
