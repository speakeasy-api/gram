import type { MetaMcpServer } from "@gram/client/models/components/metamcpserver.js";
import {
  ServedSkillsSection,
  SkillsOverMcpTabRoute,
} from "@/pages/skills/ServedSkillsSection";
import { useRoutes } from "@/routes";
import { gatewayTabHref } from "./GatewayDetailsRouting";

/**
 * The gateway Skills tab: the skills agents load over MCP when they connect
 * to this gateway. Shown only while skills over MCP is on.
 */
export function GatewaySkillsTab({
  metaMcpServer,
}: {
  metaMcpServer: Pick<MetaMcpServer, "id" | "name">;
}): JSX.Element {
  const routes = useRoutes();
  return (
    <SkillsOverMcpTabRoute
      fallbackHref={gatewayTabHref(routes, metaMcpServer.id, "overview")}
    >
      <ServedSkillsSection
        target={{ kind: "meta_mcp_server", metaMcpServerId: metaMcpServer.id }}
        surfaceName={metaMcpServer.name}
      />
    </SkillsOverMcpTabRoute>
  );
}
