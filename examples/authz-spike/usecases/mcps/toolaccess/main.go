// This executable checks policy admission versus concrete tool execution with
// fixture identity, trusted metadata, and a capture runtime, not real MCP transport.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/speakeasy-api/gram/examples/authz-spike/authz"
	"github.com/speakeasy-api/gram/examples/authz-spike/usecases/mcps"
)

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	user := authz.Principal{OrganizationID: "example-org", Kind: authz.UserPrincipal, ID: "example-user"}
	subject := authz.Subject{
		Principal: user, Authentication: authz.Authentication{Kind: authz.SessionAuthentication, ID: "example-session"},
		Delegation: authz.Delegation{Kind: authz.DirectExecution},
	}
	server := mcps.Server{OrganizationID: user.OrganizationID, ProjectID: "example-project", ID: "example-server"}
	project := authz.ResourceRef{OrganizationID: user.OrganizationID, Kind: authz.Project, ID: server.ProjectID}
	projectConnect := authz.Grant{Permission: authz.MCPConnect, Anchor: project}
	readOnly := authz.Grant{Permission: authz.MCPConnect, Anchor: project, Constraints: authz.Constraints{Disposition: authz.ReadOnly}}
	blockDelete := authz.Grant{Permission: authz.MCPBlockedConnect, Anchor: server.AuthzTarget().Resource, Constraints: authz.Constraints{Tool: "delete"}}
	blockServer := authz.Grant{Permission: authz.MCPBlockedConnect, Anchor: server.AuthzTarget().Resource}
	client, err := authz.NewClient(authz.NewMemoryBackend(), authz.AllDefinitions())
	if err != nil {
		return err
	}
	repository := fixtureRepository{server: server, tools: map[string]authz.ToolFacts{
		"list":   {Name: "list", Disposition: authz.ReadOnly},
		"delete": {Name: "delete", Disposition: authz.Destructive},
		"lookup": {Name: "lookup", Disposition: authz.DispositionUnknown},
	}}
	cases := []struct {
		name        string
		grants      []authz.Grant
		connect     bool
		allowedTool map[string]bool
	}{
		{
			name:   "read-only project grant admits connection, not destructive or unclassified tools",
			grants: []authz.Grant{readOnly}, connect: true,
			allowedTool: map[string]bool{"list": true, "delete": false, "lookup": false},
		},
		{
			name:   "one-tool block leaves connection and other tools available",
			grants: []authz.Grant{projectConnect, blockDelete}, connect: true,
			allowedTool: map[string]bool{"list": true, "delete": false, "lookup": true},
		},
		{
			name:   "unrestricted grant can execute an unclassified tool",
			grants: []authz.Grant{projectConnect}, connect: true,
			allowedTool: map[string]bool{"list": true, "delete": true, "lookup": true},
		},
		{
			name:   "server-wide block prevents upstream connection",
			grants: []authz.Grant{projectConnect, blockServer}, connect: false, allowedTool: nil,
		},
		{
			name:   "project read alone grants no MCP connection",
			grants: []authz.Grant{{Permission: authz.ProjectRead, Anchor: project}}, connect: false, allowedTool: nil,
		},
	}
	for _, example := range cases {
		if err := client.ReplaceGrants(ctx, user, example.grants); err != nil {
			return err
		}
		runtime := &captureRuntime{server: server, connections: 0, calls: 0}
		service := mcps.NewService(client, repository, runtime)
		err := service.Connect(ctx, subject, server.ID)
		if !example.connect {
			if !errors.Is(err, authz.ErrDenied) || runtime.connections != 0 || runtime.calls != 0 {
				return fmt.Errorf("%s: expected denied connection with no upstream work, got %v", example.name, err)
			}
			fmt.Printf("PASS: %s — upstream connections=0, calls=0\n", example.name)
			continue
		}
		if err != nil || runtime.connections != 1 {
			return fmt.Errorf("%s: expected one admitted connection, got %v", example.name, err)
		}
		for _, name := range []string{"list", "delete", "lookup"} {
			before := runtime.calls
			result, err := service.Invoke(ctx, subject, server.ID, name)
			if example.allowedTool[name] {
				if err != nil || result != "executed "+name || runtime.calls != before+1 {
					return fmt.Errorf("%s: %s should execute once: %v", example.name, name, err)
				}
			} else if !errors.Is(err, authz.ErrDenied) || result != "" || runtime.calls != before {
				return fmt.Errorf("%s: %s should refuse without upstream execution: %v", example.name, name, err)
			}
		}
		// Keep the same admitted connection, then revoke its authority. No cached
		// connection decision may authorize the next invocation.
		if err := client.ReplaceGrants(ctx, user, nil); err != nil {
			return err
		}
		before := runtime.calls
		result, err := service.Invoke(ctx, subject, server.ID, "list")
		if !errors.Is(err, authz.ErrDenied) || result != "" || runtime.calls != before || runtime.connections != 1 {
			return fmt.Errorf("%s: invocation after revocation used earlier admission: %v", example.name, err)
		}
		fmt.Printf("PASS: %s — every tool checked; revocation prevents later calls\n", example.name)
	}
	return nil
}

// fixtureRepository binds tool classifications to trusted, tenant-scoped metadata.
type fixtureRepository struct {
	server mcps.Server
	tools  map[string]authz.ToolFacts
}

func (r fixtureRepository) GetServer(ctx context.Context, organizationID, serverID string) (mcps.Server, error) {
	if err := ctx.Err(); err != nil {
		return mcps.Server{}, err
	}
	if r.server.OrganizationID != organizationID || r.server.ID != serverID {
		return mcps.Server{}, fmt.Errorf("server not found")
	}
	return r.server, nil
}

func (r fixtureRepository) GetTool(ctx context.Context, server mcps.Server, toolName string) (authz.ToolFacts, error) {
	if err := ctx.Err(); err != nil {
		return authz.ToolFacts{}, err
	}
	facts, exists := r.tools[toolName]
	if server != r.server || !exists {
		return authz.ToolFacts{}, fmt.Errorf("tool not found")
	}
	return facts, nil
}

// captureRuntime counts only protected upstream work, never metadata lookups.
type captureRuntime struct {
	server      mcps.Server
	connections int
	calls       int
}

func (r *captureRuntime) Open(ctx context.Context, server mcps.Server) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if server != r.server {
		return fmt.Errorf("wrong upstream server")
	}
	r.connections++
	return nil
}

func (r *captureRuntime) Call(ctx context.Context, server mcps.Server, toolName string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if server != r.server || r.connections != 1 {
		return "", fmt.Errorf("missing expected upstream connection")
	}
	r.calls++
	return "executed " + toolName, nil
}
