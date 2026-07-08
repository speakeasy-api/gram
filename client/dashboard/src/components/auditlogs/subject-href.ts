import type { AuditLog } from "@gram/client/models/components/auditlog.js";

// Builds the dashboard path to an audit subject's detail page, or null when the
// subject has no navigable page (or is missing the slug/id needed to route to
// one). Single source of truth shared by the org audit log (inline subject link
// and row-level "open" affordance) and the project Activity Timeline, so the
// destinations can never drift apart.
export function subjectHref(log: AuditLog, orgSlug: string): string | null {
  // Project-scoped subjects live under `/{org}/projects/{project}/…` and use the
  // entry's own projectSlug — which may differ from the project currently in the
  // URL, so we interpolate it rather than using the URL-bound route helpers.
  const projectBase = log.projectSlug
    ? `/${orgSlug}/projects/${log.projectSlug}`
    : null;

  switch (log.subjectType) {
    case "deployment":
      return projectBase ? `${projectBase}/deployments/${log.subjectId}` : null;
    case "toolset":
      return projectBase && log.subjectSlug
        ? `${projectBase}/mcp/${log.subjectSlug}`
        : null;
    case "mcp_server":
      return projectBase && log.subjectSlug
        ? `${projectBase}/mcp/x/${log.subjectSlug}`
        : null;
    case "environment":
      return projectBase && log.subjectSlug
        ? `${projectBase}/environments/${log.subjectSlug}`
        : null;
    case "assistant":
      return projectBase ? `${projectBase}/assistants/${log.subjectId}` : null;
    case "risk_policy":
      // PolicyCenter has no per-item route; `?policy=<id>` opens the policy.
      return projectBase
        ? `${projectBase}/risk-policies?policy=${log.subjectId}`
        : null;
    case "chat_session":
      // The agent-sessions list opens the session's transcript drawer via
      // `?chatId=<id>`; subjectId is the chat session UUID.
      return projectBase
        ? `${projectBase}/agent-sessions?chatId=${log.subjectId}`
        : null;
    case "project":
      return log.subjectSlug ? `/${orgSlug}/projects/${log.subjectSlug}` : null;
    case "plugin":
      return projectBase ? `${projectBase}/plugins/${log.subjectId}` : null;
    // access_role and access_member are org-scoped (no project), so they route
    // under `/{org}/access/…` rather than the project tree.
    case "access_role":
      // RolesTab opens a specific role's editor via the `?editRole=<id>` param.
      return `/${orgSlug}/access/roles?editRole=${log.subjectId}`;
    case "access_member":
      return `/${orgSlug}/access/members`;
    case "mcp_collection":
      return log.subjectSlug
        ? `/${orgSlug}/collections/${log.subjectSlug}`
        : null;
    case "api_key":
      return `/${orgSlug}/api-keys`;
    default:
      return null;
  }
}
