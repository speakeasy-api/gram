import { Input } from "@/components/ui/input";
import { Type } from "@/components/ui/type";
import { LinkedMcpServerRow } from "@/components/sources/LinkedMcpServerRow";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { Alert, Button, Dialog } from "@speakeasy-api/moonshine";
import { Loader2 } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";

interface RemoveMcpSourceDialogContentProps {
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
}

/**
 * Shared confirm-to-delete dialog body for MCP source kinds (remote,
 * tunneled). Owns the typed-confirmation input, linked-server summary, and
 * toast plumbing; the per-source wrappers own their delete mutation and pass
 * its state in.
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
          This will permanently delete {entityDescription}, all linked MCP
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
          To confirm, type {confirmLabel}: <strong>{confirmValue}</strong>
        </Type>
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
