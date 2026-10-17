import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Input } from "@/components/ui/Input";
import { Text } from "@/components/ui/Text";
import { formatTunneledMcpDisplay } from "@/lib/sources";
import type { TunneledMcpServer } from "@gram/client/models/components/tunneledmcpserver.js";
import { Loader2 } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { isTunnelInUseError } from "./existingTunnel";
import { useDeleteUnusedTunnel } from "./hooks";

const TUNNEL_IN_USE_MESSAGE =
  "This tunnel is still in use, possibly by MCP servers you cannot view, so it was not deleted.";

/**
 * Deletes a tunnel that no MCP server the user can view uses. It never deletes
 * MCP servers; the backend refuses while any server uses the tunnel.
 */
export function DeleteUnusedTunnelDialog({
  tunnel,
  onClose,
}: {
  tunnel: TunneledMcpServer | null;
  onClose: () => void;
}): JSX.Element {
  const remove = useDeleteUnusedTunnel();
  // Closing is refused while the delete is in flight, so its outcome (a
  // refusal in particular) is never dropped unseen.
  const close = () => {
    if (remove.isPending) return;
    remove.reset();
    onClose();
  };

  return (
    <Dialog
      open={tunnel !== null}
      onOpenChange={(open) => {
        if (!open) close();
      }}
    >
      <Dialog.Content className="max-w-xl!" closeable={!remove.isPending}>
        {tunnel ? (
          <DeleteUnusedTunnelBody
            key={tunnel.id}
            tunnel={tunnel}
            remove={remove}
            onClose={close}
          />
        ) : null}
      </Dialog.Content>
    </Dialog>
  );
}

function DeleteUnusedTunnelBody({
  tunnel,
  remove,
  onClose,
}: {
  tunnel: TunneledMcpServer;
  remove: ReturnType<typeof useDeleteUnusedTunnel>;
  onClose: () => void;
}) {
  const [confirmation, setConfirmation] = useState("");
  const tunnelName = formatTunneledMcpDisplay(tunnel);

  const handleConfirm = async () => {
    try {
      await remove.mutateAsync({ tunneledMcpServerId: tunnel.id });
      toast.success("Tunnel deleted");
      onClose();
    } catch {
      // Shown in the dialog.
    }
  };

  let errorMessage: string | undefined;
  if (remove.isError) {
    errorMessage = isTunnelInUseError(remove.error)
      ? TUNNEL_IN_USE_MESSAGE
      : remove.error.message;
  }

  return (
    <>
      <Dialog.Header>
        <Dialog.Title>Delete tunnel</Dialog.Title>
        <Dialog.Description>
          This permanently deletes the tunnel and revokes its key. Running
          tunnel agents are disconnected. A tunnel can be deleted only when no
          MCP server uses it.
        </Dialog.Description>
      </Dialog.Header>
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
      {errorMessage !== undefined ? (
        <Alert variant="error" dismissible={false}>
          {errorMessage}
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
          disabled={confirmation !== tunnelName || remove.isPending}
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
