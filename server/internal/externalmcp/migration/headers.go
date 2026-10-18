package migration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ettle/strcase"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/environments"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/speakeasy-api/gram/server/internal/externalmcp/migration/repo"
	"github.com/speakeasy-api/gram/server/internal/mcp/httpheaders"
	mcpmetadatarepo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// plannedHeader is one header the remote source sends upstream: a stored
// value, or a value the client sends on each request.
type plannedHeader struct {
	name        string
	value       string
	fromRequest string
}

// headerDefinition is the part of a registry header declaration the copy
// reads from external_mcp_tool_definitions.header_definitions.
type headerDefinition struct {
	Name string `json:"name"`
}

// planHeaders works out the remote source's headers so the upstream receives
// what the external MCP proxy sends today. Every system environment value
// becomes a stored secret header, named and merged exactly as the proxy does.
// Every declared header becomes a pass-through from the request header the
// proxy reads it from, except Authorization on a server that signs in with
// OAuth, which the sign-in supplies. A stored value wins over a pass-through
// of the same name.
func (m *Migrator) planHeaders(ctx context.Context, tx pgx.Tx, toolset toolsetsrepo.Toolset, outcome *Outcome) ([]plannedHeader, error) {
	rows, err := repo.New(tx).ListHeaderDefinitions(ctx, repo.ListHeaderDefinitionsParams{ProjectID: toolset.ProjectID, ToolsetID: toolset.ID})
	if err != nil {
		return nil, fmt.Errorf("list header definitions: %w", err)
	}
	if len(rows) == 0 {
		return nil, errNotCandidate
	}

	definitions := make([]externalmcp.HeaderDefinition, 0)
	passThrough := make([]externalmcp.HeaderDefinition, 0)
	// Per-tool definitions repeat their attachment's header declarations.
	declaredOnce := map[string]bool{}
	for _, row := range rows {
		var declared []headerDefinition
		if len(row.HeaderDefinitions) > 0 {
			if err := json.Unmarshal(row.HeaderDefinitions, &declared); err != nil {
				return nil, fail("the attachment's header declarations cannot be read")
			}
		}
		for _, header := range declared {
			definition := externalmcp.HeaderDefinition{Name: strcase.ToSNAKE(row.AttachmentSlug + "_" + header.Name), HeaderName: header.Name}
			if declaredOnce[definition.Name] {
				continue
			}
			declaredOnce[definition.Name] = true
			definitions = append(definitions, definition)
			if row.RequiresOauth && strings.EqualFold(strings.TrimSpace(header.Name), "Authorization") {
				continue
			}
			passThrough = append(passThrough, definition)
		}
	}

	systemEnv, err := environments.NewEnvironmentEntries(m.logger, tx, m.encryption, mcpmetadatarepo.New(tx)).
		LoadSystemEnv(ctx, toolset.ProjectID, toolset.ID, string(urn.ToolKindExternalMCP), rows[0].AttachmentSlug)
	if err != nil {
		return nil, fmt.Errorf("load system environment: %w", err)
	}
	stored := externalmcp.BuildHeaders(systemEnv, toolconfig.NewCaseInsensitiveEnv(), definitions, "")

	planned := make([]plannedHeader, 0, len(stored)+len(passThrough))
	storedKeys := map[string]bool{}
	seen := map[string]bool{}
	names := make([]string, 0, len(stored))
	for name := range stored {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		header := plannedHeader{name: name, value: stored[name], fromRequest: ""}
		if err := checkHeader(header); err != nil {
			return nil, err
		}
		planned = append(planned, header)
		storedKeys[headerKey(name)] = true
		seen[headerKey(name)] = true
	}

	displayNames, err := headerDisplayNames(ctx, tx, toolset)
	if err != nil {
		return nil, err
	}
	for _, definition := range passThrough {
		key := headerKey(definition.HeaderName)
		if storedKeys[key] {
			outcome.Warnings = append(outcome.Warnings, fmt.Sprintf("%s keeps its stored value; a value sent with a request is no longer used", definition.HeaderName))
		}
		if seen[key] {
			continue
		}
		source := definition.Name
		if displayName := lookupFold(displayNames, definition.Name); displayName != "" {
			source = displayName
		}
		header := plannedHeader{name: definition.HeaderName, value: "", fromRequest: httpheaders.VariableHeaderName(source)}
		if err := checkHeader(header); err != nil {
			return nil, err
		}
		planned = append(planned, header)
		seen[key] = true
	}
	return planned, nil
}

