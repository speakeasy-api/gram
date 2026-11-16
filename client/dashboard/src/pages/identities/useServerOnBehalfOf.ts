import { useListAPIKeys } from "@gram/client/react-query/listAPIKeys.js";
import { useMcpServerAuthentication } from "@gram/client/react-query/mcpServerAuthentication.js";
import { useOrganization } from "@/contexts/Auth";
import {
  useUpstreamAuthEnforcement,
  type UpstreamAuthEnforcementStatus,
} from "@/hooks/useUpstreamAuthEnforcement";
import { useOwnerConnections } from "@/pages/agents/credentials/useOwnerConnections";
import {
  useServerInventory,
  type InventoryServer,
} from "@/pages/agents/credentials/useServerInventory";
import {
  keysUseSharedCredential,
  serverOnBehalfOf,
  type ServerOnBehalfOf,
} from "./onBehalfOf";

const keysSecurity = { sessionHeaderGramSession: "" };

/**
 * Whom an agent acts for on one MCP server: the people whose own connections
 * are assigned to it there, or no one when a shared credential signs it in,
 * whether the server asks no one to sign in or the agent's keys were set up
 * to use a shared credential on it. The Permissions tab's MCP servers section
 * and the Overview's "Acting on behalf of" both read it, so they cannot
 * disagree.
 */
export function useServerOnBehalfOf(
  agentId: string,
  /** The server id a grant's selector names, or a server id from a call. */
  resourceId: string | undefined,
): ServerOnBehalfOf {
  const organization = useOrganization();
  const inventory = useServerInventory({ lookupOnly: true });
  const server = resourceId
    ? inventory.servers.find(
        (candidate) =>
          candidate.resourceId === resourceId || candidate.id === resourceId,
      )
    : undefined;
  const connections = useOwnerConnections(
    server ? [server] : [],
    !!server,
    agentId,
  );
  // A key's credential choice counts only under enforcement, and only on a
  // server that would otherwise need someone's connection.
  const enforcement = useUpstreamAuthEnforcement();
  const readChoices = !!server?.issuerId && enforcement === "enforced";
  // Shares its cache with the agent's API keys section.
  const keys = useListAPIKeys({ agentId }, keysSecurity, {
    queryKeyHashFn: (key) => JSON.stringify([organization.id, key]),
    enabled: readChoices,
    retry: false,
    throwOnError: false,
  });
  const auth = useMcpServerAuthentication(
    { mcpServerId: server?.id ?? "" },
    undefined,
    { enabled: readChoices, retry: false, throwOnError: false },
  );
  if (!resourceId) return { kind: "unknown" };
  // A failed server list says nothing about this server: it could not be
  // checked, which is not the same as no server going by this id.
  if (!server && inventory.isError) return { kind: "error" };
  return serverOnBehalfOf(
    server,
    server ? connections.assignedByServer?.get(server.id) : undefined,
    inventory.isLoading,
    Date.now(),
    keysShared(server, enforcement, keys, auth),
  );
}

/**
 * Whether the agent's keys use a shared credential chosen for the server. A
 * failed read falls back to the connections assigned, as before keys could
 * choose one.
 */
function keysShared(
  server: InventoryServer | undefined,
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
