import { useListAPIKeys } from "@gram/client/react-query/listAPIKeys.js";
import { useMcpServerAuthentication } from "@gram/client/react-query/mcpServerAuthentication.js";
import { useOrganization } from "@/contexts/Auth";
import {
  useUpstreamAuthEnforcement,
  type UpstreamAuthEnforcementStatus,
} from "@/hooks/useUpstreamAuthEnforcement";
import { keysUseSharedCredential } from "./onBehalfOf";

const keysSecurity = { sessionHeaderGramSession: "" };

/**
 * Whether an agent's live keys sign in to a server with a shared credential
 * chosen for them, rather than with anyone's connection, or "checking" while
 * that is read. The Overview's "Acting on behalf of" and the MCP sign-in tab
 * both read it, so they cannot disagree.
 *
 * A key's choice counts only under upstream auth enforcement, and only on a
 * server that would otherwise need someone's connection. A failed read falls
 * back to the connections assigned, as before keys could choose one.
 */
export function useAgentKeysUseSharedCredential(
  /** No agent, as while one is being created, has no keys to read. */
  agentId: string | undefined,
  server: { id: string; issuerId?: string } | undefined,
): boolean | "checking" {
  const organization = useOrganization();
  const enforcement = useUpstreamAuthEnforcement();
  const read = !!agentId && !!server?.issuerId && enforcement === "enforced";
  // Shares its cache with the agent's API keys section.
  const keys = useListAPIKeys({ agentId: agentId ?? "" }, keysSecurity, {
    queryKeyHashFn: (key) => JSON.stringify([organization.id, key]),
    enabled: read,
    retry: false,
    throwOnError: false,
  });
  const auth = useMcpServerAuthentication(
    { mcpServerId: server?.id ?? "" },
    undefined,
    { enabled: read, retry: false, throwOnError: false },
  );
  if (!agentId) return false;
  return keysShared(server, enforcement, keys, auth);
}

function keysShared(
  server: { id: string; issuerId?: string } | undefined,
  enforcement: UpstreamAuthEnforcementStatus,
  keys: Pick<
    ReturnType<typeof useListAPIKeys>,
    "data" | "isPending" | "isError"
  >,
  auth: Pick<
    ReturnType<typeof useMcpServerAuthentication>,
    "data" | "isPending" | "isError"
  >,
): boolean | "checking" {
  if (!server?.issuerId) return false;
  if (enforcement === "loading") return "checking";
  if (enforcement !== "enforced" || keys.isError || auth.isError) return false;
  if (!keys.data || !auth.data) return "checking";
  return keysUseSharedCredential(keys.data.keys, server.id, auth.data);
}
