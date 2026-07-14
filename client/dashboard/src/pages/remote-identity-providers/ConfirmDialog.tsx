import { Dialog } from "@/components/ui/dialog";
import { Type } from "@/components/ui/type";
import { Button } from "@/components/ui/button";

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
}): JSX.Element {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>{title}</Dialog.Title>
          <Dialog.Description>{description}</Dialog.Description>
        </Dialog.Header>
        {impact && (
          <div>
            {impact.isLoading ? (
              <Type small muted>
                Checking impact…
              </Type>
            ) : (
              <>
                <Type>{impact.summary}</Type>
                {impact.mcpServerNames && impact.mcpServerNames.length > 0 && (
                  <div className="mt-2">
                    <Type small as="div">
                      Affected MCP Servers:
                    </Type>
                    <ul className="mt-1 list-disc pl-5">
                      {impact.mcpServerNames.map((name) => (
                        <li key={name}>
                          <Type small muted as="span">
                            {name}
                          </Type>
                        </li>
                      ))}
                    </ul>
                  </div>
                )}
              </>
            )}
          </div>
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
            variant="destructive-primary"
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
