import { Text } from "@/components/ui/Text";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import { Badge } from "@/components/ui/Badge";
import { Icon } from "@/components/ui/Icon";

function countLabel(count: number, singular: string, plural: string): string {
  return `${count} ${count === 1 ? singular : plural}`;
}

function shadowMCPInventoryServerLabel(
  server: ShadowMCPInventoryServer,
): string {
  return server.serverName || server.urlHost;
}

export function ShadowMCPInventoryServerCell({
  server,
}: {
  server: ShadowMCPInventoryServer;
}): JSX.Element {
  return (
    <div className="min-w-0 space-y-1">
      <div className="flex items-center gap-2">
        <Text variant="small" className="truncate font-medium">
          {shadowMCPInventoryServerLabel(server)}
        </Text>
        {server.requestCount > 0 && (
          <Badge variant="warning" size="sm" background={false}>
            <Badge.LeftIcon>
              <Icon name="shield-alert" />
            </Badge.LeftIcon>
            <Badge.Text>
              {server.requestCount} Access Request
              {server.requestCount > 1 && "s"}
            </Badge.Text>
          </Badge>
        )}
      </div>
      <Text
        muted
        small
        className="truncate text-xs"
        title={server.canonicalServerUrl}
      >
        {server.canonicalServerUrl}
      </Text>
    </div>
  );
}

export function ShadowMCPInventoryUsageCell({
  server,
}: {
  server: ShadowMCPInventoryServer;
}): JSX.Element {
  return (
    <div className="space-y-1">
      <Text variant="small">
        {countLabel(server.observedUseCount, "call", "calls")}
      </Text>
      <Text muted small className="text-xs">
        {countLabel(server.userCount, "user", "users")}
      </Text>
    </div>
  );
}