// createHeaders writes the planned headers on the remote source, encrypting
// stored values, and records each one.
func (m *Migrator) createHeaders(ctx context.Context, tx pgx.Tx, organizationID string, principal urn.Principal, actorEmail *string, remote remotemcprepo.RemoteMcpServer, planned []plannedHeader) error {
	headers := remotemcp.NewHeaders(m.logger, tx, m.encryption)
	for _, header := range planned {
		stored := header.fromRequest == ""
		created, err := headers.CreateServerHeader(ctx, remotemcprepo.CreateServerHeaderParams{
			Name:                   header.name,
			Description:            pgtype.Text{String: "", Valid: false},
			IsRequired:             false,
			IsSecret:               stored,
			Value:                  conv.PtrToPGTextEmpty(conv.PtrEmpty(header.value)),
			ValueFromRequestHeader: conv.PtrToPGTextEmpty(conv.PtrEmpty(header.fromRequest)),
			RemoteMcpServerID:      remote.ID,
			ProjectID:              remote.ProjectID,
		})
		if err != nil {
			return fmt.Errorf("create remote header: %w", err)
		}
		if err := m.audit.LogRemoteMcpServerHeaderCreate(ctx, tx, audit.LogRemoteMcpServerHeaderCreateEvent{
			OrganizationID: organizationID, ProjectID: remote.ProjectID,
			Actor: principal, ActorDisplayName: actorEmail, ActorSlug: nil,
			RemoteMcpServerHeaderURN: urn.NewRemoteMcpServerHeader(created.ID), RemoteMcpServerHeaderName: created.Name,
			RemoteMcpServerURN: urn.NewRemoteMcpServer(remote.ID), RemoteMcpServerURL: remote.Url,
		}); err != nil {
			return fmt.Errorf("audit remote header: %w", err)
		}
	}
	return nil
}

// describeHeaders lists the planned headers as preview actions.
func describeHeaders(planned []plannedHeader) []string {
	actions := make([]string, 0, len(planned))
	for _, header := range planned {
		if header.fromRequest == "" {
			actions = append(actions, fmt.Sprintf("Send the stored value of %s as a secret header", header.name))
			continue
		}
		actions = append(actions, fmt.Sprintf("Send %s from the %s request header", header.name, header.fromRequest))
	}
	return actions
}

// checkHeader applies the rules every remote header must meet, so a header
// the product would refuse fails the migration instead of being written.
func checkHeader(header plannedHeader) error {
	if _, err := proxy.NormalizeHeaderName(header.name); err != nil {
		return fail("header %q cannot be copied: %s", header.name, err.Error())
	}
	if header.fromRequest != "" {
		if _, err := proxy.NormalizeHeaderName(header.fromRequest); err != nil {
			return fail("header %q cannot be passed through from %q: %s", header.name, header.fromRequest, err.Error())
		}
	}
	configured := proxy.ConfiguredHeader{IsRequired: false, Name: header.name, StaticValue: header.value, ValueFromRequestHeader: header.fromRequest}
	if err := proxy.CheckRemoteHeader(configured); err != nil {
		return fail("header %q cannot be copied: %s", header.name, err.Error())
	}
	return nil
}

// headerKey compares header names the way remote header uniqueness does:
// ignoring case and treating underscores as dashes.
func headerKey(name string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(name)), "_", "-")
}

// headerDisplayNames reads the display names the MCP runtime accepts in place
// of a variable's MCP-<name> request header, keyed by variable name.
func headerDisplayNames(ctx context.Context, tx pgx.Tx, toolset toolsetsrepo.Toolset) (map[string]string, error) {
	raw, err := mcpmetadatarepo.New(tx).GetHeaderDisplayNames(ctx, mcpmetadatarepo.GetHeaderDisplayNamesParams{ProjectID: toolset.ProjectID, BackendID: toolset.ID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("read header display names: %w", err)
	}
	names := map[string]string{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &names); err != nil {
			return nil, fail("the server's header display names cannot be read")
		}
	}
	return names, nil
}

func lookupFold(values map[string]string, key string) string {
	for k, v := range values {
		if strings.EqualFold(k, key) {
			return v
		}
	}
	return ""
}
