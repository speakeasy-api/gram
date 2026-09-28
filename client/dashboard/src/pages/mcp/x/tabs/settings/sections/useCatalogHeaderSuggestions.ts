import {
  headerDraftFromCatalog,
  type HeaderDraft,
} from "@/lib/remote-identity";
import { type PulseMCPServer, useListMCPCatalog } from "@/pages/catalog/hooks";
import { catalogHeadersForRemoteUrl } from "@/pages/catalog/remotes";
import { useGetRemoteMcpServer } from "@gram/client/react-query/getRemoteMcpServer.js";
import { useMemo } from "react";

/**
 * Header rows the MCP catalog says this endpoint wants.
 *
 * When a remote's URL matches a catalog entry that publishes header
 * requirements — an API key, usually — an unconfigured server can start from
 * those rows and only have to fill in values. The catalog is a dashboard
 * concern, so the lookup stays here and the rows are handed to the draft hook
 * as plain suggestions.
 */
export function useCatalogHeaderSuggestions(
  remoteMcpServerId: string,
  enabled: boolean,
): readonly HeaderDraft[] {
  const { data: remoteMcpServer } = useGetRemoteMcpServer(
    { id: remoteMcpServerId },
    undefined,
    { enabled: enabled && remoteMcpServerId !== "" },
  );
  const { data: catalogData } = useListMCPCatalog(undefined, undefined, {
    enabled,
  });

  return useMemo(() => {
    if (!remoteMcpServer?.url || !catalogData?.servers) return [];
    return catalogHeadersForRemoteUrl(
      catalogData.servers as PulseMCPServer[],
      remoteMcpServer.url,
    ).map(headerDraftFromCatalog);
  }, [catalogData?.servers, remoteMcpServer?.url]);
}
