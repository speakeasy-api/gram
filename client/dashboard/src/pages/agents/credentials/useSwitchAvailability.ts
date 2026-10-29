import { collectPageItems } from "@/components/sessions/collectPageItems";
import { useSdkClient } from "@/contexts/Sdk";
import { useRBAC } from "@/hooks/useRBAC";
import { useQuery } from "@tanstack/react-query";
import type { SetupServer } from "./setupInstructions";

export type SwitchAvailability =
  /** Still checking whether the switch is safe. */
  | "checking"
  /** The reader may switch the server, and nothing upstream breaks. */
  | "ready"
  /** People connect their own accounts through the server's current sign-in. */
  | "people_connect"
  /** The reader cannot change the server. */
  | "no_permission";

/**
 * Whether a server can be moved to a shared issuer from the agent wizard.
 *
 * Moving a server is only offered when nothing it serves upstream depends on
 * its current issuer: sign-in clients (people connecting their own accounts)
 * are bound to that issuer, so moving the server would strand their
 * connections. A server on a shared credential has none.
 */
export function useSwitchAvailability(server: SetupServer): SwitchAvailability {
  const sdk = useSdkClient();
  const { hasScope } = useRBAC();
  const canWrite = hasScope(
    "mcp:write",
    server.resourceId ?? server.id,
    server.projectId,
  );
  const request = {
    gramProject: server.projectSlug,
    userSessionIssuerId: server.issuerId,
  };
  const clients = useQuery({
    queryKey: [
      "@gram/client",
      "remoteSessionClients",
      "list",
      request,
      "switch-sign-in",
    ],
    queryFn: ({ signal }) =>
      collectPageItems(
        sdk.remoteSessionClients.list(request, undefined, { signal }),
      ),
    enabled: canWrite && !!server.issuerId && !!server.projectSlug,
    retry: false,
  });
  if (!canWrite) return "no_permission";
  if (!server.issuerId || !server.projectSlug) return "people_connect";
  if (clients.isPending) return "checking";
  // A failed check is treated like a server people connect to: the reader is
  // sent to its settings rather than offered a change that might strand them.
  if (clients.isError || clients.data.length > 0) return "people_connect";
  return "ready";
}
