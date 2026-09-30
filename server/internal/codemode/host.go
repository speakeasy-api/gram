package codemode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

const (
	// maxMembers bounds fan-out and retained catalog buffers per callback.
	maxMembers = 128
	// discoveryConcurrency bounds simultaneous upstream tools/list sessions.
	discoveryConcurrency = 4
	// memberDeadline limits an individual live catalog lookup.
	memberDeadline = 5 * time.Second
)

// ErrToolUnavailable deliberately does not distinguish hidden and absent names.
var ErrToolUnavailable = errors.New("tool unavailable")

// ErrExecutionRevoked is terminal for the admitted execution, even if its
// credentials or policy become available again later.
var ErrExecutionRevoked = errors.New("execution authorization revoked")

// Tool carries a current permitted definition and its private consistency token.
type Tool struct {
	// Path is the exact gateway-qualified name.
	Path string
	// Name is the unqualified upstream name.
	Name string
	// Description is the upstream tool description.
	Description string
	// Definition retains the upstream JSON Schema and extension fields.
	Definition json.RawMessage
	// Fingerprint binds the full definition, member, and dispatch destination.
	Fingerprint string
}

// Catalog is a live member listing after existing authorization filtering.
type Catalog struct {
	// Tools contains only currently visible definitions.
	Tools []Tool
	// Incomplete marks invalid or missing entries in the upstream listing.
	Incomplete bool
}

// Backend owns authentication and existing gateway routing. Every method must
// refresh current authorization and intersect it with the admitted member IDs.
type Backend interface {
	// Members returns the currently permitted part of the admitted member set.
	Members(context.Context) ([]Server, error)
	// List loads one member's live permitted catalog.
	List(context.Context, string) (*Catalog, error)
	// Invoke revalidates the fingerprint at the authoritative dispatcher.
	Invoke(context.Context, string, string, json.RawMessage) (json.RawMessage, error)
}

// CatalogHost holds only one execution's restrictions and resolved identities.
type CatalogHost struct {
	backend        Backend
	scope          string
	allowed        map[string]bool
	allowedServers map[string]bool
	mu             sync.Mutex
	pins           map[string]string
}

// NewHost binds a fresh execution to a backend and an optional narrowing list.
// A nil list permits the admitted catalog; a non-nil empty list permits no tools.
func NewHost(ctx context.Context, backend Backend, scope string, tools []string) (*CatalogHost, error) {
	host := &CatalogHost{backend: backend, scope: scope, allowed: nil, allowedServers: nil, mu: sync.Mutex{}, pins: make(map[string]string)}
	if tools != nil {
		if len(tools) > MaxCatalogTools {
			return nil, fmt.Errorf("too many selected tools")
		}
		host.allowed = make(map[string]bool, len(tools))
		for _, path := range tools {
			if _, _, err := splitPath(path); err != nil || host.allowed[path] {
				return nil, fmt.Errorf("invalid or duplicate selected tool")
			}
			host.allowed[path] = true
		}
		// Resolve each member once for admission, even for a large allow list.
		members := make(map[string]bool)
		for path := range host.allowed {
			member, _, _ := splitPath(path)
			members[member] = true
		}
		if len(members) > maxMembers {
			return nil, fmt.Errorf("too many selected members")
		}
		host.allowedServers = members
		found := make(map[string]bool)
		var foundMu sync.Mutex
		group, groupCtx := errgroup.WithContext(ctx)
		group.SetLimit(discoveryConcurrency)
		for member := range members {
			group.Go(func() error {
				memberCtx, cancel := context.WithTimeout(groupCtx, memberDeadline)
				defer cancel()
				catalog, err := host.load(memberCtx, member)
				if err != nil {
					if errors.Is(err, ErrExecutionRevoked) {
						return err
					}
					return ErrToolUnavailable
				}
				for _, tool := range catalog.Tools {
					if !host.allowed[tool.Path] {
						continue
					}
					foundMu.Lock()
					found[tool.Path] = true
					foundMu.Unlock()
					if err := host.pin(tool); err != nil {
						return err
					}
				}
				return nil
			})
		}
		if err := group.Wait(); err != nil {
			return nil, fmt.Errorf("resolve selected tools: %w", err)
		}

		if len(found) != len(host.allowed) {
			return nil, ErrToolUnavailable
		}
	}
	return host, nil
}

