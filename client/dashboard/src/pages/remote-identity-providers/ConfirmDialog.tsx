import { Alert } from "@/components/ui/Alert";
import { Dialog } from "@/components/ui/Dialog";
import { Text } from "@/components/ui/Text";
import { Button } from "@/components/ui/Button";

// ConfirmDialog is a small reusable confirmation surface for the org-admin
// Remote Identity Providers UI. It optionally renders an authoritative impact
// summary (counts + affected MCP server names) sourced from a server-side
// pre-flight endpoint so destructive actions are never confirmed against
// client-composed estimates.
export function ConfirmDialog({
  open,
  onOpenChange,
  title,
  description,
  confirmLabel,
  onConfirm,
  isPending,
  impact,
  error,
  confirmVariant = "destructive-primary",
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description: React.ReactNode;
  confirmLabel: string;
  onConfirm: () => void;
  isPending?: boolean;
  impact?: {
    summary: string;
    mcpServerNames?: string[];
    isLoading?: boolean;
  };
  // Rendered inline above the footer, for refusals the operator has to read and
  // act on rather than dismiss. A toast is the wrong surface for those: it
  // vanishes, and the dialog it describes is still open. Callers that only need
  // "something went wrong" should keep using a toast.
  error?: string | null;
  // Confirmations of destructive actions are the common case, so that is the
  // default; a lifecycle step that is safe but still worth a pause (activating
  // a key) reads wrong in red.
  confirmVariant?: "destructive-primary" | "primary";
}): JSX.Element {
  return (
    <Dialog
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen && isPending) return;
        onOpenChange(nextOpen);
      }}
    >
      <Dialog.Content closeable={!isPending}>
        <Dialog.Header>
          <Dialog.Title>{title}</Dialog.Title>
          <Dialog.Description>{description}</Dialog.Description>
        </Dialog.Header>
        {impact && (
          <div>
            {impact.isLoading ? (
              <Text small muted>
                Checking impact…
              </Text>
            ) : (
              <>
                <Text>{impact.summary}</Text>
                {impact.mcpServerNames && impact.mcpServerNames.length > 0 && (
                  <div className="mt-2">
                    <Text small as="div">
                      Affected MCP Servers:
                    </Text>
                    <ul className="mt-1 list-disc pl-5">
                      {impact.mcpServerNames.map((name) => (
                        <li key={name}>
                          <Text small muted as="span">
                            {name}
                          </Text>
                        </li>
                      ))}
                    </ul>
                  </div>
                )}
              </>
            )}
          </div>
        )}
        {error && (
          <Alert variant="error" dismissible={false}>
            {error}
          </Alert>
        )}
        <Dialog.Footer>
          <Button
            variant="tertiary"
            onClick={() => onOpenChange(false)}
            disabled={isPending}
          >
            <Button.Text>Cancel</Button.Text>
          </Button>
          <Button
            variant={confirmVariant}
            onClick={onConfirm}
            disabled={isPending || impact?.isLoading}
          >
            <Button.Text>{isPending ? "Working…" : confirmLabel}</Button.Text>
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
