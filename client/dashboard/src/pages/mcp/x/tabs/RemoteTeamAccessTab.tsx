import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useMemo, type JSX } from "react";
import { useServerTools } from "@/pages/access/useServerTools";
import { MCPTeamAccessTab } from "../../MCPTeamAccessTab";

/** Reuse the role editor's stored-first, write-gated remote tool discovery. */
export function RemoteTeamAccessTab({
  server,
}: {
  server: McpServer;
}): JSX.Element {
  const entry = useMemo(
    () => ({
      projectId: server.projectId,
      projectName: "",
      server: {
        id: server.id,
        name: server.name ?? server.slug ?? server.id,
        slug: server.slug ?? server.id,
        tools: [],
        dynamicTools: true,
        remoteBacked: true,
      },
    }),
    [server],
  );
  const source = useServerTools(entry);
  return (
    <MCPTeamAccessTab
      resourceId={server.id}
      serverName={server.name ?? undefined}
      toolSource={source}
    />
  );
}