func (h *CatalogHost) permitted(path string) bool { return h.allowed == nil || h.allowed[path] }

func (h *CatalogHost) members(ctx context.Context) ([]Server, error) {
	servers, err := h.backend.Members(ctx)
	if err != nil {
		return nil, fmt.Errorf("load permitted servers: %w", err)
	}
	if len(servers) > maxMembers {
		return nil, fmt.Errorf("gateway exceeds code-mode member limit")
	}
	if h.allowed == nil {
		return servers, nil
	}
	permitted := make([]Server, 0, len(servers))
	for _, server := range servers {
		if h.allowedServers[server.Slug] {
			permitted = append(permitted, server)
		}
	}
	return permitted, nil
}

// Servers returns permitted prefixes without enumerating upstream tools.
func (h *CatalogHost) Servers(ctx context.Context, args PageArgs) (*ServerPage, error) {
	servers, err := h.members(ctx)
	if err != nil {
		return nil, err
	}
	return PaginateServers(h.scope, args, servers)
}

// Search enumerates relevant members on demand and bounds upstream concurrency.
func (h *CatalogHost) Search(ctx context.Context, args SearchArgs) (*SearchPage, error) {
	if len(args.Query) > maxQueryBytes || len(args.Server) > maxQueryBytes || len(args.Cursor) > maxCursorBytes || args.Limit < 0 || args.Limit > MaxPageSize {
		return nil, fmt.Errorf("invalid search arguments")
	}
	servers, err := h.members(ctx)
	if err != nil {
		return nil, err
	}
	selected := make([]Server, 0, len(servers))
	for _, server := range servers {
		if args.Server == "" || args.Server == server.Slug {
			selected = append(selected, server)
		}
	}
	if args.Server != "" && len(selected) == 0 {
		return nil, ErrToolUnavailable
	}
	var mu sync.Mutex
	documents := make([]Document, 0)
	failed := make([]FailedMember, 0)
	totalBytes := 0
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(discoveryConcurrency)
	for _, server := range selected {
		group.Go(func() error {
			memberCtx, cancel := context.WithTimeout(groupCtx, memberDeadline)
			defer cancel()
			catalog, err := h.load(memberCtx, server.Slug)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if errors.Is(err, ErrExecutionRevoked) {
					return err
				}
				if groupCtx.Err() != nil {
					return groupCtx.Err()
				}
				failed = append(failed, FailedMember{Server: server.Slug, Reason: "catalog_unavailable"})
				return nil
			}
			if catalog.Incomplete {
				failed = append(failed, FailedMember{Server: server.Slug, Reason: "catalog_incomplete"})
			}
			for _, tool := range catalog.Tools {
				if !h.permitted(tool.Path) {
					continue
				}
				totalBytes += len(tool.Definition)
				if len(documents) >= MaxCatalogTools || totalBytes > MaxCatalogBytes {
					return fmt.Errorf("catalog exceeds discovery limit")
				}
				documents = append(documents, Document{Candidate: Candidate{Path: tool.Path, Description: tool.Description}, Name: tool.Name, ServerName: server.Name, Fingerprint: tool.Fingerprint})
			}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, fmt.Errorf("load search catalogs: %w", err)
	}
	return Rank(h.scope, args, documents, failed)
}

func (h *CatalogHost) load(ctx context.Context, server string) (*Catalog, error) {
	catalog, err := h.backend.List(ctx, server)
	if err != nil {
		return nil, fmt.Errorf("load member catalog: %w", err)
	}
	if catalog == nil || len(catalog.Tools) > MaxCatalogTools {
		return nil, fmt.Errorf("invalid member catalog")
	}
	size := 0
	seen := make(map[string]bool, len(catalog.Tools))
	for _, tool := range catalog.Tools {
		slug, _, err := splitPath(tool.Path)
		if err != nil || slug != server || seen[tool.Path] || tool.Fingerprint == "" {
			return nil, fmt.Errorf("invalid member catalog")
		}
		seen[tool.Path] = true
		size += len(tool.Definition)
		if size > MaxCatalogBytes {
			return nil, fmt.Errorf("member catalog exceeds limit")
		}
	}
	return catalog, nil
}

func (h *CatalogHost) resolve(ctx context.Context, path string) (*Tool, error) {
	server, _, err := splitPath(path)
	if err != nil || !h.permitted(path) {
		return nil, ErrToolUnavailable
	}
	catalog, err := h.load(ctx, server)
	if err != nil {
		return nil, err
	}
	for _, tool := range catalog.Tools {
		if tool.Path == path {
			if err := h.pin(tool); err != nil {
				return nil, err
			}
			return &tool, nil
		}
	}
	return nil, ErrToolUnavailable
}

func (h *CatalogHost) pin(tool Tool) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if previous, ok := h.pins[tool.Path]; ok && previous != tool.Fingerprint {
		return fmt.Errorf("tool_changed: rediscover in a new execution")
	}
	if _, exists := h.pins[tool.Path]; !exists && len(h.pins) >= MaxCatalogTools {
		return fmt.Errorf("too many resolved tools")
	}
	h.pins[tool.Path] = tool.Fingerprint
	return nil
}

