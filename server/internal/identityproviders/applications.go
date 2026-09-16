package identityproviders

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"

	gen "github.com/speakeasy-api/gram/server/gen/identity_providers"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/identityproviders/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/okta"
)

const (
	applicationInventoryLimit            = 500
	applicationAssignmentCountLimit      = 50
	applicationAssignmentConcurrency     = 3
	applicationAssignmentTimeout         = 35 * time.Second
	applicationAssignedGroupLimit        = 10
	applicationAssignedGroupPageLimit    = 20
	applicationCatalogMatchLimit         = 50
	applicationCatalogMatchConcurrency   = 4
	applicationCatalogMatchCacheTTL      = 10 * time.Minute
	applicationInventoryCacheTTL         = 5 * time.Minute
	applicationInventoryTimeout          = 120 * time.Second
	oktaCollectionPageLimit              = 200
	oktaRateLimitRemainingSleepThreshold = 5
	oktaRateLimitRetryMaxWait            = 30 * time.Second
)

func (s *Service) ListApplications(ctx context.Context, payload *gen.ListApplicationsPayload) (*gen.ListIdentityProviderApplicationsResult, error) {
	authCtx, logger, err := s.requireAccess(ctx, authz.ScopeOrgRead)
	if err != nil {
		return nil, err
	}

	requestCtx, cancel := context.WithTimeout(ctx, applicationInventoryTimeout)
	defer cancel()

	force := payload.Force != nil && *payload.Force
	return s.applicationInventory.load(authCtx.ActiveOrganizationID, force, func() (*gen.ListIdentityProviderApplicationsResult, error) {
		return s.listApplications(requestCtx, authCtx.ActiveOrganizationID, logger)
	})
}

