package slack

import (
	"context"
	"io"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const toolNameSearchUsers = "platform_slack_search_users"

type searchUsersInput struct {
	Query         *string `json:"query,omitempty" jsonschema:"Optional case-insensitive substring filter applied to user name, display name, real name, and email."`
	Cursor        *string `json:"cursor,omitempty" jsonschema:"Pagination cursor from a previous response."`
	Limit         *int    `json:"limit,omitempty" jsonschema:"Maximum number of users to fetch per page. Slack allows up to 1000."`
	IncludeLocale *bool   `json:"include_locale,omitempty" jsonschema:"Include locale in the returned user data when available."`
}

func NewSearchUsersTool(httpClient *guardian.HTTPClient) core.PlatformToolExecutor {
	readOnly := true
	destructive := false
	idempotent := true
	openWorld := true

	return &slackTool{
		descriptor: core.ToolDescriptor{
			SourceSlug:  sourceSlack,
			HandlerName: "search_users",
			Name:        toolNameSearchUsers,
			Description: "List Slack workspace users via users.list using the server's bot or user token. Optionally filters results client-side by a name substring.",
			InputSchema: core.BuildInputSchema[searchUsersInput](
				core.WithPropertyNumberRange("limit", 1, 1000),
			),
			Variables:   nil,
			Annotations: slackToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
		client: newAPIClient(defaultSlackAPIBaseURL, httpClient),
		callFn: callSearchUsers,
	}
}

func callSearchUsers(ctx context.Context, client *apiClient, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	var input searchUsersInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	request := map[string]any{}
	setOptionalString(request, "cursor", input.Cursor)
	setOptionalInt(request, "limit", input.Limit)
	setOptionalBool(request, "include_locale", input.IncludeLocale)

	body, err := client.call(ctx, "users.list", request, tokenPreferBot, env)
	if err != nil {
		return err
	}

	filtered, err := filterListResponse(body, "members", userMatchesQuery(input.Query))
	if err != nil {
		return err
	}
	return writeResponse(wr, filtered)
}

func userMatchesQuery(query *string) func(map[string]any) bool {
	needle := strings.ToLower(strings.TrimSpace(derefString(query)))
	if needle == "" {
		return nil
	}
	return func(entry map[string]any) bool {
		if stringFieldContains(entry, needle, "name", "real_name") {
			return true
		}
		profile, ok := entry["profile"].(map[string]any)
		if !ok {
			return false
		}
		return stringFieldContains(profile, needle, "display_name", "display_name_normalized", "real_name", "real_name_normalized", "email")
	}
}
