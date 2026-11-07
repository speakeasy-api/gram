package remotemcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/sync/singleflight"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/tooldisposition"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

const (
	// toolCatalogRefreshCooldown is the minimum gap between upstream listings for
	// one caller on one server. A caller can invent unlimited tool names, so the
	// bound can't be per name. Long enough to cap that traffic at about one
	// listing a minute; a tool the upstream just added waits at most this long,
	// and any tools/list records it sooner.
	toolCatalogRefreshCooldown = time.Minute

	// toolCatalogRefreshTimeout bounds one upstream listing and catalog write,
	// across every page. It is the time a tools/call can wait on a missing tool.
	toolCatalogRefreshTimeout = 30 * time.Second

	// toolCatalogRefreshStripes is the size of the fixed cooldown table, which
	// keeps memory bounded however many callers and servers there are. A
	// collision only delays a lookup by up to one cooldown.
	toolCatalogRefreshStripes = 1024
)

// toolCatalogRefreshes limits upstream listings triggered by tools/call. One
// instance is shared by every proxy a manager builds, so limits hold per process.
type toolCatalogRefreshes struct {
	flights     singleflight.Group
	lastStarted [toolCatalogRefreshStripes]atomic.Pointer[toolCatalogRefreshClaim]
	now         func() time.Time
}

// toolCatalogRefreshClaim is the refresh that holds a stripe's cooldown.
type toolCatalogRefreshClaim struct {
	// key is the refresh key that started it.
	key string

	// started is when it started, in Unix nanoseconds.
	started int64
}

// toolCatalogRefresh is what run did about a refresh.
type toolCatalogRefresh string

const (
	// toolCatalogListed means the upstream was listed for this call.
	toolCatalogListed toolCatalogRefresh = "listed"

	// toolCatalogListedRecently means this key listed the upstream within the
	// cooldown, so the catalog already holds what that listing returned.
	toolCatalogListedRecently toolCatalogRefresh = "listed_recently"

	// toolCatalogLookupSkipped means another key holds the stripe's cooldown, so
	// the upstream was not asked for this key.
	toolCatalogLookupSkipped toolCatalogRefresh = "lookup_skipped"
)

func newToolCatalogRefreshes() *toolCatalogRefreshes {
	return &toolCatalogRefreshes{
		flights:     singleflight.Group{},
		lastStarted: [toolCatalogRefreshStripes]atomic.Pointer[toolCatalogRefreshClaim]{},
		now:         time.Now,
	}
}

// run calls refresh unless the key's stripe started one within the cooldown,
// sharing one call between concurrent callers, and reports what it did. A
// skipped refresh is not an error: the caller rereads the catalog either way.
// A failed refresh doesn't start the cooldown, so a caller who repairs their
// credential isn't told a tool is missing; sharing still limits a failing
// upstream to one attempt at a time per key. refresh runs detached from ctx's
// cancellation because other callers may be waiting on it.
func (r *toolCatalogRefreshes) run(ctx context.Context, key string, refresh func(context.Context) error) (toolCatalogRefresh, error) {
	results := r.flights.DoChan(key, func() (any, error) {
		stripe := r.stripe(key)
		claimed, outcome := r.claim(stripe, key)
		if claimed == nil {
			return outcome, nil
		}

		refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), toolCatalogRefreshTimeout)
		defer cancel()

		err := refresh(refreshCtx)
		if err != nil {
			stripe.CompareAndSwap(claimed, nil)
		}
		return outcome, err
	})

	select {
	case <-ctx.Done():
		return "", fmt.Errorf("wait for tool catalog refresh: %w", ctx.Err())
	case result := <-results:
		outcome, _ := result.Val.(toolCatalogRefresh)
		return outcome, result.Err
	}
}

func (r *toolCatalogRefreshes) stripe(key string) *atomic.Pointer[toolCatalogRefreshClaim] {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(key))
	return &r.lastStarted[hash.Sum32()%toolCatalogRefreshStripes]
}

// claim starts a cooldown for key when stripe is outside one and returns it;
// otherwise it returns nil and says whose cooldown holds the stripe.
func (r *toolCatalogRefreshes) claim(stripe *atomic.Pointer[toolCatalogRefreshClaim], key string) (*toolCatalogRefreshClaim, toolCatalogRefresh) {
	now := r.now().UnixNano()
	last := stripe.Load()
	if last != nil && now-last.started < int64(toolCatalogRefreshCooldown) {
		if last.key == key {
			return nil, toolCatalogListedRecently
		}
		return nil, toolCatalogLookupSkipped
	}
	claimed := &toolCatalogRefreshClaim{key: key, started: now}
	if !stripe.CompareAndSwap(last, claimed) {
		// Calls for one key share a flight, so the winner is another key.
		return nil, toolCatalogLookupSkipped
	}
	return claimed, toolCatalogListed
}

