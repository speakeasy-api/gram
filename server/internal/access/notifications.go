package access

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/accesscontrol"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/email"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

const accessRequestNotificationTimeout = time.Minute

// upsertAccessRequest wraps the store upsert and fires the admin notification
// in a background goroutine when the request is newly created. Any resource
// type creating access requests should call this instead of accessStore.UpsertRequest
// directly so notification is automatic.
func (s *Service) upsertAccessRequest(ctx context.Context, request accesscontrol.AccessApprovalRequest) (accesscontrol.AccessApprovalRequest, bool, error) {
	result, wasCreated, err := s.accessStore.UpsertRequest(ctx, request)
	if err != nil {
		return result, wasCreated, fmt.Errorf("upsert access request: %w", err)
	}
	if wasCreated {
		go s.notifyAdminsOfNewRequestBestEffort(context.WithoutCancel(ctx), result)
	}
	return result, wasCreated, nil
}

// notifyAdminsOfNewRequestBestEffort emails all org admins when a new access
// request is created. Failures are logged and never returned to the caller.
func (s *Service) notifyAdminsOfNewRequestBestEffort(
	ctx context.Context,
	request accesscontrol.AccessApprovalRequest,
) {
	ctx, cancel := context.WithTimeout(ctx, accessRequestNotificationTimeout)
	defer cancel()

	emails, err := s.listOrgAdminEmailsForAccessRequestNotification(ctx, request.OrganizationID)
	if err != nil {
		s.logger.WarnContext(ctx, "notify admins of new access request: get admin emails", attr.SlogError(err))
		return
	}
	var approvalURL string
	if s.siteURL.Host != "" {
		projectID, err := uuid.Parse(request.ProjectID)
		if err != nil {
			s.logger.WarnContext(ctx, "notify admins of new access request: parse project id", attr.SlogError(err))
		} else {
			slugs, err := projectsrepo.New(s.db).GetProjectWithOrganizationMetadata(ctx, projectID)
			if err != nil {
				s.logger.WarnContext(ctx, "notify admins of new access request: get url slugs", attr.SlogError(err))
			} else {
				approvalURL = s.siteURL.JoinPath(slugs.Slug, "projects", slugs.ProjectSlug, "approval-requests").String()
			}
		}
	}
	tmpl := email.AccessRequestCreated{
		RequesterEmail: request.RequesterEmail,
		DisplayName:    conv.Default(request.DisplayName, "(unknown resource)"),
		ApprovalURL:    approvalURL,
	}
	for _, adminEmail := range emails {
		if err := s.sendAccessRequestCreatedEmail(ctx, adminEmail, tmpl); err != nil {
			s.logger.WarnContext(ctx, "notify admins of new access request: send email",
				attr.SlogError(err),
				attr.SlogAuthUserEmail(adminEmail),
			)
		}
	}
}

func (s *Service) listOrgAdminEmailsForAccessRequestNotification(ctx context.Context, organizationID string) ([]string, error) {
	users, err := repo.New(s.db).ListAccessNotificationUsers(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list notification candidate users: %w", err)
	}

	emails := make([]string, 0, len(users))
	seenEmails := make(map[string]struct{}, len(users))
	check := authz.Check{
		Scope:        authz.ScopeOrgAdmin,
		ResourceKind: "",
		ResourceID:   organizationID,
		Dimensions:   nil,
	}
	for _, user := range users {
		principals, err := authz.ResolveUserPrincipals(ctx, s.db, organizationID, user.ID)
		if err != nil {
			return nil, fmt.Errorf("resolve principals for notification candidate %q: %w", user.ID, err)
		}
		grants, err := authz.LoadGrants(ctx, s.db, organizationID, principals)
		if err != nil {
			return nil, fmt.Errorf("load grants for notification candidate %q: %w", user.ID, err)
		}
		if !authz.GrantsSatisfy(grants, check) {
			continue
		}
		if _, ok := seenEmails[user.Email]; ok {
			continue
		}
		seenEmails[user.Email] = struct{}{}
		emails = append(emails, user.Email)
	}

	return emails, nil
}

func (s *Service) sendAccessRequestCreatedEmail(ctx context.Context, adminEmail string, tmpl email.AccessRequestCreated) error {
	retryDelays := [...]time.Duration{250 * time.Millisecond, 2 * time.Second}

	for _, retryDelay := range retryDelays {
		err := s.emailSvc.Send(ctx, adminEmail, tmpl)
		if err == nil {
			return nil
		}

		timer := time.NewTimer(retryDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("wait to retry access request email: %w", ctx.Err())
		case <-timer.C:
		}
	}

	if err := s.emailSvc.Send(ctx, adminEmail, tmpl); err != nil {
		return fmt.Errorf("send access request email: %w", err)
	}

	return nil
}
