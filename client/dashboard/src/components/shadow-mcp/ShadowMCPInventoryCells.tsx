import { Type } from "@/components/ui/type";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import { Badge, Icon } from "@speakeasy-api/moonshine";

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
        <Type variant="small" className="truncate font-medium">
          {shadowMCPInventoryServerLabel(server)}
        </Type>
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
      <Type muted small className="truncate text-xs">
        {server.canonicalServerUrl}
      </Type>
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
      <Type variant="small">
        {countLabel(server.observedUseCount, "call", "calls")}
      </Type>
      <Type muted small className="text-xs">
        {countLabel(server.userCount, "user", "users")}
      </Type>
    </div>
  );
}
