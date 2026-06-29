import { Input } from "@/components/ui/input";
import { Type } from "@/components/ui/type";
import { LinkedMcpServerRow } from "@/components/sources/LinkedMcpServerRow";
import type { McpServer } from "@gram/client/models/components";
import { Alert, Button, Dialog } from "@speakeasy-api/moonshine";
import { Loader2 } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { useDeleteRemoteMcpSource } from "./hooks";

interface RemoveRemoteMcpDialogContentProps {
  remoteMcpServerId: string;
  url: string;
  linkedMcpServers: McpServer[];
  onClose: () => void;
  onSuccess: () => void;
}

export function RemoveRemoteMcpDialogContent({
  remoteMcpServerId,
  url,
  linkedMcpServers,
  onClose,
  onSuccess,
}: RemoveRemoteMcpDialogContentProps): JSX.Element {
  const deleteSource = useDeleteRemoteMcpSource();
  // Require typing the URL to enable the destructive action — same shape the
  // existing RemoveSourceDialogContent uses, but keyed on the URL since remote
  // MCP sources don't have a slugified name.
  const [confirmation, setConfirmation] = useState("");
  const inputMatches = confirmation === url;

  const handleConfirm = async () => {
    try {
      await deleteSource.mutateAsync({
        remoteMcpServerId,
        mcpServerIds: linkedMcpServers.map((server) => server.id),
      });
      toast.success("Remote MCP server deleted");
      onSuccess();
    } catch (error) {
      const message =
        error instanceof Error
          ? error.message
          : "Failed to delete remote MCP server";
      toast.error(message);
    }
  };

  return (
    <>
      <Dialog.Header>
        <Dialog.Title>Delete Remote MCP Server</Dialog.Title>
        <Dialog.Description>
          This will permanently delete the remote MCP server, all linked MCP
          servers, and the MCP endpoints attached to them.
        </Dialog.Description>
      </Dialog.Header>

      {linkedMcpServers.length > 0 && (
        <div className="space-y-3">
          <Type small muted>
            The following will also be removed:
          </Type>
          <ul className="divide-border space-y-2 rounded-md border">
            {linkedMcpServers.map((server) => (
              <LinkedMcpServerRow key={server.id} server={server} />
            ))}
          </ul>
        </div>
      )}

      <div className="grid gap-2">
        <Type small>
          To confirm, type the server URL: <strong>{url}</strong>
        </Type>
        <Input
          value={confirmation}
          onChange={setConfirmation}
          placeholder={url}
          disabled={deleteSource.isPending}
        />
      </div>

      <Alert variant="warning" dismissible={false}>
        Deleting {url} cannot be undone.
      </Alert>

      {deleteSource.isError && (
        <Alert variant="error" dismissible={false}>
          {deleteSource.error.message}
        </Alert>
      )}

      <Dialog.Footer>
        <Button
          variant="secondary"
          onClick={onClose}
          disabled={deleteSource.isPending}
        >
          <Button.Text>Cancel</Button.Text>
        </Button>
        <Button
          variant="destructive-primary"
          disabled={!inputMatches || deleteSource.isPending}
          onClick={() => void handleConfirm()}
        >
          {deleteSource.isPending ? (
            <>
              <Button.LeftIcon>
                <Loader2 className="size-4 animate-spin" />
              </Button.LeftIcon>
              <Button.Text>Deleting</Button.Text>
            </>
          ) : (
            <Button.Text>Delete</Button.Text>
          )}
        </Button>
      </Dialog.Footer>
    </>
  );
}
