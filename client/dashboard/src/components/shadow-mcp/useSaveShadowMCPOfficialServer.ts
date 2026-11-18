import { useProject } from "@/contexts/Auth";
import { invalidateAllShadowMCPInventory } from "@gram/client/react-query/shadowMCPInventory.js";
import { invalidateAllShadowMCPInventoryServer } from "@gram/client/react-query/shadowMCPInventoryServer.js";
import { useSetShadowMCPOfficialServerMutation } from "@gram/client/react-query/setShadowMCPOfficialServer.js";
import { useQueryClient } from "@tanstack/react-query";

export type OfficialServerChoice = {
  /** The Shadow MCP server's URL. */
  serverUrl: string;
  /** The server chosen as official; undefined keeps the one found by URL. */
  mcpServerId?: string;
  /** Keep direct connections to serverUrl working. */
  allowDirectAccess: boolean;
};

/**
 * Saves an admin's choice about a Shadow MCP server's official version and
 * refreshes the list and the server page, which both show the result.
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
          allowDirectAccess: choice.allowDirectAccess,
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
