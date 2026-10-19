package workloadpolicy

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/workload_identities"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/issuerurl"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
	"github.com/speakeasy-api/gram/server/internal/workloadpolicy/repo"
)

const (
	// deviceJoinTokenPrefix makes a leaked join token recognisable to secret
	// scanners and to anyone reading a log.
	deviceJoinTokenPrefix = "gram_dj_"

	// deviceIssuerName labels the issuer Speakeasy creates for an organization's
	// enrolled devices.
	deviceIssuerName = "Speakeasy devices"

	// deviceIssuerPath is where the device issuer lives under the server URL.
	// The issuer string is never fetched: device assertions verify against the
	// key registered for each subject.
	deviceIssuerPath = "/workload/devices/"

	// deviceSubjectPrefix marks a subject Speakeasy minted for an enrolled device.
	deviceSubjectPrefix = "device:"
)

// registerDeviceRate protects the database from probe floods; tokens are 256-bit
// random, so no rate is needed to protect them. registerDeviceTokenRate keeps one
// token's retries to its own bucket.
var (
	registerDeviceRate      = ratelimit.PerMinute(600).WithBurst(120)
	registerDeviceTokenRate = ratelimit.PerMinute(20).WithBurst(10)
)

// requireDeviceEnrollmentGrant refuses a join token for an agent whose live
// policy lacks org:device_agent_sync, without which an enrolled device could
// not even poll its policy. Enrollment needs nothing else: hooks access is
// checked per request against the project each hook names, so an agent with
// no hooks grant can still enroll devices that only use MCP.
func (s *Service) requireDeviceEnrollmentGrant(ctx context.Context, organizationID string, agentID uuid.UUID) error {
	grants, err := runtimepolicy.LoadAgentPolicy(ctx, s.db, organizationID, urn.NewPrincipal(urn.PrincipalTypeAgent, agentID.String()))
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error loading agent policy").LogError(ctx, s.logger)
	}
	if !authz.GrantsSatisfy(grants, authz.Check{Scope: authz.ScopeOrgDeviceAgentSync, ResourceKind: "", ResourceID: organizationID, Dimensions: nil}) {
		return oops.E(oops.CodeFailedPrecondition, nil, "the agent's policy must grant %s on the organization before a device can enroll under it", authz.ScopeOrgDeviceAgentSync)
	}
	return nil
}

// DeviceIssuer is the issuer an organization's enrolled devices sign as.
func DeviceIssuer(serverURL, organizationID string) string {
	return strings.TrimRight(serverURL, "/") + deviceIssuerPath + organizationID
}

// isReservedDeviceIssuer reports whether issuer falls under the device issuer
// namespace once canonicalized the way issuer lookup canonicalizes it, so a
// spelling such as an explicit :443 or an uppercase host cannot claim it.
func isReservedDeviceIssuer(issuer, serverURL string) bool {
	prefix := strings.TrimRight(serverURL, "/") + strings.TrimRight(deviceIssuerPath, "/")
	canonicalPrefix, err := issuerurl.Parse(prefix)
	if err != nil {
		return strings.HasPrefix(strings.TrimSpace(issuer), prefix)
	}
	canonical, err := issuerurl.Parse(issuer)
	if err != nil {
		return false
	}
	c, p := canonical.String(), canonicalPrefix.String()
	return c == p || strings.HasPrefix(c, p+"/")
}

