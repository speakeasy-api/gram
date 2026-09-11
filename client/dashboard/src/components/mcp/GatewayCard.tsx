import { Badge } from "@/components/ui/Badge";
import { useIconConfetti } from "@/components/icon-confetti";
import { Card } from "@/components/ui/Card";
import { Text } from "@/components/ui/Text";
import { SourceMcpIcon } from "@/components/sources/SourceCard";
import { useRoutes } from "@/routes";
import type { MetaMcpServer } from "@gram/client/models/components/metamcpserver.js";
import { useMetaMcpMembers } from "@gram/client/react-query/metaMcpMembers.js";
import { ArrowRight, Network } from "lucide-react";

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

  if (members.length === 0) {
    return <Network className="text-muted-foreground h-8 w-8" />;
  }
  const only = members[0];
  if (members.length === 1 && only) {
    return (
      <SourceMcpIcon
        mcpServerId={only.mcpServerId}
        className="h-8 w-8 object-contain"
      />
    );
  }

  const overflow = members.length > 4 ? members.length - 3 : 0;
  const shown = members.slice(0, overflow > 0 ? 3 : 4);
  return (
    <div className="grid grid-cols-2 gap-1.5">
      {shown.map((member) => (
        <SourceMcpIcon
          key={member.id}
          mcpServerId={member.mcpServerId}
          className="h-6 w-6 object-contain"
        />
      ))}
      {overflow > 0 && (
        <Text
          muted
          as="div"
          className="flex h-6 w-6 items-center justify-center font-mono text-[10px]"
        >
          +{overflow}
        </Text>
      )}
    </div>
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
  const { canvasRef, start, stop } = useIconConfetti();

  return (
    <div onMouseEnter={start} onMouseLeave={stop} className="h-full">
      {/* Clickable div rather than a link, as MCPCard is: an <a> may not nest
          the copy button below. */}
      <Card.Entity
        className="focus-visible:ring-ring cursor-pointer focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none"
        onClick={() => routes.mcp.gateway.overview.goTo(gateway.id)}
        iconRailClassName="isolate"
        iconTileClassName="icon-hover-pulse"
        overlay={
          <canvas
            ref={canvasRef}
            aria-hidden="true"
            className="pointer-events-none absolute inset-0 -z-10 size-full"
          />
        }
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
      </Card.Entity>
    </div>
  );
}
