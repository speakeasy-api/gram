import { RequireScope } from "@/components/require-scope";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { RadioCard, RadioCardGroup } from "@/components/ui/RadioCard";
import { Text } from "@/components/ui/Text";
import { formatTunneledMcpDisplay } from "@/lib/sources";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type {
  ConnectionStatus,
  TunneledMcpServer,
} from "@gram/client/models/components/tunneledmcpserver.js";
import { Trash2 } from "lucide-react";

const CONNECTION_LABEL: Record<ConnectionStatus, string> = {
  connected: "Agent connected",
  inactive: "Agent offline",
  never_connected: "Agent never connected",
};

function connectionBadgeVariant(
  status: ConnectionStatus,
): "success" | "neutral" {
  return status === "connected" ? "success" : "neutral";
}

function serverCountLabel(count: number): string {
  if (count === 0) return "No MCP servers you can view";
  if (count === 1) return "1 MCP server you can view";
  return `${count} MCP servers you can view`;
}

export type ExistingTunnelOption = {
  tunnel: TunneledMcpServer;
  /** MCP servers on the tunnel that the caller can view. */
  servers: McpServer[];
  /**
   * Why the tunnel cannot be chosen here, e.g. the target gateway already
   * includes an MCP server on it.
   */
  unavailableReason?: string;
};

/**
 * The project's tunnels, to add another MCP server to one of them. Shows the
 * key prefix and connection state but never a key. A tunnel no visible MCP
 * server uses offers a delete, which the backend refuses if it is in use.
 */
export function ExistingTunnelPicker({
  options,
  selectedId,
  disabled,
  onSelect,
  onDeleteUnused,
}: {
  options: ExistingTunnelOption[];
  selectedId: string | null;
  disabled: boolean;
  onSelect: (tunneledMcpServerId: string) => void;
  onDeleteUnused: (tunnel: TunneledMcpServer) => void;
}): JSX.Element {
  return (
    <RadioCardGroup
      value={selectedId}
      onValueChange={onSelect}
      disabled={disabled}
      aria-label="Tunnel"
      size="sm"
    >
      {options.map(({ tunnel, servers, unavailableReason }) => (
        <RadioCard
          key={tunnel.id}
          value={tunnel.id}
          disabled={unavailableReason !== undefined}
          title={formatTunneledMcpDisplay(tunnel)}
          detail={
            <div className="flex flex-wrap items-center gap-2">
              <Badge variant={connectionBadgeVariant(tunnel.connectionStatus)}>
                <Badge.Text>
                  {CONNECTION_LABEL[tunnel.connectionStatus]}
                </Badge.Text>
              </Badge>
              <Text small muted mono>
                {tunnel.keyPrefix}
              </Text>
              <Text small muted>
                {serverCountLabel(servers.length)}
              </Text>
              {unavailableReason ? (
                <Text small muted>
                  {unavailableReason}
                </Text>
              ) : null}
            </div>
          }
          trailing={
            servers.length === 0 ? (
              <RequireScope
                scope="mcp:write"
                resourceId={tunnel.projectId}
                projectId={tunnel.projectId}
                level="component"
              >
                <Button
                  type="button"
                  variant="tertiary"
                  size="sm"
                  disabled={disabled}
                  onClick={() => onDeleteUnused(tunnel)}
                >
                  <Button.LeftIcon>
                    <Trash2 className="h-4 w-4" />
                  </Button.LeftIcon>
                  <Button.Text>Delete tunnel</Button.Text>
                </Button>
              </RequireScope>
            ) : undefined
          }
        />
      ))}
    </RadioCardGroup>
  );
}
