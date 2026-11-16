import { isNotNotFound } from "@/lib/query-errors";
import { isNotFoundError } from "@/lib/route-errors";
import { getMcpServerArgs } from "@/lib/sources";
import { useGetMcpServer } from "@gram/client/react-query/getMcpServer.js";
import { useParams } from "react-router";

/** True once the route's server lookup 404s (a legacy path the page is resolving). */
export function useMcpServerRouteMissing(enabled: boolean): boolean {
  const { mcpServerSlug = "" } = useParams<{ mcpServerSlug: string }>();
  const { error } = useGetMcpServer(
    getMcpServerArgs(mcpServerSlug),
    undefined,
    {
      enabled: enabled && mcpServerSlug !== "",
      throwOnError: isNotNotFound,
    },
  );
  return enabled && isNotFoundError(error);
}
