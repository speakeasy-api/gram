import type { APIKeyPolicyGrant } from "@gram/client/models/components/apikeypolicygrant.js";
import { APIKeyPolicyGrantScope } from "@gram/client/models/components/apikeypolicygrant.js";
import type { Key } from "@gram/client/models/components/key.js";

export type Effect = "allow" | "block";

export const ALLOW_SCOPE: Record<
  "server" | "client",
  APIKeyPolicyGrant["scope"]
> = {
  server: APIKeyPolicyGrantScope.McpConnect,
  client: APIKeyPolicyGrantScope.McpAuth,
};
export const BLOCK_SCOPE: Record<
  "server" | "client",
  APIKeyPolicyGrant["scope"]
> = {
  server: APIKeyPolicyGrantScope.McpBlockedConnect,
  client: APIKeyPolicyGrantScope.McpBlockedAuth,
};

export function grantKind(grant: APIKeyPolicyGrant): "server" | "client" {
  return grant.selector.resourceKind === "mcp_auth_client"
    ? "client"
    : "server";
}

export function grantEffect(grant: APIKeyPolicyGrant): Effect {
  return grant.scope === APIKeyPolicyGrantScope.McpBlockedConnect ||
    grant.scope === APIKeyPolicyGrantScope.McpBlockedAuth
    ? "block"
    : "allow";
}

export function grantKey(grant: APIKeyPolicyGrant): string {
  const s = grant.selector;
  return [
    grant.scope,
    s.resourceKind,
    s.resourceId,
    s.projectId ?? "",
    s.clientType ?? "",
  ].join("|");
}

/** A short summary for the keys table: what the key's own policy reaches. */
export function keyPolicySummary(key: Key): string {
  const grants = key.policyGrants ?? [];
  if (grants.length === 0) return "Not set";
  const servers = grants.filter(
    (g) => grantKind(g) === "server" && grantEffect(g) === "allow",
  ).length;
  const clients = grants.filter(
    (g) => grantKind(g) === "client" && grantEffect(g) === "allow",
  ).length;
  const parts = [
    servers > 0 ? `${servers} server ${servers === 1 ? "rule" : "rules"}` : "",
    clients > 0
      ? `${clients} ${clients === 1 ? "credential" : "credentials"}`
      : "",
  ].filter(Boolean);
  return parts.length > 0 ? parts.join(", ") : "No access allowed";
}
