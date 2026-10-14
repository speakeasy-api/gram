package usersessions

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	orggen "github.com/speakeasy-api/gram/server/gen/organization_user_session_issuers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/authserver"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

const (
	// sharedIssuerAuthnChallengeMode is the general-purpose default for a
	// shared issuer: a user who has no upstream session is sent to sign in.
	sharedIssuerAuthnChallengeMode = "interactive"

	// sharedIssuerSessionDuration is the general-purpose maximum lifetime of
	// an authorization a shared issuer grants a human: two weeks. Workload
	// grants carry their own, shorter lifetime and never refresh.
	sharedIssuerSessionDuration = 336 * time.Hour

	// sharedIssuerSlugPrefix starts every generated shared issuer slug; the
	// issuer id's leading hex digits follow it so sibling issuers read apart.
	sharedIssuerSlugPrefix = "shared-authorization-server-"

	// sharedIssuerSlugIDDigits is how many leading hex digits of the issuer
	// id the generated slug carries: enough to tell sibling issuers apart in
	// a list, short enough to read.
	sharedIssuerSlugIDDigits = 8

	// maxIdempotencyKeyLength bounds the caller-chosen idempotency key in
	// characters, matching the API form's limit. It is hashed into the issuer
	// id, so its length only bounds the request.
	maxIdempotencyKeyLength = 200
)

// sharedIssuerIDNamespace is the UUID v5 namespace shared issuer ids are
// derived under. Changing it changes every derived id, so it is fixed.
var sharedIssuerIDNamespace = uuid.MustParse("5f0a4f58-0a5b-4d3c-9a1e-7c3b2a9d6e41")

// sharedIssuerID is the id of the shared issuer an organization's idempotency
// key names. Deriving the id from the key, rather than storing the key, is
// what makes a retry and a concurrent submission land on the same primary
// key, and lets the issuer URL pinned from that id be written in the same
// statement as the row. The organization is part of the input, so the same
// key in two organizations names two issuers.
func sharedIssuerID(organizationID, idempotencyKey string) uuid.UUID {
	return uuid.NewSHA1(sharedIssuerIDNamespace, []byte(organizationID+"\x00"+idempotencyKey))
}

// CreateSharedIssuer creates an organization-owned issuer in shared
// authorization-server mode with its issuer URL pinned, or returns the issuer
// an earlier submission with the same idempotency key created.
func (s *Service) CreateSharedIssuer(ctx context.Context, payload *orggen.CreateSharedIssuerPayload) (*types.UserSessionIssuer, error) {
	authCtx, err := s.requireOrganizationIssuerScope(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))
	if strings.TrimSpace(payload.IdempotencyKey) == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "idempotency_key is required").LogWarn(ctx, logger)
	}
	if utf8.RuneCountInString(payload.IdempotencyKey) > maxIdempotencyKeyLength {
		return nil, oops.E(oops.CodeBadRequest, nil, "idempotency_key must be at most %d characters", maxIdempotencyKeyLength).LogWarn(ctx, logger)
	}

	id := sharedIssuerID(authCtx.ActiveOrganizationID, payload.IdempotencyKey)
	pinnedIssuerURL, err := s.sharedHosts.PinnedSharedIssuerURL(id)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "this deployment does not serve shared authorization servers").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	txRepo := repo.New(dbtx)

	row, err := txRepo.CreateSharedOrganizationUserSessionIssuer(ctx, repo.CreateSharedOrganizationUserSessionIssuerParams{
		ID:                      id,
		OrganizationID:          conv.ToPGText(authCtx.ActiveOrganizationID),
		Slug:                    sharedIssuerSlugPrefix + id.String()[:sharedIssuerSlugIDDigits],
		AuthnChallengeMode:      sharedIssuerAuthnChallengeMode,
		SessionDuration:         pgtype.Interval{Microseconds: sharedIssuerSessionDuration.Microseconds(), Days: 0, Months: 0, Valid: true},
		AuthorizationServerMode: string(authserver.ModeShared),
		PinnedIssuerUrl:         conv.ToPGText(pinnedIssuerURL),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// The id is taken: an earlier or concurrent submission with this key
		// inserted it, and that insert has committed by the time the conflict
		// resolves. Return its issuer when it is the live shared issuer this
		// organization owns; anything else with this id is not this request's.
		existing, err := txRepo.GetOrganizationUserSessionIssuerByID(ctx, repo.GetOrganizationUserSessionIssuerByIDParams{
			ID:             id,
			OrganizationID: authCtx.ActiveOrganizationID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, oops.E(oops.CodeConflict, err, "idempotency_key was already used for an issuer that no longer exists").LogWarn(ctx, logger)
			}
			return nil, oops.E(oops.CodeUnexpected, err, "get organization user session issuer by idempotency key").LogError(ctx, logger)
		}
		if !authserver.IssuerInSharedMode(existing) {
			return nil, oops.E(oops.CodeConflict, nil, "idempotency_key was already used for an issuer that is not a shared authorization server").LogWarn(ctx, logger)
		}
		return UserSessionIssuerView(existing), nil
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create shared organization user session issuer").LogError(ctx, logger)
	}

	if err := s.audit.LogUserSessionIssuerCreate(ctx, dbtx, audit.LogUserSessionIssuerCreateEvent{
		OrganizationID:                 authCtx.ActiveOrganizationID,
		ProjectID:                      uuid.Nil,
		Actor:                          urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:               authCtx.Email,
		ActorSlug:                      nil,
		UserSessionIssuerURN:           urn.NewUserSessionIssuer(row.ID),
		Slug:                           row.Slug,
		UserSessionIssuerSnapshotAfter: UserSessionIssuerView(row),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log shared organization user session issuer creation").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return UserSessionIssuerView(row), nil
}