// Describe loads and pins an exact definition, then generates a Python example.
func (h *CatalogHost) Describe(ctx context.Context, path string) (*Description, error) {
	tool, err := h.resolve(ctx, path)
	if err != nil {
		return nil, err
	}
	definition, err := publicDefinition(tool.Definition, tool.Path)
	if err != nil {
		return nil, err
	}
	// JSON string literals are valid Python string literals for qualified names.
	quoted, err := json.Marshal(path)
	if err != nil {
		return nil, fmt.Errorf("encode tool path: %w", err)
	}
	description := &Description{Path: path, Definition: definition, Fingerprint: tool.Fingerprint, CallTemplate: "result = await tools.call(" + string(quoted) + ", arguments)"}
	if err := boundedJSON(description); err != nil {
		return nil, err
	}
	return description, nil
}

// Call validates arguments before invoking the existing authoritative dispatcher.
func (h *CatalogHost) Call(ctx context.Context, path string, arguments json.RawMessage) (*ToolResult, error) {
	tool, err := h.resolve(ctx, path)
	if err != nil {
		return nil, err
	}
	if err := ValidateArguments(tool.Definition, arguments); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("call cancelled before dispatch: %w", err)
	}
	raw, err := h.backend.Invoke(ctx, path, tool.Fingerprint, arguments)
	if err != nil {
		if errors.Is(err, ErrExecutionRevoked) {
			return nil, fmt.Errorf("invoke tool: %w", err)
		}
		var dispatch *DispatchError
		if errors.As(err, &dispatch) && dispatch.BeforeInvoke {
			return nil, dispatch
		}
		return failedResult("unknown", "tool_call_outcome_unknown"), nil
	}
	result, err := NormalizeResult(raw)
	if err != nil {
		return failedResult("completed", "tool_result_unavailable"), nil
	}
	if result.OK && string(result.Data) != "null" {
		if err := validateSchemaField(tool.Definition, "outputSchema", result.Data, false); err != nil {
			result.Warnings = append(result.Warnings, "structured output does not satisfy its declared schema")
		}
	}
	return result, nil
}

func splitPath(path string) (string, string, error) {
	server, name, ok := strings.Cut(path, "--")
	if !ok || server == "" || name == "" || len(path) > maxQueryBytes {
		return "", "", ErrToolUnavailable
	}
	return server, name, nil
}

func publicDefinition(raw json.RawMessage, path string) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return nil, fmt.Errorf("invalid tool definition")
	}
	delete(fields, "_meta")
	name, err := json.Marshal(path)
	if err != nil {
		return nil, fmt.Errorf("encode path: %w", err)
	}
	fields["name"] = name
	definition, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("encode definition: %w", err)
	}
	return definition, nil
}
