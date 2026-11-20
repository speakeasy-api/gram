import { useProject } from "@/contexts/Auth";
import { invalidateAllShadowMCPInventory } from "@gram/client/react-query/shadowMCPInventory.js";
import { invalidateAllShadowMCPInventoryServer } from "@gram/client/react-query/shadowMCPInventoryServer.js";
import { useSetShadowMCPOfficialServerMutation } from "@gram/client/react-query/setShadowMCPOfficialServer.js";
import { useQueryClient } from "@tanstack/react-query";

export type OfficialServerChoice = {
  /** The Shadow MCP server's URL. */
  serverUrl: string;
  /** The server to link as official. */
  mcpServerId?: string;
  /** Keep the direct connection: nothing is linked, not even automatically. */
  keepShadow?: boolean;
};

/**
 * Saves the admin's choice for a Shadow MCP server's official version and
 * refreshes the list and the server page, which both show the result. With
 * neither a server nor keepShadow, the choice is removed and the server that
 * proxies to the URL, if any, is linked automatically again.
 */
export function useSaveShadowMCPOfficialServer(): {
  save: (choice: OfficialServerChoice) => Promise<void>;
  isPending: boolean;
} {
  const project = useProject();
  const queryClient = useQueryClient();
  const mutation = useSetShadowMCPOfficialServerMutation();
  const save = async (choice: OfficialServerChoice) => {
    await mutation.mutateAsync({
      request: {
        setShadowMCPOfficialServerForm: {
          projectId: project.id,
          serverUrl: choice.serverUrl,
          mcpServerId: choice.mcpServerId,
          keepShadow: choice.keepShadow,
        },
      },
    });
    await Promise.all([
      invalidateAllShadowMCPInventory(queryClient),
      invalidateAllShadowMCPInventoryServer(queryClient),
    ]);
  };
  return { save, isPending: mutation.isPending };
}