func (s *Service) listApplications(ctx context.Context, organizationID string, logger *slog.Logger) (*gen.ListIdentityProviderApplicationsResult, error) {
	connection, err := repo.New(s.db).GetIdentityProviderConnectionByOrganization(ctx, organizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, nil, "identity provider connection not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error loading identity provider connection").LogError(ctx, logger)
	}
	token, err := s.acquireOktaManagementToken(ctx, organizationID, connection, []string{"okta.apps.read", "okta.groups.read"})
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
		page, err := readOktaInventoryWithRetry(ctx, logger, rateLimits, "applications", "", func() (okta.Page, okta.RateLimit, error) {
			page, err := s.okta.ListApplicationsOnce(ctx, tenantDomain, token.AccessToken, okta.PageRequest{Limit: oktaCollectionPageLimit, After: after})
			if err != nil {
				return page, page.RateLimit, fmt.Errorf("list Okta applications once: %w", err)
			}
			return page, page.RateLimit, nil
		})
		if err != nil {
			warnOktaInventoryReadFailure(ctx, logger, "applications", "", err)
			return nil, oops.E(oops.CodeGatewayError, err, "error reading Okta applications").LogError(ctx, logger)
		}
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
				SourceApplicationID:       application.ID,
				Label:                     application.Label,
				ProviderStatus:            conv.PtrEmpty(application.Status),
				SignOnURL:                 conv.PtrEmpty(application.SignOnURL),
				LogoURL:                   conv.PtrEmpty(application.LogoURL),
				GroupAssignmentCount:      nil,
				AssignedGroups:            nil,
				AssignedGroupOverflow:     nil,
				UserAssignmentCount:       nil,
				DirectUserAssignmentCount: nil,
				Match:                     nil,
				Pickable:                  false,
				UnpickableReason:          nil,
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
	var matchResults []applicationMatchResult
	var matchFailed bool
	matchDone := make(chan struct{})
	go func() {
		matchResults, matchFailed = s.appMatches.match(ctx, organizationID, applications, s.catalog)
		close(matchDone)
	}()
	if len(applications) > applicationAssignmentCountLimit {
		detail += " Assignment counts were omitted because the tenant has more than 50 applications. Assigned group names were also omitted."
	} else if len(applications) > 0 {
		partial := &atomic.Bool{}
		groupLookupSlots := make(chan struct{}, applicationAssignmentConcurrency)
		group, groupCtx := errgroup.WithContext(ctx)
		group.SetLimit(applicationAssignmentConcurrency)
		for _, application := range applications {
			group.Go(func() error {
				assignmentCtx, cancel := context.WithTimeout(groupCtx, applicationAssignmentTimeout)
				defer cancel()

				groupCount, assignedGroups, err := s.readOktaApplicationGroups(assignmentCtx, logger, rateLimits, tenantDomain, token.AccessToken, application.SourceApplicationID)
				if err != nil {
					warnOktaInventoryReadFailure(assignmentCtx, logger, "application_groups", application.SourceApplicationID, err)
					partial.Store(true)
				} else {
					application.GroupAssignmentCount = &groupCount
					application.AssignedGroupOverflow = new(max(0, groupCount-len(assignedGroups)))
					assignedGroups, err = s.resolveOktaApplicationGroupNames(assignmentCtx, logger, rateLimits, groupLookupSlots, tenantDomain, token.AccessToken, application.SourceApplicationID, assignedGroups)
					if err != nil {
						warnOktaInventoryReadFailure(assignmentCtx, logger, "groups", application.SourceApplicationID, err)
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

				userCount, directUserCount, err := countOktaApplicationUsers(assignmentCtx, logger, rateLimits, application.SourceApplicationID, func(ctx context.Context, page okta.PageRequest) (okta.Page, error) {
					result, err := s.okta.ListApplicationUsersOnce(ctx, tenantDomain, token.AccessToken, application.SourceApplicationID, page)
					if err != nil {
						return okta.Page{}, fmt.Errorf("list Okta application users once: %w", err)
					}
					return result, nil
				})
				if err != nil {
					warnOktaInventoryReadFailure(assignmentCtx, logger, "application_users", application.SourceApplicationID, err)
					partial.Store(true)
				} else {
					application.UserAssignmentCount = &userCount
					application.DirectUserAssignmentCount = &directUserCount
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

	<-matchDone
	for i, application := range applications {
		if i < len(matchResults) && matchResults[i].match != nil {
			match := matchResults[i].match
			application.Match = &gen.IdentityProviderApplicationMatch{
				ProviderKey: match.providerKey,
				CatalogRef:  match.catalogRef,
				Name:        match.name,
				RemoteURL:   match.remoteURL,
				Basis:       "name",
				Confidence:  match.confidence,
			}
		}
		application.Pickable = strings.EqualFold(conv.PtrValOr(application.ProviderStatus, ""), "ACTIVE") && application.Match != nil && application.Match.RemoteURL != ""
		if !application.Pickable {
			if !strings.EqualFold(conv.PtrValOr(application.ProviderStatus, ""), "ACTIVE") {
				application.UnpickableReason = new("inactive")
			} else {
				application.UnpickableReason = new("no_match")
			}
		}
	}
	if len(applications) > applicationCatalogMatchLimit {
		detail += " Speakeasy MCP server matching was capped at 50 applications."
	}
	if matchFailed {
		detail += " Speakeasy MCP server matching is partial because one or more catalogue reads failed."
	}
	sort.SliceStable(applications, func(i, j int) bool {
		if applications[i].Pickable != applications[j].Pickable {
			return applications[i].Pickable
		}
		return strings.ToLower(applications[i].Label) < strings.ToLower(applications[j].Label)
	})

	return &gen.ListIdentityProviderApplicationsResult{
		Applications:     applications,
		ReadAt:           time.Now().UTC().Format(time.RFC3339Nano),
		ApplicationCount: len(applications),
		Truncated:        truncated,
		Detail:           detail,
	}, nil
}

type applicationCatalogMatch struct {
	providerKey string
	catalogRef  string
	name        string
	remoteURL   string
	confidence  string
}

type applicationInventoryCacheEntry struct {
	expiresAt time.Time
	result    *gen.ListIdentityProviderApplicationsResult
}

type applicationInventoryCache struct {
	mu      sync.Mutex
	entries map[string]applicationInventoryCacheEntry
	group   singleflight.Group
}

func newApplicationInventoryCache() *applicationInventoryCache {
	return &applicationInventoryCache{
		mu:      sync.Mutex{},
		entries: make(map[string]applicationInventoryCacheEntry),
		group:   singleflight.Group{},
	}
}

func (c *applicationInventoryCache) load(organizationID string, force bool, read func() (*gen.ListIdentityProviderApplicationsResult, error)) (*gen.ListIdentityProviderApplicationsResult, error) {
	if !force {
		c.mu.Lock()
		entry, found := c.entries[organizationID]
		c.mu.Unlock()
		if found && time.Now().Before(entry.expiresAt) {
			return entry.result, nil
		}
	}

	value, err, _ := c.group.Do(organizationID, func() (any, error) {
		if !force {
			c.mu.Lock()
			entry, found := c.entries[organizationID]
			c.mu.Unlock()
			if found && time.Now().Before(entry.expiresAt) {
				return entry.result, nil
			}
		}

		result, err := read()
		if err != nil {
			return nil, err
		}
		now := time.Now()
		c.mu.Lock()
		for key, cached := range c.entries {
			if now.After(cached.expiresAt) {
				delete(c.entries, key)
			}
		}
		c.entries[organizationID] = applicationInventoryCacheEntry{
			expiresAt: now.Add(applicationInventoryCacheTTL),
			result:    result,
		}
		c.mu.Unlock()
		return result, nil
	})
	if err != nil {
		return nil, err
	}
	result, ok := value.(*gen.ListIdentityProviderApplicationsResult)
	if !ok {
		return nil, oops.E(oops.CodeUnexpected, nil, "error reading the cached application inventory")
	}
	return result, nil
}

type applicationMatchResult struct {
	match  *applicationCatalogMatch
	failed bool
}

type applicationMatchCacheEntry struct {
	fingerprint string
	expiresAt   time.Time
	results     []applicationMatchResult
	failed      bool
}

type applicationMatchCache struct {
	mu      sync.Mutex
	entries map[string]applicationMatchCacheEntry
	group   singleflight.Group
}

func newApplicationMatchCache() *applicationMatchCache {
	return &applicationMatchCache{
		mu:      sync.Mutex{},
		entries: make(map[string]applicationMatchCacheEntry),
		group:   singleflight.Group{},
	}
}

func (c *applicationMatchCache) match(ctx context.Context, organizationID string, applications []*gen.IdentityProviderApplication, catalog ApplicationCatalog) ([]applicationMatchResult, bool) {
	matchCount := min(len(applications), applicationCatalogMatchLimit)
	var fingerprintBuilder strings.Builder
	for _, application := range applications[:matchCount] {
		fingerprintBuilder.WriteString(application.SourceApplicationID)
		fingerprintBuilder.WriteByte(0)
		fingerprintBuilder.WriteString(application.Label)
		fingerprintBuilder.WriteByte(0)
	}
	fingerprint := fingerprintBuilder.String()
	now := time.Now()

	c.mu.Lock()
	entry, found := c.entries[organizationID]
	c.mu.Unlock()
	if found && entry.fingerprint == fingerprint && now.Before(entry.expiresAt) {
		return entry.results, entry.failed
	}

	value, _, _ := c.group.Do(organizationID+"\x00"+fingerprint, func() (any, error) {
		c.mu.Lock()
		entry, found := c.entries[organizationID]
		c.mu.Unlock()
		if found && entry.fingerprint == fingerprint && time.Now().Before(entry.expiresAt) {
			return entry, nil
		}

		results := make([]applicationMatchResult, matchCount)
		group, groupCtx := errgroup.WithContext(ctx)
		group.SetLimit(applicationCatalogMatchConcurrency)
		for i, application := range applications[:matchCount] {
			group.Go(func() error {
				results[i] = matchApplicationToCatalog(groupCtx, application.Label, catalog)
				return nil
			})
		}
		_ = group.Wait()
		failed := false
		for _, result := range results {
			failed = failed || result.failed
		}
		entry = applicationMatchCacheEntry{
			fingerprint: fingerprint,
			expiresAt:   time.Now().Add(applicationCatalogMatchCacheTTL),
			results:     results,
			failed:      failed,
		}
		c.mu.Lock()
		for key, cached := range c.entries {
			if time.Now().After(cached.expiresAt) {
				delete(c.entries, key)
			}
		}
		c.entries[organizationID] = entry
		c.mu.Unlock()
		return entry, nil
	})

	entry, ok := value.(applicationMatchCacheEntry)
	if !ok {
		return nil, true
	}
	return entry.results, entry.failed
}

func matchApplicationToCatalog(ctx context.Context, label string, catalog ApplicationCatalog) applicationMatchResult {
	candidates, err := catalog.Search(ctx, label)
	if err != nil {
		return applicationMatchResult{match: nil, failed: true}
	}
	normalizedLabel := normalizeApplicationCatalogName(label, false)
	if normalizedLabel == "" {
		return applicationMatchResult{match: nil, failed: false}
	}

	var selected *ApplicationCatalogCandidate
	confidence := ""
	for i := range candidates {
		if normalizeApplicationCatalogName(candidates[i].Name, false) == normalizedLabel {
			selected = &candidates[i]
			confidence = "exact"
			break
		}
	}
	if selected == nil {
		genericLabel := normalizeApplicationCatalogName(label, true)
		for i := range candidates {
			if genericLabel != "" && normalizeApplicationCatalogName(candidates[i].Name, true) == genericLabel {
				if selected != nil {
					selected = nil
					break
				}
				selected = &candidates[i]
			}
		}
		if selected != nil {
			confidence = "likely"
		}
	}
	if selected == nil && len(candidates) == 1 {
		normalizedCandidate := normalizeApplicationCatalogName(candidates[0].Name, true)
		normalizedLabel = normalizeApplicationCatalogName(label, true)
		if normalizedCandidate != "" && (strings.Contains(normalizedLabel, normalizedCandidate) || strings.Contains(normalizedCandidate, normalizedLabel)) {
			selected = &candidates[0]
			confidence = "likely"
		}
	}
	if selected == nil {
		return applicationMatchResult{match: nil, failed: false}
	}

	details, err := catalog.Inspect(ctx, selected.ProviderKey, selected.CatalogRef)
	if err != nil {
		return applicationMatchResult{match: nil, failed: true}
	}
	if details.RemoteURL == "" {
		return applicationMatchResult{match: nil, failed: false}
	}
	return applicationMatchResult{
		match: &applicationCatalogMatch{
			providerKey: details.ProviderKey,
			catalogRef:  details.CatalogRef,
			name:        details.Name,
			remoteURL:   details.RemoteURL,
			confidence:  confidence,
		},
		failed: false,
	}
}

func normalizeApplicationCatalogName(name string, trimTrailingGeneric bool) string {
	words := make([]string, 0, 4)
	var word strings.Builder
	flush := func() {
		if word.Len() == 0 {
			return
		}
		value := word.String()
		word.Reset()
		switch value {
		case "app", "inc", "sso":
			return
		default:
			words = append(words, value)
		}
	}
	for _, char := range strings.ToLower(name) {
		if unicode.IsLetter(char) || unicode.IsNumber(char) {
			word.WriteRune(char)
		} else {
			flush()
		}
	}
	flush()
	if trimTrailingGeneric {
		for len(words) > 0 {
			switch words[len(words)-1] {
			case "cloud", "online", "enterprise", "workspace", "business":
				words = words[:len(words)-1]
			default:
				return strings.Join(words, "")
			}
		}
	}
	return strings.Join(words, "")
}

func (s *Service) readOktaApplicationGroups(
	ctx context.Context,
	logger *slog.Logger,
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
		page, err := readOktaInventoryWithRetry(ctx, logger, rateLimits, "application_groups", applicationID, func() (okta.Page, okta.RateLimit, error) {
			page, err := s.okta.ListApplicationGroupsOnce(ctx, tenantDomain, accessToken, applicationID, okta.PageRequest{Limit: pageLimit, After: after})
			if err != nil {
				return page, page.RateLimit, fmt.Errorf("list Okta application groups once: %w", err)
			}
			return page, page.RateLimit, nil
		})
		if err != nil {
			return 0, nil, fmt.Errorf("list Okta application groups: %w", err)
		}
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
	logger *slog.Logger,
	rateLimits *oktaRateLimitGate,
	groupLookupSlots chan struct{},
	tenantDomain string,
	accessToken string,
	applicationID string,
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
			group, err := readOktaInventoryWithRetry(lookupCtx, logger, rateLimits, "groups", applicationID, func() (okta.Group, okta.RateLimit, error) {
				group, rateLimit, err := s.okta.GetGroupOnce(lookupCtx, tenantDomain, accessToken, assignedGroups[i].ID)
				if err != nil {
					return okta.Group{}, rateLimit, fmt.Errorf("get Okta group once: %w", err)
				}
				return group, rateLimit, nil
			})
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

func countOktaApplicationUsers(
	ctx context.Context,
	logger *slog.Logger,
	rateLimits *oktaRateLimitGate,
	applicationID string,
	list func(context.Context, okta.PageRequest) (okta.Page, error),
) (int, int, error) {
	users := make(map[string]struct{})
	directUsers := make(map[string]struct{})
	after := ""
	seenCursors := make(map[string]struct{})
	for {
		if after != "" {
			if _, seen := seenCursors[after]; seen {
				return 0, 0, errors.New("okta assignment pagination returned a repeated cursor")
			}
			seenCursors[after] = struct{}{}
		}
		page, err := readOktaInventoryWithRetry(ctx, logger, rateLimits, "application_users", applicationID, func() (okta.Page, okta.RateLimit, error) {
			page, err := list(ctx, okta.PageRequest{Limit: oktaCollectionPageLimit, After: after})
			return page, page.RateLimit, err
		})
		if err != nil {
			return 0, 0, err
		}
		for _, raw := range page.Items {
			user, err := okta.DecodeApplicationUser(raw)
			if err != nil {
				return 0, 0, fmt.Errorf("count Okta application users: %w", err)
			}
			users[user.ID] = struct{}{}
			if user.Direct {
				directUsers[user.ID] = struct{}{}
			}
		}
		if page.NextCursor == "" {
			return len(users), len(directUsers), nil
		}
		after = page.NextCursor
	}
}

type oktaRateLimitGate struct {
	mu    sync.Mutex
	reset time.Time
}

func (g *oktaRateLimitGate) Observe(rateLimit okta.RateLimit) {
	if rateLimit.Remaining == nil || rateLimit.Reset == nil || *rateLimit.Remaining >= oktaRateLimitRemainingSleepThreshold {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if rateLimit.Reset.After(g.reset) {
		g.reset = *rateLimit.Reset
	}
}

func (g *oktaRateLimitGate) Wait(ctx context.Context, maxWait time.Duration) error {
	g.mu.Lock()
	reset := g.reset
	g.mu.Unlock()
	delay := time.Until(reset)
	if delay <= 0 {
		return nil
	}
	if maxWait > 0 {
		delay = min(delay, maxWait)
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

func readOktaInventoryWithRetry[T any](
	ctx context.Context,
	logger *slog.Logger,
	rateLimits *oktaRateLimitGate,
	operation string,
	applicationID string,
	read func() (T, okta.RateLimit, error),
) (T, error) {
	var zero T
	for attempt := range 2 {
		maxWait := time.Duration(0)
		if attempt > 0 {
			maxWait = oktaRateLimitRetryMaxWait
		}
		if err := rateLimits.Wait(ctx, maxWait); err != nil {
			return zero, err
		}

		value, rateLimit, err := read()
		rateLimits.Observe(rateLimit)
		if err == nil {
			return value, nil
		}

		var apiErr *okta.APIError
		if attempt == 0 && errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusTooManyRequests {
			rateLimits.Observe(apiErr.RateLimit)
			warnOktaInventoryReadFailure(ctx, logger, operation, applicationID, err)
			continue
		}
		return zero, err
	}
	return zero, errors.New("okta inventory read retry exhausted")
}

func warnOktaInventoryReadFailure(ctx context.Context, logger *slog.Logger, operation, applicationID string, err error) {
	statusCode := 0
	errorCode := ""
	rateLimit := okta.RateLimit{Limit: nil, Remaining: nil, Reset: nil}
	var apiErr *okta.APIError
	if errors.As(err, &apiErr) {
		statusCode = apiErr.StatusCode
		errorCode = apiErr.Code
		rateLimit = apiErr.RateLimit
	}

	remaining := ""
	if rateLimit.Remaining != nil {
		remaining = strconv.FormatInt(*rateLimit.Remaining, 10)
	}
	reset := ""
	if rateLimit.Reset != nil {
		reset = strconv.FormatInt(rateLimit.Reset.Unix(), 10)
	}
	logger.WarnContext(ctx, "Okta inventory read failed",
		attr.SlogProvider("okta"),
		attr.SlogOktaInventoryOperation(operation),
		attr.SlogOktaApplicationID(applicationID),
		attr.SlogHTTPResponseStatusCode(statusCode),
		attr.SlogOktaErrorCode(errorCode),
		attr.SlogOktaRateLimitRemaining(remaining),
		attr.SlogOktaRateLimitReset(reset),
		attr.SlogError(err),
	)
}
