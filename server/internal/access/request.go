package access

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/email"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// ErrAccessRequestUnavailable is returned when notifications cannot be sent.
var ErrAccessRequestUnavailable = errors.New("access request unavailable")

// NotifyInput is one access or new-server request to email to org admins.
type NotifyInput struct {
	// OrganizationID is the organization whose administrators are notified.
	OrganizationID string
	// UserID is the requester.
	UserID string
	// Scope is the RBAC scope being requested, such as mcp:connect or mcp:write.
	Scope string
	// ResourceID is an optional resource the scope applies to.
	ResourceID string
	// ResourceName is an optional human-readable name for that resource, or
	// the MCP server a member is asking an administrator to add.
	ResourceName string
}

// NotifyResult is the outcome of Notify.
type NotifyResult struct {
	// SentToCount is the number of administrators who received email.
	SentToCount int
}

// Requester emails organization administrators when a member requests access
// or a new MCP server. It does not require an AuthContext, so OAuth consent
// and Platform MCP tools can call it with a resolved user and organization.
type Requester struct {
	logger  *slog.Logger
	db      *pgxpool.Pool
	email   *email.Service
	siteURL *url.URL
}

// NewRequester constructs a Requester. emailService may be a no-op client.
func NewRequester(logger *slog.Logger, db *pgxpool.Pool, emailService *email.Service, siteURL *url.URL) *Requester {
	if logger == nil {
		logger = slog.Default()
	}
	return &Requester{
		logger:  logger.With(attr.SlogComponent("access_request")),
		db:      db,
		email:   emailService,
		siteURL: siteURL,
	}
}

// Notify emails active organization administrators about in.
func (r *Requester) Notify(ctx context.Context, in NotifyInput) (NotifyResult, error) {
	if r == nil || r.db == nil || r.email == nil || in.OrganizationID == "" || in.UserID == "" {
		return NotifyResult{}, ErrAccessRequestUnavailable
	}

	logger := r.logger.With(
		attr.SlogOrganizationID(in.OrganizationID),
		attr.SlogUserID(in.UserID),
	)
	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(in.OrganizationID),
		attr.UserID(in.UserID),
	)

	requester, err := usersrepo.New(r.db).GetUser(ctx, in.UserID)
	if err != nil {
		return NotifyResult{}, fmt.Errorf("get requester info: %w", err)
	}

	org, err := orgrepo.New(r.db).GetOrganizationMetadata(ctx, in.OrganizationID)
	if err != nil {
		return NotifyResult{}, fmt.Errorf("get organization info: %w", err)
	}

	admins, err := repo.New(r.db).ListActiveOrganizationAdmins(ctx, in.OrganizationID)
	if err != nil {
		return NotifyResult{}, fmt.Errorf("list organization administrators: %w", err)
	}

	if len(admins) == 0 {
		logger.WarnContext(ctx, "no org admins found to notify for access request")
		return NotifyResult{SentToCount: 0}, nil
	}

	manageAccessLink := r.manageAccessLink(org.Slug, in)

	tmpl := email.AccessRequest{
		RequesterName:    conv.Default(requester.DisplayName, requester.Email),
		OrganizationName: org.Name,
		ManageAccessLink: manageAccessLink,
	}

	sentCount := 0
	for _, admin := range admins {
		if admin.Email == "" {
			continue
		}
		if err := r.email.Send(ctx, admin.Email, tmpl); err != nil {
			logger.WarnContext(ctx, "failed to send access request email",
				attr.SlogError(err),
				attr.SlogAccessRequestRecipient(admin.Email),
			)
			continue
		}
		sentCount++
	}

	if sentCount == 0 {
		return NotifyResult{}, ErrAccessRequestUnavailable
	}

	logger.InfoContext(ctx, "access request emails sent",
		attr.SlogAccessRequestSentCount(sentCount),
		attr.SlogAccessRequestAdminCount(len(admins)),
		attr.SlogAccessRequestScope(in.Scope),
	)

	return NotifyResult{SentToCount: sentCount}, nil
}

func (r *Requester) manageAccessLink(organizationSlug string, in NotifyInput) string {
	if r.siteURL == nil || organizationSlug == "" {
		return ""
	}

	accessURL := r.siteURL.JoinPath(organizationSlug, "access", "roles")
	q := url.Values{}
	q.Set("grant_user", in.UserID)
	if in.Scope != "" {
		q.Set("scope", in.Scope)
	}
	if in.ResourceID != "" {
		q.Set("resource_id", in.ResourceID)
	}
	if in.ResourceName != "" {
		q.Set("resource_name", in.ResourceName)
	}
	accessURL.RawQuery = q.Encode()
	return accessURL.String()
}
