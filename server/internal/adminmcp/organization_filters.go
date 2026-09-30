package adminmcp

import (
	"errors"

	"slices"
	"strconv"
	"strings"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/constants"
)

// maxOrganizationSearchPage bounds offset scans to 20,000 results at the maximum page size.
const maxOrganizationSearchPage = 1000

// defaultOrganizationSearchLimit keeps discovery responses small enough to inspect before selecting a target.
const defaultOrganizationSearchLimit = 10

var organizationTrialStates = []string{"running", "ending_soon", "expired", "demoted", "converted", "none"}

var organizationSortColumns = []string{"name", "slug", "account_type", "member_count", "created_at", "disabled_at", "trial_ends_at"}

func organizationSearchPayload(input FindOrganizationsInput) (*gen.ListOrganizationsPayload, error) {
	query := strings.TrimSpace(input.Query)
	if (query != "" && len(query) < 3) || len(query) > 128 || len(input.Cursor) > 128 || input.Limit < 0 || input.Limit > maxOrganizationSearchLimit {
		return nil, errors.New("provide a search query of 3 to 128 characters, a cursor up to 128 characters, and a limit of 1 to 20")
	}
	if query == "" && len(input.AccountTypes) == 0 && len(input.TrialStates) == 0 && input.DisabledStatus == "" && input.MinMembers == nil && input.MaxMembers == nil && input.CreatedFrom == "" && input.CreatedTo == "" {
		return nil, errors.New("provide a search query or an explicit organization filter")
	}
	if len(input.AccountTypes) > len(constants.AccountTypes) || len(input.TrialStates) > len(organizationTrialStates) {
		return nil, errors.New("too many account-type or trial-state filters")
	}
	for _, accountType := range input.AccountTypes {
		if !constants.IsAccountType(accountType) {
			return nil, errors.New("account_types must contain supported account tiers")
		}
	}
	for _, state := range input.TrialStates {
		if !slices.Contains(organizationTrialStates, state) {
			return nil, errors.New("trial_states must contain supported trial states")
		}
	}
	if input.DisabledStatus != "" && !slices.Contains([]string{"all", "active", "disabled"}, input.DisabledStatus) {
		return nil, errors.New("disabled_status must be all, active or disabled")
	}
	if input.Sort != "" && !slices.Contains(organizationSortColumns, input.Sort) {
		return nil, errors.New("sort must be a supported dashboard column")
	}
	if input.Direction != "" && (input.Sort == "" || !slices.Contains([]string{"asc", "desc"}, input.Direction)) {
		return nil, errors.New("direction must be asc or desc and requires sort")
	}
	if input.Page < 0 || input.Page > maxOrganizationSearchPage || (input.Cursor != "" && (input.Sort != "" || input.Page != 0)) {
		return nil, errors.New("page must be 1 to 1000; cursor cannot be combined with sort or page")
	}
	minMembers, err := organizationMemberCount(input.MinMembers)
	if err != nil {
		return nil, err
	}
	maxMembers, err := organizationMemberCount(input.MaxMembers)
	if err != nil {
		return nil, err
	}
	if minMembers != nil && maxMembers != nil && *minMembers > *maxMembers {
		return nil, errors.New("min_members must not exceed max_members")
	}
	for _, date := range []string{input.CreatedFrom, input.CreatedTo} {
		if date == "" {
			continue
		}
		parsed, err := time.Parse(time.DateOnly, date)
		if err != nil || parsed.Format(time.DateOnly) != date {
			return nil, errors.New("creation dates must use valid YYYY-MM-DD UTC dates")
		}
	}
	if input.CreatedFrom != "" && input.CreatedTo != "" && input.CreatedFrom > input.CreatedTo {
		return nil, errors.New("created_from must not be after created_to")
	}
	limit := input.Limit
	if limit == 0 {
		limit = defaultOrganizationSearchLimit
	}
	//nolint:exhaustruct // Only the optional filters supplied by this MCP request are populated.
	payload := &gen.ListOrganizationsPayload{Limit: &limit, AccountTypes: input.AccountTypes, TrialStates: input.TrialStates, MinMembers: minMembers, MaxMembers: maxMembers}
	if query != "" {
		payload.Q = &query
	}
	if input.Cursor != "" {
		payload.Cursor = &input.Cursor
	}
	if input.DisabledStatus != "" {
		payload.DisabledStatus = &input.DisabledStatus
	}
	if input.CreatedFrom != "" {
		payload.CreatedFrom = &input.CreatedFrom
	}
	if input.CreatedTo != "" {
		payload.CreatedTo = &input.CreatedTo
	}
	if input.Sort != "" {
		payload.Sort = &input.Sort
	}
	if input.Direction != "" {
		payload.Direction = &input.Direction
	}
	if input.Sort != "" || input.Page != 0 {
		page := input.Page
		if page == 0 {
			page = 1
		}
		payload.Page = &page
	}
	return payload, nil
}

func organizationMemberCount(value *string) (*int64, error) {
	if value == nil {
		return nil, nil
	}
	if *value == "" || strings.Trim(*value, "0123456789") != "" {
		return nil, errors.New("member counts must be nonnegative decimal integer strings")
	}
	count, err := strconv.ParseInt(*value, 10, 64)
	if err != nil {
		return nil, errors.New("member count is outside the int64 range")
	}
	return &count, nil
}
