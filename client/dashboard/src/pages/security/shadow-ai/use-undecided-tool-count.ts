import { useProject } from "@/contexts/Auth";
import { useAiDetections } from "@gram/client/react-query/aiDetections.js";
import { useShadowMCPInventory } from "@gram/client/react-query/shadowMCPInventory.js";
import {
  countUndecidedAITools,
  countUndecidedMCPServers,
} from "./undecided-tools";

// The inventory read is paged; one page is what the count sees. A project
// with more undecided servers than this reads as "at least a page", which
// is still the right call to action.
const INVENTORY_PAGE_LIMIT = 50;

export type UndecidedToolCount = {
  aiTools: number;
  mcpServers: number;
  total: number;
  isPending: boolean;
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
  };
}
