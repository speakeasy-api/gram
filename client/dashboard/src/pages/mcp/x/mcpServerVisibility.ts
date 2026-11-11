import { useSdkClient } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { useRBAC } from "@/hooks/useRBAC";
import { isKnownEnumValue } from "@/lib/enum";
import { handleError } from "@/lib/errors";
import {
  invalidateMcpServerQueries,
  mcpServerVisibilityToast,
  mcpServerVisibilityUpdateForm,
} from "@/lib/mcp-server-visibility";
import { standsInForToolset } from "@/pages/mcp/x/MCPServerDetailsRouting";
import { useFollowRenamedServer } from "@/pages/mcp/x/useFollowRenamedServer";
import type {
  McpServer,
  McpServerVisibility,
} from "@gram/client/models/components/mcpserver.js";
import { UpdateMcpServerFormVisibility as KnownUpdateMcpServerFormVisibility } from "@gram/client/models/components/updatemcpserverform.js";
import { buildGetMcpServerQuery } from "@gram/client/react-query/getMcpServer.js";
import { useUpdateMcpServerMutation } from "@gram/client/react-query/updateMcpServer.js";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";

export type VisibilityOption = {
  value: McpServerVisibility;
  label: string;
  description: string;
  dotClass: string;
  hoverDotClass: string;
};

export const VISIBILITY_OPTIONS: VisibilityOption[] = [
  {
    value: "disabled",
    label: "Disabled",
    description: "This server is offline. No users can connect to it",
    dotClass: "bg-amber-400",
    hoverDotClass: "group-hover:bg-amber-400",
  },
  {
    value: "private",
    label: "Private",
    description: "The server serves traffic.",
    dotClass: "bg-blue-400",
    hoverDotClass: "group-hover:bg-blue-400",
  },
];

// Public visibility is offered for hosted servers, and for tunneled-backed
// servers once the tunnel source owner has consented (double opt-in).
const PUBLIC_VISIBILITY_OPTION: VisibilityOption = {
  value: "public",
  label: "Public",
  description:
    "Anyone can connect anonymously — no login. Every tool is exposed to the public internet.",
  dotClass: "bg-green-400",
  hoverDotClass: "group-hover:bg-green-400",
};

export const HOSTED_PUBLIC_DESCRIPTION =
  "Anyone with the URL can read the tools hosted by this server. Authentication is still required to use the tools.";

export const ALL_VISIBILITY_OPTIONS = [
  ...VISIBILITY_OPTIONS,
  PUBLIC_VISIBILITY_OPTION,
];

export function visibilityLabel(visibility: McpServerVisibility): string {
  switch (visibility) {
    case "disabled":
      return "Disabled";
    case "public":
      return "Public";
    case "private":
      return "Private";
  }
  return "Unknown";
}

/**
 * Changes a server's visibility. The update replaces the whole record, so it
 * reads the latest copy first rather than write back a stale one.
 */
export function useMcpServerVisibilityUpdate(server: McpServer): {
  /** The user may edit this MCP server. */
  hasWriteScope: boolean;
  /** The user may edit, and this client knows the stored visibility. */
  canWrite: boolean;
  updateVisibility: (visibility: McpServerVisibility) => void;
  updating: boolean;
} {
  const { hasScope } = useRBAC();
  // Checked on this server, like the API does; a hosted server is granted
  // under its toolset id.
  const hasWriteScope = hasScope("mcp:write", server.toolsetId ?? server.id);
  const canWrite =
    hasWriteScope &&
    isKnownEnumValue(KnownUpdateMcpServerFormVisibility, server.visibility);
  const queryClient = useQueryClient();
  const client = useSdkClient();
  const telemetry = useTelemetry();
  const followRenamedServer = useFollowRenamedServer();
  const [fetchingLatest, setFetchingLatest] = useState(false);
  const update = useUpdateMcpServerMutation({
    onSuccess: async (updated, variables) => {
      followRenamedServer(updated);
      await invalidateMcpServerQueries(queryClient);
      const next = variables.request.updateMcpServerForm.visibility;
      toast.success(mcpServerVisibilityToast(next));
      // Keeps the toolset page's visibility events for hosted servers.
      if (standsInForToolset(server)) {
        telemetry.capture("mcp_event", {
          action:
            next === "disabled"
              ? "mcp_disabled"
              : next === "public"
                ? "mcp_made_public"
                : "mcp_made_private",
          slug: server.toolset?.slug,
        });
      }
    },
    onError: (error) => {
      toast.error(
        error instanceof Error
          ? error.message
          : "Failed to update server visibility",
      );
    },
  });

  const updateVisibility = (next: McpServerVisibility) => {
    if (!canWrite || next === server.visibility) return;
    setFetchingLatest(true);
    void (async () => {
      try {
        const latest = await queryClient.fetchQuery({
          ...buildGetMcpServerQuery(client, { id: server.id }),
          staleTime: 0,
        });
        // Someone else already made this change; refresh so the page shows it.
        if (next === latest.visibility) {
          await invalidateMcpServerQueries(queryClient);
          return;
        }
        update.mutate({
          request: {
            updateMcpServerForm: mcpServerVisibilityUpdateForm(latest, next),
          },
        });
      } catch (error) {
        handleError(error, { title: "Couldn't update server availability" });
      } finally {
        setFetchingLatest(false);
      }
    })();
  };

  return {
    hasWriteScope,
    canWrite,
    updateVisibility,
    updating: fetchingLatest || update.isPending,
  };
}
