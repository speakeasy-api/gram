import { useProject } from "@/contexts/Auth";
import { useAiDetections } from "@gram/client/react-query/aiDetections.js";
import { useShadowMCPInventory } from "@gram/client/react-query/shadowMCPInventory.js";
import {
  countUndecidedAITools,
  countUndecidedMCPServers,
} from "./undecided-tools";

// The inventory read is paged; one page is what the count sees. When more
// pages exist the count is reported as a floor, never as the whole backlog.
const INVENTORY_PAGE_LIMIT = 50;

export type UndecidedToolCount = {
  aiTools: number;
  mcpServers: number;
  total: number;
  isPending: boolean;
  /** Either read failed: the counts are not a zero, they are unknown. */
  isError: boolean;
  /** The inventory has pages beyond the one counted. */
  truncated: boolean;
};

/** How many detected AI tools and MCP servers are still waiting on an access
 *  decision. Mount the caller only where the count is wanted: both reads run
 *  as soon as the hook does. */
export function useUndecidedToolCount(): UndecidedToolCount {
  const project = useProject();
  const detectionsQuery = useAiDetections(undefined, undefined, {
    throwOnError: false,
  });
  const inventoryQuery = useShadowMCPInventory(
    { projectId: project.id, limit: INVENTORY_PAGE_LIMIT },
    undefined,
    { throwOnError: false },
  );
  const aiTools = countUndecidedAITools(detectionsQuery.data?.detections ?? []);
  const mcpServers = countUndecidedMCPServers(
    inventoryQuery.data?.servers ?? [],
  );
  return {
    aiTools,
    mcpServers,
    total: aiTools + mcpServers,
    isPending: detectionsQuery.isPending || inventoryQuery.isPending,
    isError: detectionsQuery.isError || inventoryQuery.isError,
    truncated: !!inventoryQuery.data?.nextCursor,
  };
}
