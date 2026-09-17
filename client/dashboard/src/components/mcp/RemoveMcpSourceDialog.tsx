import { Alert } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Input } from "@/components/ui/Input";
import { Text } from "@/components/ui/Text";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useMcpEndpoints } from "@gram/client/react-query/mcpEndpoints.js";
import { Loader2 } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";

export type RemoveMcpSourceDialogContentProps = {
  /** Dialog title, e.g. "Delete Remote MCP Server". */
  title: string;
  /** Noun phrase in the description, e.g. "the remote MCP server". */
  entityDescription: string;
  /** Label for the confirmation value, e.g. "the server URL". */
  confirmLabel: string;
  /** Exact string the user must type to arm the destructive action. */
  confirmValue: string;
  successMessage: string;
  failureMessage: string;
  linkedMcpServers: McpServer[];
  isPending: boolean;
  errorMessage: string | undefined;
  onClose: () => void;
  onSuccess: () => void;
  /** Performs the deletion; rejects on failure. */
  onConfirm: () => Promise<void>;
};

function endpointSummary(
  isLoading: boolean,
  isError: boolean,
  slugs: string[],
): string {
  if (isLoading) return "Loading endpoints";
  if (isError) return "Unable to load endpoints";
  if (slugs.length === 0) return "No endpoints attached";
  const noun = slugs.length === 1 ? "endpoint" : "endpoints";
  return `${slugs.length} ${noun}: ${slugs.join(", ")}`;
}

function LinkedMcpServerRow({ server }: { server: McpServer }) {
  const { data, isError, isLoading } = useMcpEndpoints({
    mcpServerId: server.id,
  });
  const slugs = (data?.mcpEndpoints ?? []).map((endpoint) => endpoint.slug);

  return (
    <li className="flex flex-col gap-1 px-3 py-2">
      <div className="flex items-center gap-2">
        <Text small className="font-medium" title={server.id}>
          {server.name || "MCP Server"}
        </Text>
        <Badge variant="neutral">
          <Badge.Text>{server.visibility}</Badge.Text>
        </Badge>
      </div>
      <Text small muted>
        {endpointSummary(isLoading, isError, slugs)}
      </Text>
    </li>
  );
}

/**
 * Confirm-to-delete dialog body for MCP source kinds (remote, tunneled,
 * unproxied). Owns the typed-confirmation input, the linked-server summary,
 * and toast plumbing; callers own their delete mutation and pass its state in.
 */
export function RemoveMcpSourceDialogContent({
  title,
  entityDescription,
  confirmLabel,
  confirmValue,
  successMessage,
  failureMessage,
  linkedMcpServers,
  isPending,
  errorMessage,
  onClose,
  onSuccess,
  onConfirm,
}: RemoveMcpSourceDialogContentProps): JSX.Element {
  const [confirmation, setConfirmation] = useState("");
  const inputMatches = confirmation === confirmValue;

  const handleConfirm = async () => {
    try {
      await onConfirm();
      toast.success(successMessage);
      onSuccess();
    } catch (error) {
      const message = error instanceof Error ? error.message : failureMessage;
      toast.error(message);
    }
  };

  return (
    <>
      <Dialog.Header>
        <Dialog.Title>{title}</Dialog.Title>
        <Dialog.Description>
          This will permanently delete {entityDescription}, every MCP server
          backed by it, and the MCP endpoints attached to those servers.
        </Dialog.Description>
      </Dialog.Header>

      {linkedMcpServers.length > 0 && (
        <div className="space-y-3">
          <Text small muted>
            The following will also be removed:
          </Text>
          <ul className="divide-border divide-y border">
            {linkedMcpServers.map((server) => (
              <LinkedMcpServerRow key={server.id} server={server} />
            ))}
          </ul>
        </div>
      )}

      <div className="grid gap-2">
        <Text small>
          To confirm, type {confirmLabel}: <strong>{confirmValue}</strong>
        </Text>
        <Input
          value={confirmation}
          onChange={setConfirmation}
          placeholder={confirmValue}
          disabled={isPending}
        />
      </div>

      <Alert variant="warning" dismissible={false}>
        Deleting {confirmValue} cannot be undone.
      </Alert>

      {errorMessage !== undefined && (
        <Alert variant="error" dismissible={false}>
          {errorMessage}
        </Alert>
      )}

      <Dialog.Footer>
        <Button variant="secondary" onClick={onClose} disabled={isPending}>
          <Button.Text>Cancel</Button.Text>
        </Button>
        <Button
          variant="destructive-primary"
          disabled={!inputMatches || isPending}
          onClick={() => void handleConfirm()}
        >
          {isPending && (
            <Button.LeftIcon>
              <Loader2 className="size-4 animate-spin" />
            </Button.LeftIcon>
          )}
          <Button.Text>{isPending ? "Deleting" : "Delete"}</Button.Text>
        </Button>
      </Dialog.Footer>
    </>
  );
}
