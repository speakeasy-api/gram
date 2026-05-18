package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"

	"go.opentelemetry.io/otel/trace/noop"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

const defaultWorkOSEndpoint = "https://api.workos.com"

type options struct {
	workosAPIKey   string
	workosEndpoint string
	workosOrgIDs   []string
}

type stringList []string

func (s *stringList) String() string {
	return strings.Join(*s, ",")
}

func (s *stringList) Set(value string) error {
	for part := range strings.SplitSeq(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			*s = append(*s, part)
		}
	}
	return nil
}

type rawRoleListResponse struct {
	Data         []workos.Role `json:"data"`
	ListMetadata struct {
		After string `json:"after"`
	} `json:"list_metadata"`
}

func main() {
	ctx := context.Background()
	if err := run(ctx, parseFlags()); err != nil {
		fmt.Fprintf(os.Stderr, "workos-role-dump: %v\n", err)
		os.Exit(1)
	}
}

func parseFlags() options {
	opts := options{
		workosAPIKey:   strings.TrimSpace(firstNonEmpty(os.Getenv("WORKOS_API_KEY"), os.Getenv("WORK_OS_SECRET_KEY"))),
		workosEndpoint: strings.TrimSpace(firstNonEmpty(os.Getenv("WORKOS_API_URL"), defaultWorkOSEndpoint)),
		workosOrgIDs:   nil,
	}

	var orgIDs stringList
	flag.StringVar(&opts.workosAPIKey, "workos-api-key", opts.workosAPIKey, "WorkOS API key (defaults to WORKOS_API_KEY or WORK_OS_SECRET_KEY)")
	flag.StringVar(&opts.workosEndpoint, "workos-endpoint", opts.workosEndpoint, "WorkOS API endpoint override (defaults to WORKOS_API_URL or api.workos.com)")
	flag.Var(&orgIDs, "workos-org-id", "WorkOS organization id to inspect; repeat or comma-separate")
	flag.Parse()

	opts.workosOrgIDs = orgIDs
	must(validateOptions(opts))
	return opts
}

func validateOptions(opts options) error {
	if opts.workosAPIKey == "" {
		return errors.New("--workos-api-key, WORKOS_API_KEY, or WORK_OS_SECRET_KEY is required")
	}
	if opts.workosEndpoint == "" {
		return errors.New("--workos-endpoint or WORKOS_API_URL must be non-empty")
	}
	if len(opts.workosOrgIDs) == 0 {
		return errors.New("--workos-org-id is required")
	}
	return nil
}

func run(ctx context.Context, opts options) error {
	policy := guardian.NewDefaultPolicy(noop.NewTracerProvider())
	sdkClient := workos.NewClient(policy, opts.workosAPIKey, workos.ClientOpts{
		Endpoint:   opts.workosEndpoint,
		HTTPClient: nil,
	})
	httpClient := policy.PooledClient()

	for _, orgID := range opts.workosOrgIDs {
		fmt.Printf("WorkOS organization %s\n", orgID)

		sdkRoles, err := sdkClient.ListRoles(ctx, orgID)
		if err != nil {
			return fmt.Errorf("list roles through SDK wrapper for %s: %w", orgID, err)
		}
		rawRoles, err := listRawAuthorizationRoles(ctx, httpClient, opts, orgID)
		if err != nil {
			return fmt.Errorf("list raw authorization roles for %s: %w", orgID, err)
		}

		printRoleList("  sdk ListOrganizationRoles", sdkRoles)
		printRoleList("  raw /authorization/organizations/{orgID}/roles", rawRoles)
		printRoleDiff(sdkRoles, rawRoles)
	}
	return nil
}

func listRawAuthorizationRoles(ctx context.Context, httpClient *guardian.HTTPClient, opts options, orgID string) ([]workos.Role, error) {
	roles := make([]workos.Role, 0)
	var after string
	for {
		reqURL, err := rawRolesURL(opts.workosEndpoint, orgID, after)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return nil, fmt.Errorf("create raw roles request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+opts.workosAPIKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("send raw roles request: %w", err)
		}
		body, readErr := io.ReadAll(resp.Body)
		if closeErr := resp.Body.Close(); closeErr != nil && readErr == nil {
			readErr = closeErr
		}
		if readErr != nil {
			return nil, fmt.Errorf("read raw roles response: %w", readErr)
		}
		if resp.StatusCode >= http.StatusBadRequest {
			return nil, fmt.Errorf("raw roles request failed status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
		}

		var parsed rawRoleListResponse
		if err := json.Unmarshal(body, &parsed); err != nil {
			return nil, fmt.Errorf("decode raw roles response: %w", err)
		}
		roles = append(roles, parsed.Data...)
		if parsed.ListMetadata.After == "" {
			return roles, nil
		}
		after = parsed.ListMetadata.After
	}
}

func rawRolesURL(endpoint, orgID, after string) (string, error) {
	base, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("parse WorkOS endpoint: %w", err)
	}
	path, err := url.JoinPath(base.Path, "/authorization/organizations", orgID, "roles")
	if err != nil {
		return "", fmt.Errorf("build raw roles path: %w", err)
	}
	base.Path = path
	q := base.Query()
	q.Set("limit", "100")
	if after != "" {
		q.Set("after", after)
	}
	base.RawQuery = q.Encode()
	return base.String(), nil
}

func printRoleList(title string, roles []workos.Role) {
	roles = sortedRoles(roles)
	fmt.Printf("%s count=%d\n", title, len(roles))
	for _, role := range roles {
		fmt.Printf("    slug=%q type=%q name=%q updated_at=%q\n", role.Slug, role.Type, role.Name, role.UpdatedAt)
	}
}

func printRoleDiff(sdkRoles []workos.Role, rawRoles []workos.Role) {
	sdkSet := roleSlugSet(sdkRoles)
	rawSet := roleSlugSet(rawRoles)
	missingFromSDK := difference(rawSet, sdkSet)
	missingFromRaw := difference(sdkSet, rawSet)
	fmt.Println("  diff")
	fmt.Printf("    raw_missing_from_sdk=%v\n", missingFromSDK)
	fmt.Printf("    sdk_missing_from_raw=%v\n", missingFromRaw)
}

func sortedRoles(roles []workos.Role) []workos.Role {
	out := append([]workos.Role(nil), roles...)
	sort.Slice(out, func(i, j int) bool {
		return out[i].Slug < out[j].Slug
	})
	return out
}

func roleSlugSet(roles []workos.Role) map[string]bool {
	out := make(map[string]bool, len(roles))
	for _, role := range roles {
		out[role.Slug] = true
	}
	return out
}

func difference(left map[string]bool, right map[string]bool) []string {
	out := make([]string, 0)
	for value := range left {
		if !right[value] {
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func must(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "workos-role-dump: %v\n", err)
		os.Exit(2)
	}
}