func (s *Service) CreateDeviceJoinToken(ctx context.Context, payload *gen.CreateDeviceJoinTokenPayload) (*gen.WorkloadDeviceJoinToken, error) {
	t, err := s.resolve(ctx, authz.ScopeWorkloadWrite)
	if err != nil {
		return nil, err
	}
	// The creator is recorded as the enrollment's actor, so a token needs a
	// person behind it; a principal-backed key carries no user.
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.UserID == "" {
		return nil, oops.E(oops.CodeForbidden, nil, "a device join token must be created by a person, not an agent credential")
	}

	agentID, err := uuid.Parse(payload.AgentID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "agent_id is not a valid uuid")
	}

	agent, err := s.repo.GetDeviceJoinAgent(ctx, repo.GetDeviceJoinAgentParams{OrganizationID: t.organizationID, ID: agentID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "agent not found")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "error resolving agent").LogError(ctx, s.logger)
	}
	if !deviceJoinAgentRunnable(agent) {
		return nil, oops.E(oops.CodeFailedPrecondition, nil, "the agent is suspended, revoked, or awaiting a new owner, so no device can enroll under it")
	}

	// An enrolled device acts under the agent's whole policy, which is what
	// authorizing the agent means. Owning it is the other way to hold that.
	if agent.OwnerUserID != authCtx.UserID {
		if err := s.authz.Require(ctx, authz.Check{
			Scope:        authz.ScopeAgentAuthorize,
			ResourceKind: authz.ResourceKindAgent,
			ResourceID:   agent.ID.String(),
			Dimensions:   nil,
		}); err != nil {
			return nil, err
		}
	}

	if err := s.requireDeviceEnrollmentGrant(ctx, t.organizationID, agent.ID); err != nil {
		return nil, err
	}

	token, hash, err := newDeviceJoinToken()
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error generating join token").LogError(ctx, s.logger)
	}
	expiresAt := time.Now().Add(time.Duration(payload.ExpiresInSeconds) * time.Second)

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error starting transaction").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	row, err := repo.New(dbtx).CreateWorkloadDeviceJoinToken(ctx, repo.CreateWorkloadDeviceJoinTokenParams{
		OrganizationID:  t.organizationID,
		TokenHash:       hash,
		AgentID:         agent.ID,
		CreatedByUserID: authCtx.UserID,
		ExpiresAt:       pgtype.Timestamptz{Time: expiresAt, InfinityModifier: pgtype.Finite, Valid: true},
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error creating join token").LogError(ctx, s.logger)
	}

	if err := s.audit.LogWorkloadDeviceJoinTokenCreate(ctx, dbtx, audit.LogWorkloadDeviceJoinTokenCreateEvent{
		OrganizationID:       t.organizationID,
		Actor:                t.actor,
		ActorDisplayName:     t.actorEmail,
		ActorSlug:            nil,
		JoinTokenURN:         urn.NewWorkloadDeviceJoinToken(row.ID),
		JoinTokenDisplayName: agent.Name,
		JoinTokenSnapshotAfter: &audit.WorkloadDeviceJoinTokenSnapshot{
			AgentID:       agent.ID.String(),
			ExpiresAt:     row.ExpiresAt.Time.UTC().Format(time.RFC3339),
			DeviceSubject: "",
			Hostname:      "",
		},
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error recording join token").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error committing transaction").LogError(ctx, s.logger)
	}

	return &gen.WorkloadDeviceJoinToken{
		Token:     token,
		ExpiresAt: row.ExpiresAt.Time.UTC().Format(time.RFC3339),
	}, nil
}

