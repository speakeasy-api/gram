import { GramError } from "@gram/client/models/errors/gramerror.js";

/** Response headers the server adds to a 403 the authz engine produced. */
const REQUIRED_SCOPE_HEADER = "Gram-Required-Scope";
const REQUIRED_RESOURCE_KIND_HEADER = "Gram-Required-Resource-Kind";

export interface Denial {
  /** The scope the request lacked, such as "log:read". */
  scope: string;
  /** The kind of resource it was checked against, such as "project". */
  resourceKind: string;
}

/**
 * The permission a failed request lacked, when the server said. A 403 that
 * did not come from an authz check, or any other error, gives null.
 */
export function denialFromError(error: unknown): Denial | null {
  if (!(error instanceof GramError) || error.statusCode !== 403) return null;
  const scope = error.headers.get(REQUIRED_SCOPE_HEADER);
  if (!scope) return null;
  return {
    scope,
    resourceKind: error.headers.get(REQUIRED_RESOURCE_KIND_HEADER) ?? "",
  };
}

const RESOURCE_KIND_LABELS: Record<string, string> = {
  org: "this organization",
  project: "this project",
  mcp: "this MCP server",
  mcp_auth_client: "this auth client",
  environment: "this environment",
  skill: "this skill",
  plugin: "this plugin",
  assistant: "this assistant",
  chat: "this session",
  agent: "this agent",
  workload: "this workload",
};

function resourceKindLabel(kind: string): string {
  return RESOURCE_KIND_LABELS[kind] ?? "this resource";
}

/**
 * One sentence a person can act on. `label` is the catalogue label when the
 * caller has it; the slug is always included so it can be requested as is.
 */
export function denialMessage(denial: Denial, label?: string): string {
  const permission =
    label && label !== denial.scope
      ? `${label} (${denial.scope})`
      : denial.scope;
  return `You need the ${permission} permission on ${resourceKindLabel(denial.resourceKind)}. An organization admin can grant it.`;
}
