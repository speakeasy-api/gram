package changelog

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const (
	defaultLimit = 10
	maxLimit     = 25
)

// GetChangelog surfaces recent Speakeasy platform release notes to the
// managed assistant so it can answer "what's new / what changed" questions.
type GetChangelog struct {
	client *Client
}

type getChangelogInput struct {
	Limit          int    `json:"limit,omitempty" jsonschema:"Maximum number of releases to return. Defaults to 10, capped at 25."`
	Product        string `json:"product,omitempty" jsonschema:"Filter releases by product area. Omit to include all areas."`
	IncludeDetails bool   `json:"include_details,omitempty" jsonschema:"Include the full itemized release notes per entry. Defaults to false (version, date, title, and summary only) to keep responses small."`
}

type getChangelogResult struct {
	Entries   []Entry `json:"entries"`
	SourceURL string  `json:"source_url"`
}

// NewGetChangelogTool builds the changelog tool over httpClient. The client
// must route through a guardian policy so outbound egress stays restricted.
func NewGetChangelogTool(httpClient *guardian.HTTPClient) *GetChangelog {
	return &GetChangelog{client: NewClient(httpClient, DefaultFeedURL)}
}

// NewGetChangelogToolWithURL is like NewGetChangelogTool but reads the
// changelog feed from feedURL. Exposed for tests.
func NewGetChangelogToolWithURL(httpClient *guardian.HTTPClient, feedURL string) *GetChangelog {
	return &GetChangelog{client: NewClient(httpClient, feedURL)}
}

func (s *GetChangelog) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "changelog",
		HandlerName: "get_changelog",
		Name:        "platform_get_changelog",
		Description: "List recent Speakeasy platform release notes from the public changelog (speakeasy.com/changelog). Use this to answer questions about what is new or what recently changed on the platform or dashboard. Each entry has a version, release date, product area, title, prose summary, and permalink; set include_details to true to also get the itemized feature/fix notes.",
		InputSchema: core.BuildInputSchema[getChangelogInput](
			core.WithPropertyEnum("product", "platform", "dashboard"),
			core.WithPropertyNumberRange("limit", 1, maxLimit),
		),
		Variables:   nil,
		Annotations: core.ReadOnlyAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (s *GetChangelog) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	input := getChangelogInput{Limit: 0, Product: "", IncludeDetails: false}
	if err := core.DecodeInput(payload, &input); err != nil {
		return err
	}

	limit := input.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}

	entries, err := s.client.Entries(ctx)
	if err != nil {
		return fmt.Errorf("load changelog entries: %w", err)
	}

	product := strings.ToLower(strings.TrimSpace(input.Product))
	if product != "" && !supportedProducts[product] {
		return fmt.Errorf("unsupported product %q: must be one of platform, dashboard", product)
	}

	out := make([]Entry, 0, limit)
	for _, entry := range entries {
		if product != "" && entry.Product != product {
			continue
		}
		if !input.IncludeDetails {
			entry.Details = ""
		}
		out = append(out, entry)
		if len(out) == limit {
			break
		}
	}

	return core.EncodeResult(wr, getChangelogResult{
		Entries:   out,
		SourceURL: s.client.feedURL,
	})
}