func (s *Service) RegisterDevice(ctx context.Context, payload *gen.RegisterDevicePayload) (*gen.WorkloadDeviceRegistration, error) {
	// The fleet ceiling is checked first, so traffic past it creates no
	// per-token bucket. The per-token bucket is keyed on the token's hash.
	for _, limit := range []struct {
		limiter *ratelimit.Limiter
		key     string
	}{
		{s.registerLimiter, "all"},
		{s.registerTokenLimiter, hashDeviceJoinToken(payload.JoinToken)},
	} {
		if limit.limiter == nil {
			continue
		}
		result, err := limit.limiter.Allow(ctx, limit.key)
		switch {
		case err != nil:
			return nil, oops.E(oops.CodeUnavailable, err, "device enrollment is unavailable, try again shortly").LogError(ctx, s.logger)
		case !result.Allowed:
			return nil, oops.E(oops.CodeRateLimitExceeded, nil, "too many device enrollments, try again shortly")
		}
	}

	// Validated before the token is touched, so a malformed key does not burn
	// a token the administrator would have to reissue.
	key, err := parseDevicePublicKey(payload.PublicJwk)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "public_jwk is not an acceptable device key")
	}

	subject := deviceSubjectPrefix + uuid.NewString()

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error starting transaction").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	q := repo.New(dbtx)

	joinToken, err := q.ConsumeWorkloadDeviceJoinToken(ctx, repo.ConsumeWorkloadDeviceJoinTokenParams{
		TokenHash: hashDeviceJoinToken(payload.JoinToken),
		Subject:   conv.ToPGText(subject),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return s.replayedDeviceRegistration(ctx, q, payload.JoinToken, key.thumbprint)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error redeeming join token").LogError(ctx, s.logger)
	}
	org := joinToken.OrganizationID
	actor := urn.NewPrincipal(urn.PrincipalTypeUser, joinToken.CreatedByUserID)

	// Re-checked at redemption: suspending an agent must stop new devices
	// enrolling under it, not only the ones already running.
	agent, err := q.GetDeviceJoinAgent(ctx, repo.GetDeviceJoinAgentParams{OrganizationID: org, ID: joinToken.AgentID})
	if err != nil || !deviceJoinAgentRunnable(agent) {
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeUnexpected, err, "error resolving agent").LogError(ctx, s.logger)
		}
		return nil, oops.C(oops.CodeUnauthorized)
	}
	// Re-checked too: a grant removed since the token was issued would enroll
	// a device that cannot poll. Failing here rolls back the redemption, so the
	// token is not spent.
	if err := s.requireDeviceEnrollmentGrant(ctx, org, agent.ID); err != nil {
		return nil, err
	}

	issuer, err := s.deviceIssuer(ctx, dbtx, org, actor)
	if err != nil {
		return nil, err
	}

	if _, err := q.CreateWorkloadDeviceKey(ctx, repo.CreateWorkloadDeviceKeyParams{
		OrganizationID:   org,
		WorkloadIssuerID: issuer.ID,
		Subject:          subject,
		PublicJwk:        key.document,
		Thumbprint:       key.thumbprint,
		Hostname:         conv.PtrToPGTextEmpty(payload.Hostname),
		Serial:           conv.PtrToPGTextEmpty(payload.Serial),
	}); err != nil {
		// One key per device: a key already live for another device would let
		// its private key sign as both.
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == pgerrcode.UniqueViolation {
			return nil, oops.E(oops.CodeConflict, err, "this key is already registered to another device; generate a new key")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "error registering device key").LogError(ctx, s.logger)
	}

	admission, err := q.CreateWorkloadAdmission(ctx, repo.CreateWorkloadAdmissionParams{
		OrganizationID:   org,
		ProjectID:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		WorkloadIssuerID: issuer.ID,
		Subject:          subject,
		MatchKind:        string(workloadidentity.MatchKindExact),
		Name:             conv.PtrToPGTextEmpty(payload.Hostname),
		Tags:             []string{},
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error admitting device").LogError(ctx, s.logger)
	}

	if _, err := q.UpsertWorkloadAgentAssignment(ctx, repo.UpsertWorkloadAgentAssignmentParams{
		OrganizationID:   org,
		WorkloadIssuerID: issuer.ID,
		Subject:          subject,
		MatchKind:        string(workloadidentity.MatchKindExact),
		AgentID:          agent.ID,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error assigning the device's agent").LogError(ctx, s.logger)
	}

	if err := s.audit.LogWorkloadAdmissionAdmit(ctx, dbtx, audit.LogWorkloadAdmissionAdmitEvent{
		OrganizationID:         org,
		ProjectID:              uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Actor:                  actor,
		ActorDisplayName:       nil,
		ActorSlug:              nil,
		AdmissionURN:           urn.NewWorkloadAdmission(admission.ID),
		AdmissionDisplayName:   admission.Subject,
		AdmissionSnapshotAfter: AdmissionSnapshot(admission, issuer, agent.ID.String()),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error recording device admission").LogError(ctx, s.logger)
	}

	if err := s.audit.LogWorkloadDeviceJoinTokenRedeem(ctx, dbtx, audit.LogWorkloadDeviceJoinTokenRedeemEvent{
		OrganizationID:       org,
		Actor:                actor,
		ActorDisplayName:     nil,
		ActorSlug:            nil,
		JoinTokenURN:         urn.NewWorkloadDeviceJoinToken(joinToken.ID),
		JoinTokenDisplayName: agent.Name,
		JoinTokenSnapshotBefore: &audit.WorkloadDeviceJoinTokenSnapshot{
			AgentID:       agent.ID.String(),
			ExpiresAt:     joinToken.ExpiresAt.Time.UTC().Format(time.RFC3339),
			DeviceSubject: "",
			Hostname:      "",
		},
		JoinTokenSnapshotAfter: &audit.WorkloadDeviceJoinTokenSnapshot{
			AgentID:       agent.ID.String(),
			ExpiresAt:     joinToken.ExpiresAt.Time.UTC().Format(time.RFC3339),
			DeviceSubject: subject,
			Hostname:      conv.PtrValOr(payload.Hostname, ""),
		},
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error recording join token redemption").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error committing transaction").LogError(ctx, s.logger)
	}

	return &gen.WorkloadDeviceRegistration{
		DeviceSubject:  subject,
		Issuer:         issuer.Issuer,
		AgentID:        agent.ID.String(),
		OrganizationID: org,
	}, nil
}

// replayedDeviceRegistration accepts a spent token only from the device that
// spent it, with the same key, inside the token's lifetime, so a crash before
// saving needs no new token. Every other spent token looks unknown.
func (s *Service) replayedDeviceRegistration(ctx context.Context, q *repo.Queries, joinToken string, thumbprint string) (*gen.WorkloadDeviceRegistration, error) {
	replay, err := q.GetReplayedDeviceRegistration(ctx, repo.GetReplayedDeviceRegistrationParams{
		TokenHash:  hashDeviceJoinToken(joinToken),
		Thumbprint: thumbprint,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.C(oops.CodeUnauthorized)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error redeeming join token").LogError(ctx, s.logger)
	}

	agent, err := q.GetDeviceJoinAgent(ctx, repo.GetDeviceJoinAgentParams{OrganizationID: replay.OrganizationID, ID: replay.AgentID})
	if err != nil || !deviceJoinAgentRunnable(agent) {
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeUnexpected, err, "error resolving agent").LogError(ctx, s.logger)
		}
		return nil, oops.C(oops.CodeUnauthorized)
	}

	s.logger.InfoContext(ctx, "device re-presented its enrollment",
		attr.SlogWorkloadSubject(replay.Subject.String),
		attr.SlogOrganizationID(replay.OrganizationID),
	)

	return &gen.WorkloadDeviceRegistration{
		DeviceSubject:  replay.Subject.String,
		Issuer:         replay.Issuer,
		AgentID:        replay.AgentID.String(),
		OrganizationID: replay.OrganizationID,
	}, nil
}

// deviceIssuer returns the organization's device issuer, creating it on the
// first enrollment. It is locked for write before the caller adds children, as
// every admission is.
func (s *Service) deviceIssuer(ctx context.Context, dbtx pgx.Tx, org string, actor urn.Principal) (repo.WorkloadIssuer, error) {
	q := repo.New(dbtx)

	issuers, err := q.FindDeviceIssuer(ctx, org)
	if err != nil {
		return repo.WorkloadIssuer{}, oops.E(oops.CodeUnexpected, err, "error finding the device issuer").LogError(ctx, s.logger)
	}
	created := false
	if len(issuers) == 0 {
		issuerURL := DeviceIssuer(s.sharedHosts.ServerURL, org)
		row, err := q.CreateDeviceIssuer(ctx, repo.CreateDeviceIssuerParams{
			OrganizationID: org,
			Name:           deviceIssuerName,
			Issuer:         issuerURL,
			// Required by the schema and never fetched: device assertions verify
			// against each subject's registered key.
			JwksUri: issuerURL + "/jwks.json",
		})
		switch {
		case err == nil:
			issuers, created = []repo.WorkloadIssuer{row}, true
		case errors.Is(err, pgx.ErrNoRows):
			// A concurrent enrollment created it first, or an operator holds the
			// name with an issuer of their own.
			issuers, err = q.FindDeviceIssuer(ctx, org)
			if err != nil {
				return repo.WorkloadIssuer{}, oops.E(oops.CodeUnexpected, err, "error finding the device issuer").LogError(ctx, s.logger)
			}
		default:
			return repo.WorkloadIssuer{}, oops.E(oops.CodeUnexpected, err, "error creating the device issuer").LogError(ctx, s.logger)
		}
	}
	if len(issuers) == 0 {
		return repo.WorkloadIssuer{}, oops.E(oops.CodeConflict, nil, "another trusted issuer is named %q; rename it so devices can enroll", deviceIssuerName).LogError(ctx, s.logger)
	}
	issuer := issuers[0]

	if _, err := q.LockWorkloadIssuerForWrite(ctx, repo.LockWorkloadIssuerForWriteParams{OrganizationID: org, ID: issuer.ID}); err != nil {
		return repo.WorkloadIssuer{}, oops.E(oops.CodeUnexpected, err, "error locking the device issuer").LogError(ctx, s.logger)
	}

	if created {
		if err := s.audit.LogWorkloadIssuerCreate(ctx, dbtx, audit.LogWorkloadIssuerCreateEvent{
			OrganizationID:      org,
			ProjectID:           uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			Actor:               actor,
			ActorDisplayName:    nil,
			ActorSlug:           nil,
			IssuerURN:           urn.NewWorkloadIssuer(issuer.ID),
			IssuerName:          issuer.Name,
			IssuerSnapshotAfter: IssuerSnapshot(issuer),
		}); err != nil {
			return repo.WorkloadIssuer{}, oops.E(oops.CodeUnexpected, err, "error recording the device issuer").LogError(ctx, s.logger)
		}
	}

	return issuer, nil
}

// deviceJoinAgentRunnable reports whether devices may enroll under an agent:
// one that cannot run would only produce devices that are refused.
func deviceJoinAgentRunnable(agent repo.GetDeviceJoinAgentRow) bool {
	return !agent.SuspendedAt.Valid && !agent.RevokedAt.Valid && !agent.OwnerReassignmentRequiredAt.Valid
}

func newDeviceJoinToken() (token string, hash string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("read random join token: %w", err)
	}
	token = deviceJoinTokenPrefix + base64.RawURLEncoding.EncodeToString(buf)
	return token, hashDeviceJoinToken(token), nil
}

func hashDeviceJoinToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// devicePublicKey is a validated device key, normalized for storage.
type devicePublicKey struct {
	// document is the public JWK with kid set to its thumbprint and alg set.
	document   []byte
	thumbprint string
}

// parseDevicePublicKey accepts a public EC P-256 or Ed25519 JWK and nothing
// else. Private members are refused before the key is parsed, so a device that
// sends its private key by mistake has it rejected rather than stored.
func parseDevicePublicKey(raw map[string]any) (devicePublicKey, error) {
	encoded, err := json.Marshal(raw)
	if err != nil {
		return devicePublicKey{}, errors.New("public_jwk is not valid JSON")
	}
	set, err := json.Marshal(struct {
		Keys []json.RawMessage `json:"keys"`
	}{Keys: []json.RawMessage{encoded}})
	if err != nil {
		return devicePublicKey{}, errors.New("public_jwk is not valid JSON")
	}
	if err := jwks.ValidatePublicOnly(set); err != nil {
		return devicePublicKey{}, fmt.Errorf("public_jwk: %w", err)
	}

	var key jose.JSONWebKey
	if err := key.UnmarshalJSON(encoded); err != nil {
		return devicePublicKey{}, errors.New("public_jwk is not a JWK")
	}
	if !key.Valid() || !key.IsPublic() {
		return devicePublicKey{}, errors.New("public_jwk must be a public key")
	}

	var alg jose.SignatureAlgorithm
	switch pub := key.Key.(type) {
	case *ecdsa.PublicKey:
		if pub.Curve != elliptic.P256() {
			return devicePublicKey{}, errors.New("an EC key must use the P-256 curve")
		}
		alg = jose.ES256
	case ed25519.PublicKey:
		alg = jose.EdDSA
	default:
		return devicePublicKey{}, errors.New("the key must be EC P-256 or Ed25519")
	}

	sum, err := key.Thumbprint(crypto.SHA256)
	if err != nil {
		return devicePublicKey{}, errors.New("public_jwk thumbprint could not be computed")
	}
	thumbprint := base64.RawURLEncoding.EncodeToString(sum)

	normalized := jose.JSONWebKey{
		Key:                         key.Key,
		KeyID:                       thumbprint,
		Algorithm:                   string(alg),
		Use:                         "sig",
		Certificates:                nil,
		CertificatesURL:             nil,
		CertificateThumbprintSHA1:   nil,
		CertificateThumbprintSHA256: nil,
	}
	document, err := normalized.MarshalJSON()
	if err != nil {
		return devicePublicKey{}, errors.New("public_jwk could not be normalized")
	}

	return devicePublicKey{document: document, thumbprint: thumbprint}, nil
}

// isDeviceIssuer reports whether an issuer row is the Speakeasy device issuer,
// whose admissions each carry a registered key.
func isDeviceIssuer(issuer repo.WorkloadIssuer) bool {
	return issuer.KeySource.Valid && issuer.KeySource.String == workloadidentity.KeySourceDeviceKeys
}
