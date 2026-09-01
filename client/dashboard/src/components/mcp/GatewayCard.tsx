import { Badge } from "@/components/ui/Badge";
import { Card } from "@/components/ui/Card";
import { CopyButton } from "@/components/ui/CopyButton";
import { Text } from "@/components/ui/Text";
import { SourceMcpIcon } from "@/components/sources/SourceCard";
import { useRoutes } from "@/routes";
import type { MetaMcpServer } from "@gram/client/models/components/metamcpserver.js";
import { useMetaMcpMembers } from "@gram/client/react-query/metaMcpMembers.js";
import { ArrowRight, Network } from "lucide-react";
import { GatewayStatusIndicator } from "./MCPStatusIndicator";

// The gateway's cargo is its member servers, so the icon rail shows their
// logos (up to four, then a +N tile) instead of a generic glyph.
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
  url,
}: {
  gateway: MetaMcpServer;
  /** Canonical address, when the gateway has one. */
  url: string | undefined;
}): JSX.Element {
  const routes = useRoutes();

  return (
    // Clickable div rather than a link, as MCPCard is: an <a> may not nest the
    // copy button below.
    <Card.Entity
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
        <Badge variant="neutral" className="bg-card">
          <Badge.Text>
            {`${gateway.memberCount ?? 0} ${gateway.memberCount === 1 ? "member" : "members"}`}
          </Badge.Text>
        </Badge>
      </div>

      {url ? (
        <div
          className="flex items-center gap-1"
          // Card.Entity turns Enter/Space into navigation; leave them to the
          // copy button when it holds focus.
          onKeyDown={(e) => e.stopPropagation()}
        >
          <Text muted className="truncate font-mono text-xs">
            {url.replace(/^https?:\/\//, "")}
          </Text>
          <CopyButton text={url} size="xs" tooltip="Copy URL" />
        </div>
      ) : (
        <Text muted className="text-xs">
          No address yet
        </Text>
      )}

      <div className="mt-auto flex items-center justify-between gap-2 pt-2">
        <div className="flex items-center gap-2">
          <Badge variant="neutral">
            <Badge.Text>Gateway</Badge.Text>
          </Badge>
          <GatewayStatusIndicator
            visibility={gateway.visibility}
            requiresSignIn={!!gateway.userSessionIssuerId}
          />
        </div>
        <div className="text-muted-foreground group-hover:text-primary flex items-center gap-1 text-sm transition-colors">
          <span>Open</span>
          <ArrowRight className="h-3.5 w-3.5" />
        </div>
      </div>
    </Card.Entity>
  );
}
