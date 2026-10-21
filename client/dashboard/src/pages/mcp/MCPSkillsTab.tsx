import { Text } from "@/components/ui/Text";
import {
  ServedSkillsSection,
  SkillsOverMcpTabRoute,
} from "@/pages/skills/ServedSkillsSection";
import { useRoutes } from "@/routes";
import { mcpDetailTabHref } from "./MCPDetailsRouting";

type SkillsTabToolset = {
  id: string;
  slug: string;
  name: string;
  mcpEnabled?: boolean | undefined;
};

/**
 * The route behind the MCP server Skills tab: only while skills over MCP is
 * on, and only for readers of this project's skills.
 */
export function MCPSkillsTabRoute({
  toolset,
}: {
  toolset: SkillsTabToolset;
}): JSX.Element {
  const routes = useRoutes();
  return (
    <SkillsOverMcpTabRoute
      fallbackHref={mcpDetailTabHref(routes, toolset.slug, "overview")}
    >
      <MCPSkillsTab toolset={toolset} />
    </SkillsOverMcpTabRoute>
  );
}

/**
 * The skills agents load when they connect to this hosted MCP server. They
 * are the skills linked to the toolset's own server, whose id is the toolset
 * id; other servers built on the same toolset keep their own links.
 */
export function MCPSkillsTab({
  toolset,
}: {
  toolset: SkillsTabToolset;
}): JSX.Element {
  return (
    <ServedSkillsSection
      target={{ kind: "mcp_server", mcpServerId: toolset.id }}
      grantResourceId={toolset.id}
      surfaceName={toolset.name}
      notice={
        toolset.mcpEnabled === false ? (
          <Text small className="text-warning mb-3 block">
            This server is disabled, so no agent can load these skills until you
            enable it.
          </Text>
        ) : null
      }
    />
  );
}
