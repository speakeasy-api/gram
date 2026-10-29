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
  /** Whether people connect their own accounts could not be checked. */
  | "check_failed"
  /** The reader cannot change the server. */
  | "no_permission";

/**
 * Whether a server can be moved to a shared issuer from the agent wizard.
 *
 * Moving a server is only offered when nobody's upstream sign-in depends on
 * its current issuer. A sign-in client that people connect their own accounts
 * through is bound to that issuer, so moving the server would strand their
 * connections. Clients holding the server's own credential are not counted:
 * they belong to the server, not to the people using it.
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
  const peopleConnect = useQuery({
    // Kept apart from the client list caches, which hold pages of clients.
    queryKey: ["agents", "switch-sign-in", "people-connect", request],
    queryFn: async ({ signal }) => {
      const pages = await sdk.remoteSessionClients.list(request, undefined, {
        signal,
      });
      for await (const page of pages) {
        if (
          page.result.items.some(
            (client) => client.credentialOwner === "subject",
          )
        ) {
          return true;
        }
      }
      return false;
    },
    enabled: canWrite && !!server.issuerId && !!server.projectSlug,
    retry: false,
  });
  if (!canWrite) return "no_permission";
  if (!server.issuerId || !server.projectSlug) return "check_failed";
  if (peopleConnect.isPending) return "checking";
  if (peopleConnect.isError) return "check_failed";
  return peopleConnect.data ? "people_connect" : "ready";
}
