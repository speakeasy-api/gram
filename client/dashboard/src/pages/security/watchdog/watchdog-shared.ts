import { defineFilters } from "@/components/filters";
import { type DateRangePreset } from "@/elements";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { useListToolsets } from "@gram/client/react-query/listToolsets.js";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import { useMetaMcpServers } from "@gram/client/react-query/metaMcpServers.js";
import { useMemo } from "react";
import {
  buildMCPFindingNames,
  type MCPFindingNames,
} from "../mcp-finding-context";

export const WATCHDOG_PRESETS: DateRangePreset[] = ["1d", "7d", "30d"];

// A separate schema keeps the server dimension out of the always-on filters:
// it renders only when MCP-scoped guardrails are enabled. Shared with the
// signal detail page so both read the same URL param.
export const WATCHDOG_SERVER_FILTERS = defineFilters([
  { id: "mcp_server_id", label: "MCP server", kind: "select", pinned: true },
]);

/** Server and toolset names that MCP findings render with. */
export function useMCPFindingNames(): {
  names: MCPFindingNames;
  mcpServers:
    | NonNullable<ReturnType<typeof useMcpServers>["data"]>["mcpServers"]
    | undefined;
} {
  const gramProject = useProjectSlugForRequests();
  const { data: mcpServersData } = useMcpServers({ gramProject }, undefined, {
    throwOnError: false,
  });
  const { data: metaMcpServersData } = useMetaMcpServers(
    { gramProject },
    undefined,
    { throwOnError: false },
  );
  const { data: toolsetsData } = useListToolsets({ gramProject }, undefined, {
    throwOnError: false,
  });
  const mcpServers = mcpServersData?.mcpServers;
  const names = useMemo(
    () =>
      buildMCPFindingNames(
        [...(mcpServers ?? []), ...(metaMcpServersData?.metaMcpServers ?? [])],
        toolsetsData?.toolsets ?? [],
      ),
    [mcpServers, metaMcpServersData?.metaMcpServers, toolsetsData?.toolsets],
  );
  return { names, mcpServers };
}
