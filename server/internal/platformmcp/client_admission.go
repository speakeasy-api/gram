package platformmcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// clientAdmissionCustomURLLimit bounds the custom document URLs one read
// returns. An issuer configured by hand holds a handful; the cap exists so a
// tool result stays bounded rather than to page a real collection.
const clientAdmissionCustomURLLimit = 50

var (
	ErrClientAdmissionInvalid     = errors.New("invalid platform mcp client admission request")
	ErrClientAdmissionUnavailable = errors.New("platform mcp client admission unavailable")
)

// ClientAdmission is the CIMD admission state of one registered MCP's session
// issuer. Mode is always the EFFECTIVE mode: an issuer that never had one set
// reports the resolved default, matching what the dashboard shows.
type ClientAdmission struct {
	Mode             string
	AllowedModes     []string
	CustomClientURLs []string
}

// ClientAdmissionService reads and writes the CIMD admission policy of the
// session issuer a Platform MCP registration owns. It is the tool-surface
// equivalent of MCP Server -> Settings -> Authentication, and writes the same
// audit event the management API writes for the same change.
//
// Nothing here is connection-scoped: a registration is resolved through the
// connection-tolerant lifecycle lookup, and the write is attributed to the
// caller's real user.
type ClientAdmissionService struct {
	db    *pgxpool.Pool
	audit *audit.Logger
}

func NewClientAdmissionService(db *pgxpool.Pool, auditLogger *audit.Logger) *ClientAdmissionService {
	return &ClientAdmissionService{db: db, audit: auditLogger}
}

func (s *ClientAdmissionService) valid() bool {
	return s != nil && s.db != nil && s.audit != nil
}

