package identityproviders

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/sync/errgroup"

	gen "github.com/speakeasy-api/gram/server/gen/identity_providers"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/identityproviders/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/okta"
)

const (
	applicationInventoryLimit            = 500
	applicationAssignmentCountLimit      = 50
	applicationAssignmentConcurrency     = 4
	applicationAssignedGroupLimit        = 10
	applicationAssignedGroupPageLimit    = 20
	applicationInventoryTimeout          = 120 * time.Second
	oktaCollectionPageLimit              = 200
	oktaRateLimitRemainingSleepThreshold = 10
)

func (s *Service) ListApplications(ctx context.Context, _ *gen.ListApplicationsPayload) (*gen.ListIdentityProviderApplicationsResult, error) {
	authCtx, logger, err := s.requireAccess(ctx, authz.ScopeOrgRead)
	if err != nil {
		return nil, err
	}

	requestCtx, cancel := context.WithTimeout(ctx, applicationInventoryTimeout)
	defer cancel()

	connection, err := repo.New(s.db).GetIdentityProviderConnectionByOrganization(requestCtx, authCtx.ActiveOrganizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, nil, "identity provider connection not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error loading identity provider connection").LogError(ctx, logger)
	}
	token, err := s.acquireOktaManagementToken(requestCtx, authCtx.ActiveOrganizationID, connection, []string{"okta.apps.read", "okta.groups.read"})
	if err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "error authorizing the Okta application inventory").LogError(ctx, logger)
	}

	tenantDomain := normalizeOktaDomain(connection.TenantIdentifier)
	applications := make([]*gen.IdentityProviderApplication, 0)
	rateLimits := &oktaRateLimitGate{mu: sync.Mutex{}, reset: time.Time{}}
	after := ""
	truncated := false
	seenCursors := make(map[string]struct{})
	for {
		if after != "" {
			if _, seen := seenCursors[after]; seen {
				return nil, oops.E(oops.CodeGatewayError, nil, "Okta application pagination returned a repeated cursor").LogError(ctx, logger)
			}
			seenCursors[after] = struct{}{}
		}
		if err := rateLimits.Wait(requestCtx); err != nil {
			return nil, oops.E(oops.CodeGatewayError, err, "timeout reading Okta applications").LogError(ctx, logger)
		}
		page, err := s.okta.ListApplications(requestCtx, tenantDomain, token.AccessToken, okta.PageRequest{Limit: oktaCollectionPageLimit, After: after})
		if err != nil {
			return nil, oops.E(oops.CodeGatewayError, err, "error reading Okta applications").LogError(ctx, logger)
		}
		rateLimits.Observe(page.RateLimit)
		for i, raw := range page.Items {
			if len(applications) == applicationInventoryLimit {
				truncated = true
				break
			}
			application, err := okta.DecodeApplication(raw)
			if err != nil {
				return nil, oops.E(oops.CodeGatewayError, err, "error decoding an Okta application").LogError(ctx, logger)
			}
			if application.ID == "" || application.Label == "" {
				return nil, oops.E(oops.CodeGatewayError, nil, "Okta returned an incomplete application").LogError(ctx, logger)
			}
			applications = append(applications, &gen.IdentityProviderApplication{
				SourceApplicationID:   application.ID,
				Label:                 application.Label,
				ProviderStatus:        conv.PtrEmpty(application.Status),
				SignOnURL:             conv.PtrEmpty(application.SignOnURL),
				LogoURL:               conv.PtrEmpty(application.LogoURL),
				GroupAssignmentCount:  nil,
				AssignedGroups:        nil,
				AssignedGroupOverflow: nil,
				UserAssignmentCount:   nil,
			})
			if len(applications) == applicationInventoryLimit && (i+1 < len(page.Items) || page.NextCursor != "") {
				truncated = true
				break
			}
		}
		if truncated || page.NextCursor == "" {
			break
		}
		after = page.NextCursor
	}

	detail := "Application inventory read directly from Okta."
	if truncated {
		detail = "Application inventory was capped at 500 applications."
	}
	if len(applications) > applicationAssignmentCountLimit {
		detail += " Assignment counts were omitted because the tenant has more than 50 applications. Assigned group names were also omitted."
	} else if len(applications) > 0 {
		partial := &atomic.Bool{}
		groupLookupSlots := make(chan struct{}, applicationAssignmentConcurrency)
		group, groupCtx := errgroup.WithContext(requestCtx)
		group.SetLimit(applicationAssignmentConcurrency)
		for _, application := range applications {
			group.Go(func() error {
				groupCount, assignedGroups, err := s.readOktaApplicationGroups(groupCtx, rateLimits, tenantDomain, token.AccessToken, application.SourceApplicationID)
				if err != nil {
					partial.Store(true)
				} else {
					application.GroupAssignmentCount = &groupCount
					application.AssignedGroupOverflow = new(max(0, groupCount-len(assignedGroups)))
					assignedGroups, err = s.resolveOktaApplicationGroupNames(groupCtx, rateLimits, groupLookupSlots, tenantDomain, token.AccessToken, assignedGroups)
					if err != nil {
						partial.Store(true)
					} else {
						application.AssignedGroups = make([]*gen.IdentityProviderAssignedGroup, len(assignedGroups))
						for i, assignedGroup := range assignedGroups {
							application.AssignedGroups[i] = &gen.IdentityProviderAssignedGroup{
								SourceGroupID: assignedGroup.ID,
								Name:          assignedGroup.Name,
							}
						}
					}
				}

				userCount, err := countOktaCollection(groupCtx, rateLimits, func(ctx context.Context, page okta.PageRequest) (okta.Page, error) {
					return s.okta.ListApplicationUsers(ctx, tenantDomain, token.AccessToken, application.SourceApplicationID, page)
				}, countDirectOktaApplicationUsers)
				if err != nil {
					partial.Store(true)
				} else {
					application.UserAssignmentCount = &userCount
				}
				return nil
			})
		}
		_ = group.Wait()
		if partial.Load() {
			detail += " Assignment counts are partial because one or more assignment reads failed or timed out."
		} else {
			detail += " Assignment counts were read for every application. Assigned group names were read for up to 10 groups per application."
		}
	}

	return &gen.ListIdentityProviderApplicationsResult{
		Applications:     applications,
		ReadAt:           time.Now().UTC().Format(time.RFC3339Nano),
		ApplicationCount: len(applications),
		Truncated:        truncated,
		Detail:           detail,
	}, nil
}

