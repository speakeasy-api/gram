import { Badge } from "@/components/ui/Badge";
import { McpEntityCard } from "./McpEntityCard";
import { Text } from "@/components/ui/Text";
import { McpIconGroup } from "./McpIconGroup";
import { useRoutes } from "@/routes";
import type { MetaMcpServer } from "@gram/client/models/components/metamcpserver.js";
import { useMetaMcpMembers } from "@gram/client/react-query/metaMcpMembers.js";
import { ArrowRight } from "lucide-react";

// The gateway's cargo is its member servers, so the icon rail shows their
// logos (up to four, then a +N tile) instead of a generic glyph.
/**
 * The servers a gateway puts behind one URL, named.
 *
 * A gateway has no description of its own, and its name rarely says what is
 * inside it — which is the only question worth answering on a card.
 */
function GatewayMemberSummary({
  metaMcpServerId,
}: {
  metaMcpServerId: string;
}): JSX.Element | null {
  const { data } = useMetaMcpMembers({ metaMcpServerId }, undefined, {
    throwOnError: false,
    staleTime: 60 * 1000,
  });
  const members = data?.members ?? [];
  if (members.length === 0) return null;

  const named = members.map(
    (member) => member.mcpServerName || member.mcpServerSlug || "Untitled",
  );
  const shown = named.slice(0, 2);
  const rest = named.length - shown.length;
  const text =
    rest > 0 ? `${shown.join(", ")} and ${rest} more` : shown.join(", ");

  return (
    <Text small muted className="mt-1 truncate" title={named.join(", ")}>
      Fronts {text}
    </Text>
  );
}

function GatewayMemberIcons({
  metaMcpServerId,
}: {
  metaMcpServerId: string;
}): JSX.Element {
  const { data } = useMetaMcpMembers({ metaMcpServerId }, undefined, {
    throwOnError: false,
    staleTime: 60 * 1000,
  });
  const members = data?.members ?? [];

  return (
    <McpIconGroup serverIds={members.map((member) => member.mcpServerId)} />
  );
}

// GatewayCard renders a meta MCP server (Gateway Endpoint) inside the /mcp
// listing grid, alongside MCPCard (toolsets) and MCPServerCard (mcp_servers).
export function GatewayCard({
  gateway,
}: {
  gateway: MetaMcpServer;
}): JSX.Element {
  const routes = useRoutes();

  return (
    <McpEntityCard
      className="focus-visible:ring-ring cursor-pointer focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none"
      onClick={() => routes.mcp.gateway.overview.goTo(gateway.id)}
      icon={<GatewayMemberIcons metaMcpServerId={gateway.id} />}
    >
      <div className="mb-2 flex items-start justify-between gap-2">
        <Text
          variant="subheading"
          as="div"
          className="text-md group-hover:text-primary flex-1 truncate transition-colors"
          title={gateway.name}
        >
          {gateway.name}
        </Text>
      </div>

      <GatewayMemberSummary metaMcpServerId={gateway.id} />

      <div className="mt-auto flex items-center justify-between gap-2 pt-2">
        <div className="flex items-center gap-2">
          <Badge variant="neutral">
            <Badge.Text>Gateway</Badge.Text>
          </Badge>
        </div>
        <div className="text-muted-foreground group-hover:text-primary flex items-center gap-1 text-sm transition-colors">
          <span>Open</span>
          <ArrowRight className="h-3.5 w-3.5" />
        </div>
      </div>
    </McpEntityCard>
  );
}