func (s *ClientAdmissionService) Get(ctx context.Context, principal Principal, project ResolvedProject, registrationID uuid.UUID) (ClientAdmission, error) {
	if !s.valid() || principal.OrganizationID == "" || principal.UserID == "" || project.ID == uuid.Nil || registrationID == uuid.Nil {
		return ClientAdmission{}, ErrClientAdmissionUnavailable
	}
	issuerID, err := s.registrationIssuer(ctx, s.db, principal, project, registrationID)
	if err != nil {
		return ClientAdmission{}, err
	}
	q := usersessionsrepo.New(s.db)
	issuer, err := q.GetUserSessionIssuerByID(ctx, usersessionsrepo.GetUserSessionIssuerByIDParams{ID: issuerID, ProjectID: project.ID, OrganizationID: principal.OrganizationID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ClientAdmission{}, ErrRegistrationInvalid
	}
	if err != nil {
		return ClientAdmission{}, fmt.Errorf("load platform mcp client admission issuer: %w", err)
	}
	urls, err := s.customClientURLs(ctx, q, project.ID, principal.OrganizationID, issuerID)
	if err != nil {
		return ClientAdmission{}, err
	}
	mode, _ := admission.ResolveMode(issuer.ClientIDMetadataAdmissionMode.String, issuer.ClientIDMetadataAdmissionMode.Valid)
	return clientAdmission(string(mode), urls), nil
}

// Set writes one admission mode. The caller is responsible for having obtained
// explicit user confirmation: this changes which MCP clients may authorize
// against the registered server, and ModeDisabled additionally withdraws the
// advertised CIMD support from the issuer's RFC 8414 metadata.
func (s *ClientAdmissionService) Set(ctx context.Context, principal Principal, project ResolvedProject, registrationID uuid.UUID, mode string) (ClientAdmission, error) {
	if !s.valid() || principal.OrganizationID == "" || principal.UserID == "" || project.ID == uuid.Nil || registrationID == uuid.Nil {
		return ClientAdmission{}, ErrClientAdmissionUnavailable
	}
	// Validated here as well as at the tool boundary: the enum documented on
	// the tool schema only guards a well-behaved client.
	if !admission.IsValidMode(mode) {
		return ClientAdmission{}, ErrClientAdmissionInvalid
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return ClientAdmission{}, fmt.Errorf("begin platform mcp client admission update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	issuerID, err := s.registrationIssuer(ctx, tx, principal, project, registrationID)
	if err != nil {
		return ClientAdmission{}, err
	}
	q := usersessionsrepo.New(tx)
	// The same row lock the dashboard's issuer writes take, so two concurrent
	// mode changes serialize rather than interleave their audit snapshots.
	if _, err := q.LockUserSessionIssuer(ctx, usersessionsrepo.LockUserSessionIssuerParams{ID: issuerID, ProjectID: project.ID, OrganizationID: principal.OrganizationID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ClientAdmission{}, ErrRegistrationInvalid
		}
		return ClientAdmission{}, fmt.Errorf("lock platform mcp client admission issuer: %w", err)
	}
	existing, err := q.GetUserSessionIssuerByID(ctx, usersessionsrepo.GetUserSessionIssuerByIDParams{ID: issuerID, ProjectID: project.ID, OrganizationID: principal.OrganizationID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ClientAdmission{}, ErrRegistrationInvalid
	}
	if err != nil {
		return ClientAdmission{}, fmt.Errorf("load platform mcp client admission issuer: %w", err)
	}
	updated, err := q.UpdateUserSessionIssuer(ctx, usersessionsrepo.UpdateUserSessionIssuerParams{
		// Omitted fields keep their stored values: only the admission mode is
		// written here.
		Slug:                          pgtype.Text{String: "", Valid: false},
		AuthnChallengeMode:            pgtype.Text{String: "", Valid: false},
		SessionDuration:               pgtype.Interval{Microseconds: 0, Days: 0, Months: 0, Valid: false},
		ClientIDMetadataAdmissionMode: conv.ToPGText(mode),
		ID:                            issuerID,
		ProjectID:                     project.ID,
		OrganizationID:                principal.OrganizationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ClientAdmission{}, ErrRegistrationInvalid
	}
	if err != nil {
		return ClientAdmission{}, fmt.Errorf("update platform mcp client admission mode: %w", err)
	}
	if err := s.audit.LogUserSessionIssuerUpdate(ctx, tx, audit.LogUserSessionIssuerUpdateEvent{
		OrganizationID:                  principal.OrganizationID,
		ProjectID:                       project.ID,
		Actor:                           urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID),
		ActorDisplayName:                nil,
		ActorSlug:                       nil,
		UserSessionIssuerURN:            urn.NewUserSessionIssuer(updated.ID),
		Slug:                            updated.Slug,
		UserSessionIssuerSnapshotBefore: usersessions.UserSessionIssuerView(existing),
		UserSessionIssuerSnapshotAfter:  usersessions.UserSessionIssuerView(updated),
	}); err != nil {
		return ClientAdmission{}, fmt.Errorf("audit platform mcp client admission mode: %w", err)
	}
	urls, err := s.customClientURLs(ctx, q, project.ID, principal.OrganizationID, issuerID)
	if err != nil {
		return ClientAdmission{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ClientAdmission{}, fmt.Errorf("commit platform mcp client admission update: %w", err)
	}
	resolved, _ := admission.ResolveMode(updated.ClientIDMetadataAdmissionMode.String, updated.ClientIDMetadataAdmissionMode.Valid)
	return clientAdmission(string(resolved), urls), nil
}

// registrationIssuer resolves the session issuer of a complete registration the
// caller may act on. An incomplete registration has no issuer to configure yet.
func (s *ClientAdmissionService) registrationIssuer(ctx context.Context, db platformrepo.DBTX, principal Principal, project ResolvedProject, registrationID uuid.UUID) (uuid.UUID, error) {
	registration, err := lifecycleRegistration(ctx, platformrepo.New(db), principal, project.ID, registrationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrRegistrationInvalid
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("load platform mcp client admission registration: %w", err)
	}
	// Ownership matters here as it does for metadata updates: an issuer this
	// registration did not create may serve other MCPs, and an admission mode is
	// a property of the issuer, not of one server attached to it.
	if registration.Status != registrationStatusRegistered || !registrationComponentsComplete(registration) || !registration.UserSessionIssuerOwned {
		return uuid.Nil, ErrRegistrationInvalid
	}
	return registration.UserSessionIssuerID.UUID, nil
}

func (s *ClientAdmissionService) customClientURLs(ctx context.Context, q *usersessionsrepo.Queries, projectID uuid.UUID, organizationID string, issuerID uuid.UUID) ([]string, error) {
	rows, err := q.ListUserSessionIssuerCimdClientsByIssuerID(ctx, usersessionsrepo.ListUserSessionIssuerCimdClientsByIssuerIDParams{
		ProjectID:           projectID,
		OrganizationID:      organizationID,
		UserSessionIssuerID: issuerID,
		Cursor:              uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		LimitValue:          clientAdmissionCustomURLLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("list platform mcp client admission custom clients: %w", err)
	}
	urls := make([]string, 0, len(rows))
	for _, row := range rows {
		urls = append(urls, row.ClientIDMetadataUri)
	}
	return urls, nil
}

func clientAdmission(mode string, urls []string) ClientAdmission {
	allowed := make([]string, 0, len(admission.Modes()))
	for _, value := range admission.Modes() {
		allowed = append(allowed, string(value))
	}
	return ClientAdmission{Mode: mode, AllowedModes: allowed, CustomClientURLs: urls}
}
