import { SharedTunnelImpact } from "@/components/mcp/shared-tunnel-impact";
import { useSharedTunnelImpact } from "@/components/mcp/use-shared-tunnel-impact";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Input } from "@/components/ui/Input";
import { Text } from "@/components/ui/Text";
import { useSdkClient } from "@/contexts/Sdk";
import { formatTunneledMcpDisplay, mcpServerRouteParam } from "@/lib/sources";
import {
  addMcpServerOnTunnelHref,
  serverSetKey,
  TunnelDeleteIncompleteError,
  TunnelServersChangedError,
  tunnelDeleteRecovery,
} from "@/pages/sources/tunneled-mcp/existingTunnel";
import { useDeleteTunneledMcpSource } from "@/pages/sources/tunneled-mcp/hooks";
import { useRoutes } from "@/routes";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { TunneledMcpServer } from "@gram/client/models/components/tunneledmcpserver.js";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { fetchLinkedMcpServers } from "./sourceDelete";

// How long the outcome of a partly completed delete stays on screen after the
// dialog navigates away: it carries the next step.
const INCOMPLETE_DELETE_TOAST_MS = 20_000;

// What the typed confirmation was given for: the tunnel name and the exact
// set of MCP servers on screen at that moment.
type Confirmation = { text: string; servers: McpServer[] };

/**
 * Deletes a tunnel and the MCP servers on it that the user reviewed. The list
 * is read fresh when the dialog opens, Delete stays disabled until it lands,
 * and a confirmation covers only the servers shown when it was typed: if the
 * list changes afterwards, the user has to confirm again.
 */
export function DeleteTunnelDialogContent({
  tunnel,
  mcpServerId,
  onClose,
  onLeave,
  onBusyChange,
}: {
  tunnel: TunneledMcpServer;
  mcpServerId: string;
  onClose: () => void;
  /** Leaves the page, to `href` when given; this server may be gone. */
  onLeave: (href?: string) => void;
  /** Told while the delete runs, so the dialog can refuse to close. */
  onBusyChange?: (busy: boolean) => void;
}): JSX.Element {
  const routes = useRoutes();
  const client = useSdkClient();
  const queryClient = useQueryClient();
  const impact = useSharedTunnelImpact(tunnel.id, { active: true });
  const remove = useDeleteTunneledMcpSource();
  // Spans the delete and the recovery after a partial one, which
  // remove.isPending does not: recovery may still navigate away.
  const [running, setRunning] = useState(false);
  useEffect(() => onBusyChange?.(running), [running, onBusyChange]);
  const [confirmation, setConfirmation] = useState<Confirmation>({
    text: "",
    servers: [],
  });
  const tunnelName = formatTunneledMcpDisplay(tunnel);

  // A confirmation typed against a different list than the one now shown no
  // longer counts, whether servers were added, removed or swapped.
  const listChanged =
    confirmation.text !== "" &&
    impact.isReady &&
    serverSetKey(confirmation.servers) !== serverSetKey(impact.servers);
  const typed = listChanged ? "" : confirmation.text;
  const armed = typed === tunnelName && impact.isReady && !running;

  // Works out what is left after a delete that stopped partway and sends the
  // user to wherever it can be finished.
  const recover = async (error: TunnelDeleteIncompleteError) => {
    const tunnelListHref = addMcpServerOnTunnelHref(
      routes.mcp.add.tunneled.href(),
      tunnel.id,
    );
    let remaining: McpServer[];
    try {
      remaining = await fetchLinkedMcpServers(client, queryClient, {
        tunneledMcpServerId: tunnel.id,
      });
    } catch {
      // What is left is unknown, and this server may be gone. The tunnel list
      // reads it again and links every server still on the tunnel.
      toast.error(
        `${error.message} Could not check which MCP servers still use the tunnel. The tunnel list shows them; delete them from their settings, then delete the tunnel.`,
        { duration: INCOMPLETE_DELETE_TOAST_MS },
      );
      onLeave(tunnelListHref);
      return;
    }
    const recovery = tunnelDeleteRecovery(remaining, mcpServerId);
    switch (recovery.kind) {
      case "stay":
        // This server survived, so the dialog can show what is left.
        setConfirmation({ text: "", servers: [] });
        impact.retry();
        return;
      case "server":
        toast.error(
          `${error.message} ${recovery.mcpServer.name || "Another MCP server"} still uses the tunnel; continue from its settings.`,
          { duration: INCOMPLETE_DELETE_TOAST_MS },
        );
        onLeave(
          routes.mcp.x.settings.href(mcpServerRouteParam(recovery.mcpServer)),
        );
        return;
      case "tunnel":
        toast.error(
          `${error.message} No MCP server you can view still uses the tunnel. Delete it from the tunnel list; if it is still in use by servers you cannot view, ask a project admin.`,
          { duration: INCOMPLETE_DELETE_TOAST_MS },
        );
        onLeave(tunnelListHref);
    }
  };

  const handleConfirm = async () => {
    setRunning(true);
    try {
      await remove.mutateAsync({
        tunneledMcpServerId: tunnel.id,
        confirmedMcpServerIds: confirmation.servers.map((server) => server.id),
      });
      toast.success("Tunnel and its MCP servers deleted");
      onLeave();
    } catch (error) {
      if (error instanceof TunnelServersChangedError) {
        // Nothing was deleted. Show the current list and ask again.
        setConfirmation({ text: "", servers: [] });
        impact.retry();
        return;
      }
      if (error instanceof TunnelDeleteIncompleteError && error.progressed) {
        await recover(error);
      }
      // Otherwise nothing changed: the error stays in the dialog for a retry.
    } finally {
      setRunning(false);
    }
  };

  return (
    <>
      <Dialog.Header>
        <Dialog.Title>Delete tunnel and its MCP servers</Dialog.Title>
        <Dialog.Description>
          This permanently deletes the tunnel, the MCP servers listed below, and
          their endpoints. Running tunnel agents are disconnected.
        </Dialog.Description>
      </Dialog.Header>

      <SharedTunnelImpact
        impact={impact}
        tunnelName={tunnelName}
        currentMcpServerId={mcpServerId}
        effect="Only the MCP servers listed are deleted; if any others use the tunnel, it is kept."
        publicWarning={tunnel.allowPublic}
      />

      {listChanged ? (
        <Alert variant="warning" dismissible={false}>
          The MCP servers on this tunnel changed after you confirmed. Review the
          list and type the tunnel name again.
        </Alert>
      ) : null}

      <div className="grid gap-2">
        <Text small>
          To confirm, type the tunnel name: <strong>{tunnelName}</strong>
        </Text>
        <Input
          value={typed}
          onChange={(text) =>
            setConfirmation({ text, servers: impact.servers })
          }
          placeholder={tunnelName}
          disabled={running || !impact.isReady}
          aria-label="Type the tunnel name to confirm"
        />
      </div>

      {remove.isError ? (
        <Alert variant="error" dismissible={false}>
          {remove.error.message}
        </Alert>
      ) : null}

      <Dialog.Footer>
        <Button variant="secondary" onClick={onClose} disabled={running}>
          <Button.Text>Cancel</Button.Text>
        </Button>
        <Button
          variant="destructive-primary"
          disabled={!armed}
          onClick={() => void handleConfirm()}
        >
          {running ? (
            <Button.LeftIcon>
              <Loader2 className="size-4 animate-spin" />
            </Button.LeftIcon>
          ) : null}
          <Button.Text>{running ? "Deleting" : "Delete"}</Button.Text>
        </Button>
      </Dialog.Footer>
    </>
  );
}
