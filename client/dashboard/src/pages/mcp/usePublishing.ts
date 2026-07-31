import { useSdkClient } from "@/contexts/Sdk";
import {
  useAttachServer,
  useCollections,
  useDetachServer,
} from "@/pages/collections/hooks";
import type { Collection } from "@/pages/collections/types";
import type { AttachServerRequestBody } from "@gram/client/models/components/attachserverrequestbody.js";
import type { ExternalMCPServer } from "@gram/client/models/components/externalmcpserver.js";
import { buildCollectionsListServersQuery } from "@gram/client/react-query/collectionsListServers.js";
import { useQueries } from "@tanstack/react-query";
import { useMemo, useState } from "react";

// PublishingTarget identifies the mcp_server being published. A toolset-backed
// wrapper also carries its backing toolset id so attachments recorded against
// the toolset before the mcp_servers cutover still read as published; writes
// always key on mcp_server_id (the backend resolves either key to the same
// attachment row).
export type PublishingTarget = {
  kind: "mcpServer";
  mcpServerId: string;
  backingToolsetId?: string;
};

function serverMatchesTarget(
  server: ExternalMCPServer,
  target: PublishingTarget,
): boolean {
  if (server.mcpServerId === target.mcpServerId) {
    return true;
  }
  return (
    !!target.backingToolsetId && server.toolsetId === target.backingToolsetId
  );
}

function attachBodyForTarget(
  collectionId: string,
  target: PublishingTarget,
): AttachServerRequestBody {
  return { collectionId, mcpServerId: target.mcpServerId };
}

export type UsePublishingResult = {
  collections: Collection[];
  effectiveSelected: Set<string>;
  hasChanges: boolean;
  isSaving: boolean;
  isLoading: boolean;
  toggleCollection: (collectionId: string) => void;
  handleSave: () => Promise<void>;
  handleDiscard: () => void;
};

// usePublishing owns the collection-publishing logic: which collections already
// serve this target, the user's pending selection, and committing attach/detach
// mutations. It deliberately holds no chrome so a host page can render its own
// section/footer. Callers that aren't org:admin should still gate the
// surrounding UI, since attach/detach authorize as org:admin.
export function usePublishing(target: PublishingTarget): UsePublishingResult {
  const client = useSdkClient();
  const { data: collections, isLoading: collectionsLoading } = useCollections();
  const attachServer = useAttachServer();
  const detachServer = useDetachServer();
  const [selectedIds, setSelectedIds] = useState<Set<string> | null>(null);
  const [isSaving, setIsSaving] = useState(false);

  const serveQueries = useQueries({
    queries: collections.map((collection) => ({
      ...buildCollectionsListServersQuery(client, {
        collectionSlug: collection.slug!,
      }),
      enabled: !!collection.slug,
    })),
  });

  const publishedCollectionIds = useMemo(() => {
    const ids = new Set<string>();

    for (let i = 0; i < collections.length; i++) {
      const servers = serveQueries[i]?.data?.servers ?? [];
      for (const server of servers) {
        if (serverMatchesTarget(server, target)) {
          ids.add(collections[i]!.id!);
          break;
        }
      }
    }

    return ids;
  }, [collections, serveQueries, target]);

  const effectiveSelected = selectedIds ?? publishedCollectionIds;

  const hasChanges = useMemo(() => {
    if (!selectedIds) return false;
    if (selectedIds.size !== publishedCollectionIds.size) return true;
    for (const id of selectedIds) {
      if (!publishedCollectionIds.has(id)) return true;
    }
    return false;
  }, [selectedIds, publishedCollectionIds]);

  const toggleCollection = (collectionId: string) => {
    setSelectedIds((prev) => {
      const current = prev ?? new Set(publishedCollectionIds);
      const next = new Set(current);
      if (next.has(collectionId)) {
        next.delete(collectionId);
      } else {
        next.add(collectionId);
      }
      return next;
    });
  };

  const handleSave = async () => {
    if (!selectedIds) return;

    setIsSaving(true);
    try {
      const toAttach = [...selectedIds].filter(
        (id) => !publishedCollectionIds.has(id),
      );
      const toDetach = [...publishedCollectionIds].filter(
        (id) => !selectedIds.has(id),
      );

      await Promise.all([
        ...toAttach.map((collectionId) =>
          attachServer.mutateAsync({
            request: {
              attachServerRequestBody: attachBodyForTarget(
                collectionId,
                target,
              ),
            },
          }),
        ),
        ...toDetach.map((collectionId) =>
          detachServer.mutateAsync({
            request: {
              attachServerRequestBody: attachBodyForTarget(
                collectionId,
                target,
              ),
            },
          }),
        ),
      ]);

      setSelectedIds(null);
    } finally {
      setIsSaving(false);
    }
  };

  const handleDiscard = () => {
    setSelectedIds(null);
  };

  const isLoading =
    collectionsLoading || serveQueries.some((query) => query.isLoading);

  return {
    collections,
    effectiveSelected,
    hasChanges,
    isSaving,
    isLoading,
    toggleCollection,
    handleSave,
    handleDiscard,
  };
}
