import { StatTile, type StatTileProps } from "@/components/chart/stat-tile";
import { pluralize } from "@/lib/format";
import { useRoutes } from "@/routes";
import {
  type UndecidedToolCount,
  useUndecidedToolCount,
} from "../shadow-ai/use-undecided-tool-count";

const UNDECIDED_TOOLS_TOOLTIP =
  "Harnesses and assistants the device agent found, and MCP servers seen in agent traffic, that nobody has approved or denied yet. Decide tools in the Shadow AI policy and MCP servers on the MCP approvals tab, both under Guardrails.";

/** What the tile shows for a count: a dash when a read failed (unknown is
 *  not zero), a floor when the inventory has more pages, the number
 *  otherwise. */
function tileFacts(
  undecided: UndecidedToolCount,
): Pick<StatTileProps, "value" | "displayValue" | "subtext" | "tone"> {
  if (undecided.isError) {
    return {
      value: 0,
      displayValue: "–",
      subtext: "could not be loaded",
      tone: "neutral",
    };
  }
  const breakdown = `${pluralize(undecided.aiTools, "tool")}, ${pluralize(undecided.mcpServers, "MCP server")}`;
  return {
    value: undecided.total,
    displayValue: undecided.truncated ? `${undecided.total}+` : undefined,
    subtext: undecided.truncated ? `${breakdown} on the first page` : breakdown,
    tone: undecided.total > 0 ? "warning" : "neutral",
  };
}

/** The Watchdog tile for the Shadow AI backlog. */
export function UndecidedToolsTile(): JSX.Element {
  const routes = useRoutes();
  const undecided = useUndecidedToolCount();
  return (
    <StatTile
      title="AI tools without a decision"
      tooltip={UNDECIDED_TOOLS_TOOLTIP}
      format="number"
      icon="laptop"
      isRefreshing={!undecided.isError && undecided.isPending}
      link={routes.policyCenter.href()}
      linkText="Review"
      {...tileFacts(undecided)}
    />
  );
}