func (s *Service) readOktaApplicationGroups(
	ctx context.Context,
	rateLimits *oktaRateLimitGate,
	tenantDomain string,
	accessToken string,
	applicationID string,
) (int, []okta.Group, error) {
	count := 0
	after := ""
	pageLimit := applicationAssignedGroupPageLimit
	assignedGroups := make([]okta.Group, 0, applicationAssignedGroupLimit)
	seenCursors := make(map[string]struct{})
	for {
		if after != "" {
			if _, seen := seenCursors[after]; seen {
				return 0, nil, errors.New("okta application group pagination returned a repeated cursor")
			}
			seenCursors[after] = struct{}{}
		}
		if err := rateLimits.Wait(ctx); err != nil {
			return 0, nil, fmt.Errorf("wait to list Okta application groups: %w", err)
		}
		page, err := s.okta.ListApplicationGroups(ctx, tenantDomain, accessToken, applicationID, okta.PageRequest{Limit: pageLimit, After: after})
		if err != nil {
			return 0, nil, fmt.Errorf("list Okta application groups: %w", err)
		}
		rateLimits.Observe(page.RateLimit)
		count += len(page.Items)
		for _, raw := range page.Items {
			if len(assignedGroups) == applicationAssignedGroupLimit {
				break
			}
			assignedGroup, err := okta.DecodeApplicationGroup(raw)
			if err != nil {
				return 0, nil, fmt.Errorf("decode Okta application group assignment: %w", err)
			}
			assignedGroups = append(assignedGroups, assignedGroup)
		}
		if page.NextCursor == "" {
			break
		}
		after = page.NextCursor
		pageLimit = oktaCollectionPageLimit
	}

	return count, assignedGroups, nil
}

func (s *Service) resolveOktaApplicationGroupNames(
	ctx context.Context,
	rateLimits *oktaRateLimitGate,
	groupLookupSlots chan struct{},
	tenantDomain string,
	accessToken string,
	assignedGroups []okta.Group,
) ([]okta.Group, error) {
	lookupGroup, lookupCtx := errgroup.WithContext(ctx)
	for i := range assignedGroups {
		if assignedGroups[i].Name != "" {
			continue
		}
		lookupGroup.Go(func() error {
			select {
			case groupLookupSlots <- struct{}{}:
				defer func() { <-groupLookupSlots }()
			case <-lookupCtx.Done():
				return fmt.Errorf("wait to read Okta group: %w", lookupCtx.Err())
			}
			if err := rateLimits.Wait(lookupCtx); err != nil {
				return fmt.Errorf("wait to read Okta group: %w", err)
			}
			group, err := s.okta.GetGroup(lookupCtx, tenantDomain, accessToken, assignedGroups[i].ID)
			if err != nil {
				return fmt.Errorf("read Okta group: %w", err)
			}
			assignedGroups[i] = group
			return nil
		})
	}
	if err := lookupGroup.Wait(); err != nil {
		return nil, fmt.Errorf("resolve Okta application group names: %w", err)
	}
	return assignedGroups, nil
}

func countOktaCollection(
	ctx context.Context,
	rateLimits *oktaRateLimitGate,
	list func(context.Context, okta.PageRequest) (okta.Page, error),
	countPage func(okta.Page) (int, error),
) (int, error) {
	count := 0
	after := ""
	seenCursors := make(map[string]struct{})
	for {
		if after != "" {
			if _, seen := seenCursors[after]; seen {
				return 0, errors.New("okta assignment pagination returned a repeated cursor")
			}
			seenCursors[after] = struct{}{}
		}
		if err := rateLimits.Wait(ctx); err != nil {
			return 0, err
		}
		page, err := list(ctx, okta.PageRequest{Limit: oktaCollectionPageLimit, After: after})
		if err != nil {
			return 0, err
		}
		rateLimits.Observe(page.RateLimit)
		pageCount, err := countPage(page)
		if err != nil {
			return 0, err
		}
		count += pageCount
		if page.NextCursor == "" {
			return count, nil
		}
		after = page.NextCursor
	}
}

func countDirectOktaApplicationUsers(page okta.Page) (int, error) {
	count := 0
	for _, raw := range page.Items {
		direct, err := okta.IsDirectApplicationUser(raw)
		if err != nil {
			return 0, fmt.Errorf("count direct Okta application users: %w", err)
		}
		if direct {
			count++
		}
	}
	return count, nil
}

type oktaRateLimitGate struct {
	mu    sync.Mutex
	reset time.Time
}

func (g *oktaRateLimitGate) Observe(rateLimit okta.RateLimit) {
	if rateLimit.Remaining == nil || rateLimit.Reset == nil || *rateLimit.Remaining > oktaRateLimitRemainingSleepThreshold {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if rateLimit.Reset.After(g.reset) {
		g.reset = *rateLimit.Reset
	}
}

func (g *oktaRateLimitGate) Wait(ctx context.Context) error {
	g.mu.Lock()
	reset := g.reset
	g.mu.Unlock()
	delay := time.Until(reset)
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("wait for Okta rate limit reset: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}
