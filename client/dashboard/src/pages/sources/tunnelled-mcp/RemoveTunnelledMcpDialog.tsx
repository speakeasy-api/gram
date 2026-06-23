import { Input } from "@/components/ui/input";
import { Type } from "@/components/ui/type";
import type { McpServer } from "@gram/client/models/components";
import { useMcpEndpoints } from "@gram/client/react-query/index.js";
import { Alert, Badge, Button, Dialog } from "@speakeasy-api/moonshine";
import { Loader2 } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { useDeleteTunnelledMcpSource } from "./hooks";

interface RemoveTunnelledMcpDialogContentProps {
  tunnelledMcpServerId: string;
  displayName: string;
  linkedMcpServers: McpServer[];
  onClose: () => void;
  onSuccess: () => void;
}

export function RemoveTunnelledMcpDialogContent({
  tunnelledMcpServerId,
  displayName,
  linkedMcpServers,
  onClose,
  onSuccess,
}: RemoveTunnelledMcpDialogContentProps): JSX.Element {
  const deleteSource = useDeleteTunnelledMcpSource();
  const [confirmation, setConfirmation] = useState("");
  const inputMatches = confirmation === displayName;

  const handleConfirm = async () => {
    try {
      await deleteSource.mutateAsync({
        tunnelledMcpServerId,
        mcpServerIds: linkedMcpServers.map((server) => server.id),
      });
      toast.success("Tunnelled MCP server deleted");
      onSuccess();
    } catch (error) {
      const message =
        error instanceof Error
          ? error.message
          : "Failed to delete tunnelled MCP server";
      toast.error(message);
    }
  };

  return (
    <>
      <Dialog.Header>
        <Dialog.Title>Delete Tunnelled MCP Server</Dialog.Title>
        <Dialog.Description>
          This will permanently delete the tunnelled MCP source, all linked MCP
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
          To confirm, type the display name: <strong>{displayName}</strong>
        </Type>
        <Input
          value={confirmation}
          onChange={setConfirmation}
          placeholder={displayName}
          disabled={deleteSource.isPending}
        />
      </div>

      <Alert variant="warning" dismissible={false}>
        Deleting {displayName} cannot be undone.
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

function LinkedMcpServerRow({ server }: { server: McpServer }) {
  const { data: endpoints, isLoading } = useMcpEndpoints({
    mcpServerId: server.id,
  });
  const shortId = server.id.slice(0, 8);
  return (
    <li className="flex flex-col gap-1 px-3 py-2">
      <div className="flex items-center gap-2">
        <Type small className="font-mono" title={server.id}>
          {shortId}…
        </Type>
        <Badge variant="neutral">
          <Badge.Text>{server.visibility}</Badge.Text>
        </Badge>
      </div>
      {isLoading ? (
        <Type small muted>
          Loading endpoints…
        </Type>
      ) : endpoints && endpoints.mcpEndpoints.length > 0 ? (
        <Type small muted>
          {endpoints.mcpEndpoints.length} endpoint
          {endpoints.mcpEndpoints.length === 1 ? "" : "s"}:{" "}
          {endpoints.mcpEndpoints.map((e) => e.slug).join(", ")}
        </Type>
      ) : (
        <Type small muted>
          No endpoints attached
        </Type>
      )}
    </li>
  );
}
