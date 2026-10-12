import { StatTile } from "@/components/chart/stat-tile";
import { pluralize } from "@/lib/format";
import { useRoutes } from "@/routes";
import { useUndecidedToolCount } from "../shadow-ai/use-undecided-tool-count";

const UNDECIDED_TOOLS_TOOLTIP =
  "Harnesses and assistants the device agent found, and MCP servers seen in agent traffic, that nobody has approved or disallowed yet. Decide in the Shadow AI policy under Guardrails.";

/** The Watchdog tile for the Shadow AI backlog. Rendered only while the
 *  Shadow AI fold-in is on, so its reads run only where they are shown. */
export function UndecidedToolsTile(): JSX.Element {
  const routes = useRoutes();
  const undecided = useUndecidedToolCount();
  return (
    <StatTile
      title="AI tools without a decision"
      tooltip={UNDECIDED_TOOLS_TOOLTIP}
      value={undecided.total}
      format="number"
      tone={undecided.total > 0 ? "warning" : "neutral"}
      icon="laptop"
      isRefreshing={undecided.isPending}
      subtext={`${pluralize(undecided.aiTools, "tool")}, ${pluralize(undecided.mcpServers, "MCP server")}`}
      link={routes.policyCenter.href()}
      linkText="Review"
    />
  );
}
