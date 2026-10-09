import { Alert } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Skeleton } from "@/components/ui/Skeleton";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { Loader2 } from "lucide-react";
import type { ReactNode } from "react";
import {
  PUBLIC_SIBLING_WARNING,
  useSharedTunnelImpact,
  type SharedTunnelImpactState,
} from "./use-shared-tunnel-impact";

function ServerRow({
  server,
  isCurrent,
}: {
  server: McpServer;
  isCurrent: boolean;
}) {
  return (
    <li className="flex items-center gap-2 px-3 py-2">
      <Text small className="font-medium" title={server.id}>
        {server.name || "MCP Server"}
      </Text>
      <Badge variant={server.visibility === "public" ? "warning" : "neutral"}>
        <Badge.Text>{server.visibility}</Badge.Text>
      </Badge>
      {isCurrent ? (
        <Text small muted>
          (this server)
        </Text>
      ) : null}
    </li>
  );
}

function ServerList({
  impact,
  currentMcpServerId,
}: {
  impact: SharedTunnelImpactState;
  currentMcpServerId?: string;
}) {
  if (impact.isError) {
    return (
      <Alert variant="error" dismissible={false}>
        <Stack gap={2}>
          <Text small>
            Could not load the MCP servers on this tunnel. Retry before
            continuing.
          </Text>
          <div>
            <Button variant="secondary" size="sm" onClick={impact.retry}>
              <Button.Text>Retry</Button.Text>
            </Button>
          </div>
        </Stack>
      </Alert>
    );
  }
  if (impact.isLoading) {
    return <Skeleton className="h-16 w-full" />;
  }
  if (impact.servers.length === 0) {
    return (
      <Text small muted>
        No MCP servers you can view use this tunnel.
      </Text>
    );
  }
  return (
    // Bounded so a tunnel with many servers cannot push the confirmation
    // controls below the fold.
    <ul className="divide-border max-h-64 divide-y overflow-y-auto border">
      {impact.servers.map((server) => (
        <ServerRow
          key={server.id}
          server={server}
          isCurrent={server.id === currentMcpServerId}
        />
      ))}
    </ul>
  );
}

/**
 * Explains that a control acts on the tunnel rather than one MCP server, and
 * lists the MCP servers it reaches. Every tunnel-wide setting and action
 * renders this so the shared effect is stated the same way everywhere.
 */
export function SharedTunnelImpact({
  impact,
  tunnelName,
  currentMcpServerId,
  effect,
  publicWarning = false,
  intro,
}: {
  impact: SharedTunnelImpactState;
  /** Named in the default intro; required unless `intro` replaces it. */
  tunnelName?: string;
  currentMcpServerId?: string;
  /** What this particular control changes for every server on the tunnel. */
  effect?: ReactNode;
  /**
   * Show the public-sibling warning even when no listed server is public. Pass
   * the tunnel's `allowPublic`: a public server on it may be one the user
   * cannot view, and without public access none can serve anonymous callers.
   */
  publicWarning?: boolean;
  /**
   * Replaces the default "this belongs to the tunnel" sentence, for a control
   * that changes one server but whose effect reaches the shared upstream.
   */
  intro?: ReactNode;
}): JSX.Element {
  const showPublicWarning =
    publicWarning ||
    impact.servers.some((server) => server.visibility === "public");

  return (
    <Stack gap={2} data-testid="shared-tunnel-impact">
      <Text small>
        {intro ?? (
          <>
            This belongs to the tunnel <strong>{tunnelName}</strong> and applies
            to every MCP server on it, including any you cannot view.
          </>
        )}
        {effect ? <> {effect}</> : null}
      </Text>
      <Text small muted>
        MCP servers you can view
        {impact.isReady ? ` (${impact.servers.length})` : null}
      </Text>
      <ServerList impact={impact} currentMcpServerId={currentMcpServerId} />
      {showPublicWarning ? (
        <Alert variant="warning" dismissible={false}>
          {PUBLIC_SIBLING_WARNING}
        </Alert>
      ) : null}
    </Stack>
  );
}

/**
 * Confirmation step for a tunnel-wide change. It reads the MCP servers on the
 * tunnel when it opens and keeps Confirm disabled until that read succeeds.
 */
export function SharedTunnelConfirmDialog({
  open,
  onOpenChange,
  tunneledMcpServerId,
  tunnelName,
  currentMcpServerId,
  title,
  description,
  effect,
  publicWarning,
  intro,
  children,
  confirmLabel,
  pendingLabel,
  isPending,
  errorMessage,
  onConfirm,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  tunneledMcpServerId: string;
  tunnelName?: string;
  currentMcpServerId?: string;
  title: string;
  description: ReactNode;
  effect?: ReactNode;
  publicWarning?: boolean;
  intro?: ReactNode;
  /** Extra content above the impact, such as the value being saved. */
  children?: ReactNode;
  confirmLabel: string;
  pendingLabel: string;
  isPending: boolean;
  errorMessage?: string;
  onConfirm: () => void;
}): JSX.Element {
  const impact = useSharedTunnelImpact(tunneledMcpServerId, { active: open });

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!isPending) onOpenChange(next);
      }}
    >
      <Dialog.Content className="max-w-xl!" closeable={!isPending}>
        <Dialog.Header>
          <Dialog.Title>{title}</Dialog.Title>
          <Dialog.Description>{description}</Dialog.Description>
        </Dialog.Header>
        {children}
        <SharedTunnelImpact
          impact={impact}
          tunnelName={tunnelName}
          currentMcpServerId={currentMcpServerId}
          effect={effect}
          publicWarning={publicWarning}
          intro={intro}
        />
        {errorMessage !== undefined ? (
          <Alert variant="error" dismissible={false}>
            {errorMessage}
          </Alert>
        ) : null}
        <Dialog.Footer>
          <Button
            variant="secondary"
            disabled={isPending}
            onClick={() => onOpenChange(false)}
          >
            <Button.Text>Cancel</Button.Text>
          </Button>
          <Button
            variant="primary"
            disabled={!impact.isReady || isPending}
            onClick={onConfirm}
          >
            {isPending ? (
              <Button.LeftIcon>
                <Loader2 className="size-4 animate-spin" />
              </Button.LeftIcon>
            ) : null}
            <Button.Text>{isPending ? pendingLabel : confirmLabel}</Button.Text>
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
