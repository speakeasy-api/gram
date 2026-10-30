package example

import (
	"context"
	"net/http"

	"github.com/speakeasy-api/gram/spikes/clean-slate-authz/xrbac"
)

// Option B: the Extensible RBAC client. Handlers pass an identity-only
// Subject and a Target built from loaded rows; the backend loads authority
// for every call.

type XService struct {
	authz *xrbac.Client
}

type xToolsetRow struct{ ID, ProjectID, OrganizationID string }

func serverTarget(t xToolsetRow) xrbac.Target {
	return xrbac.Target{
		Resource:  xrbac.Ref{Org: t.OrganizationID, Kind: "mcp", ID: t.ID},
		Ancestors: []xrbac.Ref{{Org: t.OrganizationID, Kind: "project", ID: t.ProjectID}, {Org: t.OrganizationID, Kind: "org", ID: t.OrganizationID}},
	}
}

// subjectFrom is the middleware's job: authentication → identity only.
func subjectFrom(r *http.Request) xrbac.Subject {
	return xrbac.Subject{Org: "<ORG_ID>", Actor: "user:<USER_ID>", Method: xrbac.AuthSession}
}

func (s *XService) UpdateToolset(ctx context.Context, subject xrbac.Subject, ts xToolsetRow) error {
	if err := s.authz.Require(ctx, subject, xrbac.ResourceCheck(xrbac.ScopeMCPWrite, serverTarget(ts))); err != nil {
		return err
	}
	// ... mutate
	return nil
}

// CallTool: the tool check (and, when used, the auth-client check) in one
// consistent evaluation.
func (s *XService) CallTool(ctx context.Context, subject xrbac.Subject, ts xToolsetRow, tool, classification string) error {
	d, err := s.authz.AuthorizeAll(ctx, subject, []xrbac.Request{
		xrbac.ToolCallCheck(xrbac.ScopeMCPConnect, serverTarget(ts), xrbac.ToolFacts{Name: tool, Classification: classification}),
		// xrbac.ResourceCheck(xrbac.ScopeMCPAuth, authClientTarget), // when an upstream auth client is used
	})
	if err != nil {
		return err
	}
	return d.RequireAllowed()
}

// Admit: before a tool is chosen, prove some tool call passes every limit.
func (s *XService) Admit(ctx context.Context, subject xrbac.Subject, ts xToolsetRow) error {
	d, err := s.authz.DiscoverAccess(ctx, subject, xrbac.AccessQuery{Scope: xrbac.ScopeMCPConnect, Target: serverTarget(ts)})
	if err != nil {
		return err
	}
	return d.RequireAllowed()
}

// ListChats: PlanQuery is deferred in the plan, so a list loads a bounded
// candidate page with SQLc and filters it. Totals and pagination are not
// authorization-aware until the query design lands.
func (s *XService) ListChats(ctx context.Context, subject xrbac.Subject, page []string, org, project string) ([]xrbac.Target, error) {
	reqs := make([]xrbac.Request, len(page))
	for i, id := range page {
		reqs[i] = xrbac.ResourceCheck(xrbac.ScopeChatRead, xrbac.Target{
			Resource:  xrbac.Ref{Org: org, Kind: "chat", ID: id},
			Ancestors: []xrbac.Ref{{Org: org, Kind: "project", ID: project}, {Org: org, Kind: "org", ID: org}},
		})
	}
	return s.authz.Filter(ctx, subject, reqs)
}

// UpdateAgentPolicy: edit permission, then whole-policy containment against
// the editor and the current owner (loaded by the backend, not the caller).
func (s *XService) UpdateAgentPolicy(ctx context.Context, subject xrbac.Subject, agent xrbac.Target, proposed []xrbac.Rule) error {
	if err := s.authz.Require(ctx, subject, xrbac.ResourceCheck(xrbac.ScopeAgentWrite, agent)); err != nil {
		return err
	}
	d, err := s.authz.CheckDelegation(ctx, subject, xrbac.Delegation{Agent: agent, Proposed: proposed})
	if err != nil {
		return err
	}
	if err := d.RequireAllowed(); err != nil {
		return err
	}
	// Save policy + audit in the caller's transaction (plan task 3/4).
	return nil
}
