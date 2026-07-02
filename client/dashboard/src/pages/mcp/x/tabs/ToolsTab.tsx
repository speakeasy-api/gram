import { useResolvedMcpServerUrl } from "@/hooks/useToolsetUrl";
import type { McpEndpoint } from "@gram/client/models/components/mcpendpoint.js";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { RemoteMcpToolsSection } from "./RemoteMcpToolsSection";

type ToolsTabProps = {
  mcpServer: McpServer;
  endpoints: McpEndpoint[];
  isLoadingEndpoints: boolean;
};

export function ToolsTab({
  mcpServer,
  endpoints,
  isLoadingEndpoints,
}: ToolsTabProps): JSX.Element {
  const { mcpUrl, loading } = useResolvedMcpServerUrl(
    endpoints,
    isLoadingEndpoints,
  );

  return (
    <div className="mx-auto w-full max-w-[1270px] px-8 py-8">
      <RemoteMcpToolsSection
        mcpUrl={mcpUrl}
        isResolvingUrl={loading}
        mcpServerId={mcpServer.id}
        userSessionIssuerId={mcpServer.userSessionIssuerId}
      />
    </div>
  );
}