// errToolNotOnUpstream refuses a call for a tool neither the catalog nor the
// upstream's listing knows about.
var errToolNotOnUpstream = &proxy.RejectError{
	Code:    proxy.RejectCodeInvalidParams,
	Message: "this tool isn't available on this MCP server",
	Data:    nil,
}

// toolCatalogSync keeps a server's tool catalog current for enforcement: rules
// read from the catalog, and tools it is missing are recorded from the upstream
// before those rules run. Writes only add tools, so annotations are whatever the
// first listing to include a tool declared, whichever account made it.
type toolCatalogSync struct {
	store          ToolCatalogStore
	refreshes      *toolCatalogRefreshes
	organizationID string
	mcpServerID    string
	projectID      string

	// metrics counts refusals the upstream was not asked about; nil counts nothing.
	metrics *ProxyMetrics

	// listUpstream returns the upstream's whole unfiltered tool list using the
	// credentials of the given request.
	listUpstream func(context.Context, *http.Request) ([]*mcp.Tool, error)
}

// callDisposition returns the disposition to enforce for a tools/call, recording
// the tool from the upstream first when the catalog has no entry for it. Any
// failure is returned, because enforcing with an empty disposition would let the
// call past rules that match on it.
func (c *toolCatalogSync) callDisposition(ctx context.Context, call *proxy.ToolsCallRequest) (string, error) {
	// Catalog names are stored trimmed, so a padded spelling can't read as a
	// different tool from the one a rule covers.
	name := strings.TrimSpace(call.Params.Name)

	catalog, err := c.store.ToolCatalog(ctx, c.mcpServerID, c.projectID)
	if err != nil {
		return "", fmt.Errorf("read tool catalog: %w", err)
	}
	if disposition, recorded := catalog[name]; recorded {
		return disposition, nil
	}

	if call.UserRequest == nil || call.UserRequest.UserHTTPRequest == nil {
		return "", errors.New("list upstream tools: call carries no HTTP request")
	}
	// Cloned because the shared refresh can outlive this request's handler.
	userRequest := call.UserRequest.UserHTTPRequest.Clone(context.WithoutCancel(ctx))

	refreshKey := toolCatalogRefreshKey(ctx, c.mcpServerID)

	outcome, err := c.refreshes.run(ctx, refreshKey, func(refreshCtx context.Context) error {
		tools, err := c.listUpstream(refreshCtx, userRequest)
		if err != nil {
			return fmt.Errorf("list upstream tools: %w", err)
		}
		if err := c.store.RecordMissingTools(refreshCtx, c.organizationID, c.mcpServerID, c.projectID, toolRecordsFromSDK(tools)); err != nil {
			return fmt.Errorf("record upstream tools: %w", err)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("refresh tool catalog: %w", err)
	}

	catalog, err = c.store.ToolCatalog(ctx, c.mcpServerID, c.projectID)
	if err != nil {
		return "", fmt.Errorf("reread tool catalog: %w", err)
	}
	if disposition, recorded := catalog[name]; recorded {
		return disposition, nil
	}
	if outcome == toolCatalogLookupSkipped {
		c.metrics.RecordToolLookupSkipped(ctx, c.organizationID)
	}
	return "", errToolNotOnUpstream
}

// toolCatalogRefreshKey scopes shared lookups and cooldowns to one caller on one
// server, because a lookup runs with that caller's upstream credential. Assistants,
// device agents and principal credentials have no user id, so the authenticated
// actor comes first.
func toolCatalogRefreshKey(ctx context.Context, mcpServerID string) string {
	if actor, ok := contextvalues.AuthenticatedActor(ctx); ok {
		return mcpServerID + "\x00" + actor.String()
	}
	if authCtx, ok := contextvalues.GetAuthContext(ctx); ok && authCtx != nil {
		return mcpServerID + "\x00" + authCtx.UserID
	}
	return mcpServerID
}

// recordListed adds tools from an unfiltered tools/list response that the catalog
// has no entry for, and writes nothing when every tool already has one.
func (c *toolCatalogSync) recordListed(ctx context.Context, list *proxy.ToolsListResponse) error {
	catalog, err := c.store.ToolCatalog(ctx, c.mcpServerID, c.projectID)
	if err != nil {
		return fmt.Errorf("read tool catalog: %w", err)
	}

	allRecorded := true
	for _, tool := range list.Result.Tools {
		if tool == nil {
			return errors.New("record listed tools: upstream listed a null tool")
		}
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			continue
		}
		if _, recorded := catalog[name]; !recorded {
			allRecorded = false
			break
		}
	}
	if allRecorded {
		return nil
	}

	listed, err := toolRecordsFromWire(list)
	if err != nil {
		return err
	}

	missing := make([]tooldisposition.ToolRecord, 0, len(listed))
	for _, tool := range listed {
		if _, recorded := catalog[strings.TrimSpace(tool.ToolName)]; !recorded {
			missing = append(missing, tool)
		}
	}
	if err := c.store.RecordMissingTools(ctx, c.organizationID, c.mcpServerID, c.projectID, missing); err != nil {
		return fmt.Errorf("record listed tools: %w", err)
	}
	return nil
}

// wireToolList decodes the annotations of a tools/list result with every hint as
// a pointer. [mcp.ToolAnnotations] holds readOnlyHint and idempotentHint as plain
// bools, which can't tell an explicit false from unset.
type wireToolList struct {
	Tools []struct {
		Name        string `json:"name"`
		Annotations *struct {
			Title           *string `json:"title"`
			ReadOnlyHint    *bool   `json:"readOnlyHint"`
			DestructiveHint *bool   `json:"destructiveHint"`
			IdempotentHint  *bool   `json:"idempotentHint"`
			OpenWorldHint   *bool   `json:"openWorldHint"`
		} `json:"annotations"`
	} `json:"tools"`
}

func toolRecordsFromWire(list *proxy.ToolsListResponse) ([]tooldisposition.ToolRecord, error) {
	if list.RemoteMessage == nil {
		return nil, errors.New("decode listed tools: response carries no remote message")
	}
	response, ok := list.RemoteMessage.Message.(*jsonrpc.Response)
	if !ok {
		return nil, fmt.Errorf("decode listed tools: message is %T", list.RemoteMessage.Message)
	}

	var decoded wireToolList
	if err := json.Unmarshal(response.Result, &decoded); err != nil {
		return nil, fmt.Errorf("decode listed tools: %w", err)
	}

	records := make([]tooldisposition.ToolRecord, 0, len(decoded.Tools))
	for _, tool := range decoded.Tools {
		if strings.TrimSpace(tool.Name) == "" {
			continue
		}
		record := tooldisposition.ToolRecord{ToolName: tool.Name, Title: nil, ReadOnlyHint: nil, DestructiveHint: nil, IdempotentHint: nil, OpenWorldHint: nil}
		if a := tool.Annotations; a != nil {
			record.Title = a.Title
			record.ReadOnlyHint = a.ReadOnlyHint
			record.DestructiveHint = a.DestructiveHint
			record.IdempotentHint = a.IdempotentHint
			record.OpenWorldHint = a.OpenWorldHint
		}
		records = append(records, record)
	}
	return records, nil
}

// toolRecordsFromSDK converts tools listed through the SDK client. An explicit
// false readOnlyHint or idempotentHint is recorded as unset, because the SDK type
// can't represent the difference; neither yields a disposition either way.
func toolRecordsFromSDK(tools []*mcp.Tool) []tooldisposition.ToolRecord {
	records := make([]tooldisposition.ToolRecord, 0, len(tools))
	for _, tool := range tools {
		if tool == nil || strings.TrimSpace(tool.Name) == "" {
			continue
		}
		record := tooldisposition.ToolRecord{ToolName: tool.Name, Title: nil, ReadOnlyHint: nil, DestructiveHint: nil, IdempotentHint: nil, OpenWorldHint: nil}
		if a := tool.Annotations; a != nil {
			if a.Title != "" {
				record.Title = new(a.Title)
			}
			if a.ReadOnlyHint {
				record.ReadOnlyHint = new(true)
			}
			record.DestructiveHint = a.DestructiveHint
			if a.IdempotentHint {
				record.IdempotentHint = new(true)
			}
			record.OpenWorldHint = a.OpenWorldHint
		}
		records = append(records, record)
	}
	return records
}
