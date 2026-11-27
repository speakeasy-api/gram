package transcriptstore

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/jackc/pgx/v5"
	"golang.org/x/sync/singleflight"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/repo"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
)

// Provisioner creates an organization's transcript key when it has none.
// *orgencryption.Provisioner implements it.
type Provisioner interface {
	EnsureUseCase(ctx context.Context, organizationID string, useCase orgencryption.UseCase, actor orgencryption.Actor) error
}

const (
	// projectOrganizationTTL bounds how long a renamed organization slug can
	// keep matching the flag's old target.
	projectOrganizationTTL  = 5 * time.Minute
	projectOrganizationSize = 10_000
	// provisionRetryAfter spaces out key provisioning attempts for an
	// organization whose last attempt failed, so a KMS outage does not turn
	// every write into a provisioning call.
	provisionRetryAfter = time.Minute
	provisionTimeout    = 30 * time.Second
)

type projectOrganization struct {
	id   string
	slug string
}

// flagGate decides which projects FlagChatTranscriptShadow puts in shadow
// mode. Only Transcripts calls it. It never calls PostHog over the network:
// the flag is evaluated from the locally cached definitions, so a flip lands
// within the SDK's one minute poll.
type flagGate struct {
	features    feature.Provider
	provisioner Provisioner
	logger      *slog.Logger
	// organizations caches each project's organization ID and slug.
	organizations *expirable.LRU[uuid.UUID, projectOrganization]
	// provisioned holds the organizations whose transcript key this process
	// has confirmed exists. A flag-enabled project is in shadow mode only once
	// its organization is in it, so Seal never runs for an organization
	// Prepare has not provisioned.
	provisioned sync.Map
	// failedAt holds when provisioning last failed for an organization.
	failedAt  sync.Map
	provision singleflight.Group
}

func newFlagGate(features feature.Provider, provisioner Provisioner, logger *slog.Logger) *flagGate {
	return &flagGate{
		features:      features,
		provisioner:   provisioner,
		logger:        logger,
		organizations: expirable.NewLRU[uuid.UUID, projectOrganization](projectOrganizationSize, nil, projectOrganizationTTL),
		provisioned:   sync.Map{},
		failedAt:      sync.Map{},
		provision:     singleflight.Group{},
	}
}

// organization returns the project's organization. ok is false when the
// lookup fails, which reads as plaintext. db is the caller's transaction when
// it has one, so a cache miss inside it takes no second connection.
func (g *flagGate) organization(ctx context.Context, db repo.DBTX, projectID uuid.UUID) (projectOrganization, bool) {
	if org, ok := g.organizations.Get(projectID); ok {
		return org, true
	}
	row, err := repo.New(db).GetProjectOrganization(ctx, projectID)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			g.logger.WarnContext(ctx, "look up chat transcript project organization, treating the project as plaintext",
				attr.SlogProjectID(projectID.String()), attr.SlogError(err))
		}
		return projectOrganization{id: "", slug: ""}, false
	}
	org := projectOrganization{id: row.OrganizationID, slug: row.OrganizationSlug}
	g.organizations.Add(projectID, org)
	return org, true
}

// enabled evaluates the flag for the organization from the cached
// definitions. An error or an undecided evaluation reads as off.
func (g *flagGate) enabled(ctx context.Context, org projectOrganization) bool {
	on, err := g.features.IsFlagEnabledLocal(ctx, feature.FlagChatTranscriptShadow, org.id, feature.OrgProjectGroups(org.slug, ""), nil)
	if err != nil {
		g.logger.WarnContext(ctx, "evaluate chat transcript shadow flag, treating the organization as plaintext",
			attr.SlogOrganizationID(org.id), attr.SlogError(err))
		return false
	}
	return on
}

func (g *flagGate) isProvisioned(organizationID string) bool {
	_, ok := g.provisioned.Load(organizationID)
	return ok
}

// ensureProvisioned creates the organization's transcript key when it has
// none, once per process. Call it only outside a transaction: provisioning
// may call KMS. It reports false when provisioning failed, and then the
// organization stays plaintext and is retried after provisionRetryAfter.
func (g *flagGate) ensureProvisioned(ctx context.Context, organizationID string) bool {
	if g.isProvisioned(organizationID) {
		return true
	}
	if at, ok := g.failedAt.Load(organizationID); ok {
		if failed, isTime := at.(time.Time); isTime && time.Since(failed) < provisionRetryAfter {
			return false
		}
	}
	_, err, _ := g.provision.Do(organizationID, func() (any, error) {
		// Shared by every caller waiting on this organization, so one
		// cancelled request does not fail the others.
		provisionCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), provisionTimeout)
		defer cancel()
		if err := g.provisioner.EnsureUseCase(provisionCtx, organizationID, orgencryption.UseCaseChatTranscripts, orgencryption.SystemActor()); err != nil {
			return nil, err //nolint:wrapcheck // logged below with its context.
		}
		g.provisioned.Store(organizationID, struct{}{})
		g.failedAt.Delete(organizationID)
		return nil, nil
	})
	if err != nil {
		g.failedAt.Store(organizationID, time.Now())
		g.logger.ErrorContext(ctx, "provision chat transcript encryption for a flag-enabled organization, keeping it plaintext",
			attr.SlogOrganizationID(organizationID), attr.SlogError(err))
		return false
	}
	return true
}
