import { SharedTunnelImpact } from "@/components/mcp/shared-tunnel-impact";
import { useSharedTunnelImpact } from "@/components/mcp/use-shared-tunnel-impact";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Input } from "@/components/ui/Input";
import { Text } from "@/components/ui/Text";
import { formatTunneledMcpDisplay } from "@/lib/sources";
import {
  TunnelDeleteIncompleteError,
  TunnelServersChangedError,
} from "@/pages/sources/tunneled-mcp/existingTunnel";
import { useDeleteTunneledMcpSource } from "@/pages/sources/tunneled-mcp/hooks";
import type { TunneledMcpServer } from "@gram/client/models/components/tunneledmcpserver.js";
import { Loader2 } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";

// How long the outcome of a partly completed delete stays on screen after the
// dialog navigates away: it carries the recovery steps.
const INCOMPLETE_DELETE_TOAST_MS = 20_000;

/**
 * Deletes a tunnel and the MCP servers on it that the user reviewed. The list
 * is read fresh when the dialog opens, Delete stays disabled until it lands,
 * and only the servers shown are deleted.
 */
export function DeleteTunnelDialogContent({
  tunnel,
  mcpServerId,
  onClose,
  onLeave,
}: {
  tunnel: TunneledMcpServer;
  mcpServerId: string;
  onClose: () => void;
  /** Called once this server may be gone, so the page has to be left. */
  onLeave: () => void;
}): JSX.Element {
  const impact = useSharedTunnelImpact(tunnel.id, { active: true });
  const remove = useDeleteTunneledMcpSource();
  const [confirmation, setConfirmation] = useState("");
  const tunnelName = formatTunneledMcpDisplay(tunnel);
  const armed =
    confirmation === tunnelName && impact.isReady && !remove.isPending;

  const handleConfirm = async () => {
    try {
      await remove.mutateAsync({
        tunneledMcpServerId: tunnel.id,
        confirmedMcpServerIds: impact.servers.map((server) => server.id),
      });
      toast.success("Tunnel and its MCP servers deleted");
      onLeave();
    } catch (error) {
      if (error instanceof TunnelServersChangedError) {
        // Nothing was deleted. Show the current list and ask again.
        setConfirmation("");
        impact.retry();
        return;
      }
      if (error instanceof TunnelDeleteIncompleteError && error.progressed) {
        toast.error(error.message, { duration: INCOMPLETE_DELETE_TOAST_MS });
        onLeave();
      }
      // Otherwise nothing changed: the error stays in the dialog for a retry.
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
      />

      <div className="grid gap-2">
        <Text small>
          To confirm, type the tunnel name: <strong>{tunnelName}</strong>
        </Text>
        <Input
          value={confirmation}
          onChange={setConfirmation}
          placeholder={tunnelName}
          disabled={remove.isPending}
          aria-label="Type the tunnel name to confirm"
        />
      </div>

      {remove.isError ? (
        <Alert variant="error" dismissible={false}>
          {remove.error.message}
        </Alert>
      ) : null}

      <Dialog.Footer>
        <Button
          variant="secondary"
          onClick={onClose}
          disabled={remove.isPending}
        >
          <Button.Text>Cancel</Button.Text>
        </Button>
        <Button
          variant="destructive-primary"
          disabled={!armed}
          onClick={() => void handleConfirm()}
        >
          {remove.isPending ? (
            <Button.LeftIcon>
              <Loader2 className="size-4 animate-spin" />
            </Button.LeftIcon>
          ) : null}
          <Button.Text>{remove.isPending ? "Deleting" : "Delete"}</Button.Text>
        </Button>
      </Dialog.Footer>
    </>
  );
}
