import { StatusBadge } from "@/components/mcp-approvals/EvidencePanel";
import { Text } from "@/components/ui/Text";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import { Badge } from "@/components/ui/Badge";
import { Icon } from "@/components/ui/Icon";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { cn } from "@/lib/utils";
import {
  isToolNamespaceServer,
  shadowMCPInventoryServerLabel,
  shadowMCPSourceLabels,
  TOOL_NAMESPACE_UNRESOLVED_HINT,
  toolNamespacePattern,
} from "./shadowMCPServerIdentity";

function countLabel(count: number, singular: string, plural: string): string {
  return `${count} ${count === 1 ? singular : plural}`;
}

/**
 * Flags a row whose server URL the inventory has not resolved: an LLM proxy
 * saw it only as a tool namespace, so nothing can enforce against it yet.
 */
export function ShadowMCPUnresolvedIdentityBadge(): JSX.Element {
  return (
    <SimpleTooltip tooltip={TOOL_NAMESPACE_UNRESOLVED_HINT}>
      <Badge variant="warning" size="sm" background={false}>
        <Badge.LeftIcon>
          <Icon name="circle-help" />
        </Badge.LeftIcon>
        <Badge.Text>Identity unresolved</Badge.Text>
      </Badge>
    </SimpleTooltip>
  );
}

/**
 * The hook sources that observed a server ("seen via LiteLLM"), as inline
 * chips so the line sits inside a row cell or a page description alike.
 * Renders nothing for a server known only from its review request.
 */
export function ShadowMCPSources({
  sources,
}: {
  sources: string[] | undefined;
}): JSX.Element | null {
  const labels = shadowMCPSourceLabels(sources);
  if (labels.length === 0) return null;

  return (
    <span className="text-muted-foreground inline-flex flex-wrap items-center gap-1 text-xs">
      <span>seen via</span>
      {labels.map((label) => (
        <Badge key={label} variant="neutral" size="sm" background={false}>
          <Badge.Text>{label}</Badge.Text>
        </Badge>
      ))}
    </span>
  );
}

function AccessRequestBadge({ count }: { count: number }): JSX.Element | null {
  if (count <= 0) return null;

  return (
    <Badge variant="warning" size="sm" background={false}>
      <Badge.LeftIcon>
        <Icon name="shield-alert" />
      </Badge.LeftIcon>
      <Badge.Text>
        {count} Access Request
        {count > 1 && "s"}
      </Badge.Text>
    </Badge>
  );
}

/**
 * What the server cell's two lines say for each kind of row: the primary
 * line names the server, the secondary line says where that name came from.
 */
function serverCellLines(server: ShadowMCPInventoryServer): {
  primary: string;
  primaryClassName?: string;
  secondary: string;
  secondaryClassName?: string;
} {
  switch (server.targetKind) {
    case "stdio_command":
      return {
        primary: server.canonicalServerUrl,
        primaryClassName: "font-mono",
        secondary: "Local command — known only from its access request",
      };
    case "tool_namespace":
      return {
        primary: shadowMCPInventoryServerLabel(server),
        secondary: toolNamespacePattern(server.canonicalServerUrl),
        secondaryClassName: "truncate font-mono",
      };
    case "server_url":
    case undefined:
      return {
        primary: shadowMCPInventoryServerLabel(server),
        secondary: server.canonicalServerUrl,
        secondaryClassName: "truncate",
      };
  }
}

export function ShadowMCPInventoryServerCell({
  server,
}: {
  server: ShadowMCPInventoryServer;
}): JSX.Element {
  const lines = serverCellLines(server);

  return (
    <div className="min-w-0 space-y-1">
      <div className="flex items-center gap-2">
        <Text
          variant="small"
          className={cn("truncate font-medium", lines.primaryClassName)}
          title={lines.primary}
        >
          {lines.primary}
        </Text>
        {isToolNamespaceServer(server) && <ShadowMCPUnresolvedIdentityBadge />}
        <AccessRequestBadge count={server.requestCount} />
      </div>
      <Text
        muted
        small
        className={cn("text-xs", lines.secondaryClassName)}
        title={lines.secondary}
      >
        {lines.secondary}
      </Text>
      <ShadowMCPSources sources={server.sources} />
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
      {request.status === "approved" && request.evidenceChangedAt && (
        <Badge variant="warning" size="sm" background={false}>
          <Badge.LeftIcon>
            <Icon name="shield-alert" />
          </Badge.LeftIcon>
          <Badge.Text>Changed since approval</Badge.Text>
        </Badge>
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
