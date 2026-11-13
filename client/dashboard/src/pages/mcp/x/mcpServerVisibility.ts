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
import { type QueryClient, useQueryClient } from "@tanstack/react-query";
import { useState, useSyncExternalStore } from "react";

import { toast } from "sonner";

/**
 * The visibility each server is being changed to, shared by every control for
 * that server, so the header and Settings show the same optimistic state
 * during one update. Kept per query client: the app has one, so every
 * control shares it, and each test's fresh client starts empty.
 */
type PendingStore = {
  pending: Map<string, McpServerVisibility>;
  listeners: Set<() => void>;
  subscribe: (listener: () => void) => () => void;
};
const pendingStores = new WeakMap<QueryClient, PendingStore>();
function pendingStoreFor(queryClient: QueryClient): PendingStore {
  let store = pendingStores.get(queryClient);
  if (!store) {
    const listeners = new Set<() => void>();
    store = {
      pending: new Map(),
      listeners,
      subscribe: (listener) => {
        listeners.add(listener);
        return () => {
          listeners.delete(listener);
        };
      },
    };
    pendingStores.set(queryClient, store);
  }
  return store;
}
function setPending(
  store: PendingStore,
  serverId: string,
  visibility: McpServerVisibility | null,
): void {
  if (visibility === null) store.pending.delete(serverId);
  else store.pending.set(serverId, visibility);
  for (const listener of store.listeners) listener();
}

/**
 * Remote and unproxied servers only switch on and off ("Availability");
 * hosted and tunneled servers, once on, also choose who may connect
 * ("Visibility": private or public). Off is stored as visibility "disabled"
 * for every kind.
 */
export function isOnOffServer(mcpServer: McpServer): boolean {
  return !standsInForToolset(mcpServer) && !mcpServer.tunneledMcpServerId;
}

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
    dotClass: "bg-[var(--fill-destructive-default)]",
    hoverDotClass: "group-hover:bg-[var(--fill-destructive-default)]",
  },
  {
    value: "private",
    label: "Private",
    description: "The server serves traffic.",
    dotClass: "bg-[var(--fill-success-default)]",
    hoverDotClass: "group-hover:bg-[var(--fill-success-default)]",
  },
];

// Public visibility is offered for hosted servers, and for tunneled-backed
// servers once the tunnel source owner has consented (double opt-in).
const PUBLIC_VISIBILITY_OPTION: VisibilityOption = {
  value: "public",
  label: "Public",
  description:
    "Anyone can connect anonymously — no login. Every tool is exposed to the public internet.",
  dotClass: "bg-[var(--fill-success-default)]",
  hoverDotClass: "group-hover:bg-[var(--fill-success-default)]",
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
  /**
   * The visibility to show: the requested one while an update is in flight,
   * so the control answers at once, then the stored one.
   */
  displayVisibility: McpServerVisibility;
} {
  const { hasScope } = useRBAC();
  // Checked on this server, like the API does; a hosted server is granted
  // under its toolset id.
  const hasWriteScope = hasScope("mcp:write", server.toolsetId ?? server.id);
  const canWrite =
    hasWriteScope &&
    isKnownEnumValue(KnownUpdateMcpServerFormVisibility, server.visibility);
  const queryClient = useQueryClient();
  const pendingStore = pendingStoreFor(queryClient);
  const client = useSdkClient();
  const telemetry = useTelemetry();
  const followRenamedServer = useFollowRenamedServer();
  const [fetchingLatest, setFetchingLatest] = useState(false);
  const requested = useSyncExternalStore(
    pendingStore.subscribe,
    () => pendingStore.pending.get(server.id) ?? null,
  );
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
    // The page has the saved state by now (onSuccess refetched it), so the
    // shared optimistic value can go.
    onSettled: () => setPending(pendingStore, server.id, null),
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
    setPending(pendingStore, server.id, next);
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
          setPending(pendingStore, server.id, null);
          return;
        }
        update.mutate({
          request: {
            updateMcpServerForm: mcpServerVisibilityUpdateForm(latest, next),
          },
        });
      } catch (error) {
        setPending(pendingStore, server.id, null);
        handleError(error, { title: "Couldn't update server availability" });
      } finally {
        setFetchingLatest(false);
      }
    })();
  };

  // Any control for this server shows a change in flight, whichever
  // control started it.
  const updating = fetchingLatest || update.isPending || requested !== null;
  return {
    hasWriteScope,
    canWrite,
    updateVisibility,
    updating,
    displayVisibility: requested ?? server.visibility,
  };
}
