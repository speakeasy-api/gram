import { StatusBadge } from "@/components/mcp-approvals/EvidencePanel";
import { Text } from "@/components/ui/Text";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import { Badge } from "@/components/ui/Badge";
import { Icon } from "@/components/ui/Icon";
import { cn } from "@/lib/utils";

function countLabel(count: number, singular: string, plural: string): string {
  return `${count} ${count === 1 ? singular : plural}`;
}

function shadowMCPInventoryServerLabel(
  server: ShadowMCPInventoryServer,
): string {
  return server.serverName || server.urlHost || server.canonicalServerUrl;
}

export function ShadowMCPInventoryServerCell({
  server,
}: {
  server: ShadowMCPInventoryServer;
}): JSX.Element {
  if (server.targetKind === "stdio_command") {
    return (
      <div className="min-w-0 space-y-1">
        <Text
          variant="small"
          className="truncate font-mono font-medium"
          title={server.canonicalServerUrl}
        >
          {server.canonicalServerUrl}
        </Text>
        <Text muted small className="text-xs">
          Local command — known only from its access request
        </Text>
      </div>
    );
  }

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

/**
 * The review state a row carries: its status badge plus how many people are
 * waiting when a decision is pending. A dash for rows the review system has
 * not touched — observed traffic with no dossier yet.
 */
export function ShadowMCPInventoryReviewCell({
  server,
}: {
  server: ShadowMCPInventoryServer;
}): JSX.Element {
  const request = server.approvalRequest;
  // An unreviewed dossier holds evidence but is not a review state: nobody
  // asked and nothing was decided, so it reads the same as no review.
  if (!request || request.status === "unreviewed") {
    return (
      <Text muted small>
        —
      </Text>
    );
  }

  return (
    <div className="space-y-1">
      <StatusBadge status={request.status} />
      {request.status === "requested" && request.requesterCount > 0 && (
        <Text muted small className="text-xs">
          {countLabel(request.requesterCount, "person", "people")} waiting
        </Text>
      )}
    </div>
  );
}

export function ShadowMCPInventoryUsageCell({
  server,
}: {
  server: ShadowMCPInventoryServer;
}): JSX.Element {
  // No per-row trend series is available on ShadowMCPInventoryServer (only
  // aggregate counts), so there is no sparkline here. The 2px destructive left
  // edge flags servers with pending access requests (signal-row idiom).
  return (
    <div
      className={cn(
        "space-y-1",
        server.requestCount > 0 && "border-destructive-default border-l-2 pl-2",
      )}
    >
      <Text variant="small">
        {countLabel(server.observedUseCount, "call", "calls")}
      </Text>
      <Text muted small className="text-xs">
        {countLabel(server.userCount, "user", "users")}
      </Text>
    </div>
  );
}
