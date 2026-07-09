import * as React from "react";

import { Dialog } from "@/components/ui/dialog";
import { Field, FieldLabel } from "@/components/ui/field";
import { Button, Icon, Input } from "@/components/ui/moonshine";

export interface ConfirmDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: React.ReactNode;
  description?: React.ReactNode;
  /** Bordered panel listing what the action affects, e.g. a `<ul>` of consequences. */
  impact?: React.ReactNode;
  confirmLabel?: string;
  cancelLabel?: string;
  /** Renders the confirm button as `destructive-primary` instead of `primary`. */
  destructive?: boolean;
  onConfirm: () => void | Promise<void>;
  /**
   * When set, the confirm button stays disabled until the user types this
   * exact string into a rendered "type to confirm" field.
   */
  confirmValue?: string;
}

/**
 * Generic confirmation prompt built on the local `Dialog` compound
 * (`@/components/ui/dialog`). Controlled — the caller owns `open`; prefer
 * `useConfirm` below for simple `window.confirm`-style call sites that don't
 * need a controlled dialog of their own.
 */
export function ConfirmDialog({
  open,
  onOpenChange,
  title,
  description,
  impact,
  confirmLabel = "Confirm",
  cancelLabel = "Cancel",
  destructive = false,
  onConfirm,
  confirmValue,
}: ConfirmDialogProps): React.JSX.Element {
  const [pending, setPending] = React.useState(false);
  const [typedValue, setTypedValue] = React.useState("");
  const typeToConfirmId = React.useId();

  // Drop any stale typed value when the dialog is reopened for a new action,
  // so a previous match doesn't leave Confirm armed on first paint.
  React.useEffect(() => {
    if (open) setTypedValue("");
  }, [open]);

  const requiresTypedMatch = confirmValue !== undefined;
  const typedMismatch =
    requiresTypedMatch && typedValue.length > 0 && typedValue !== confirmValue;
  const canConfirm = !requiresTypedMatch || typedValue === confirmValue;

  const handleOpenChange = (next: boolean): void => {
    // Ignore close attempts (Escape, overlay click, the [x] button) while an
    // onConfirm promise is in flight — mirrors the InputDialog pattern.
    if (!pending) onOpenChange(next);
  };

  const handleConfirm = async (): Promise<void> => {
    if (pending || !canConfirm) return;
    setPending(true);
    try {
      await onConfirm();
      onOpenChange(false);
    } catch {
      // Leave the dialog open (with buttons re-enabled below) so the user
      // can retry. Callers that want a toast on failure should catch inside
      // their own onConfirm and call toastError() there.
    } finally {
      setPending(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <Dialog.Content className="sm:max-w-md">
        <Dialog.Header>
          <Dialog.Title>{title}</Dialog.Title>
          {description && (
            <Dialog.Description>{description}</Dialog.Description>
          )}
        </Dialog.Header>

        {impact && (
          <div className="border-neutral-softest bg-muted/40 text-foreground border px-3 py-2.5 text-sm">
            {impact}
          </div>
        )}

        {requiresTypedMatch && (
          <Field data-invalid={typedMismatch || undefined}>
            <FieldLabel htmlFor={typeToConfirmId}>
              Type &ldquo;{confirmValue}&rdquo; to confirm
            </FieldLabel>
            <Input
              id={typeToConfirmId}
              value={typedValue}
              onChange={(event) => setTypedValue(event.target.value)}
              placeholder={confirmValue}
              disabled={pending}
              error={typedMismatch}
              autoFocus
            />
          </Field>
        )}

        <Dialog.Footer>
          <Button
            variant="tertiary"
            onClick={() => onOpenChange(false)}
            disabled={pending}
          >
            <Button.Text>{cancelLabel}</Button.Text>
          </Button>
          <Button
            variant={destructive ? "destructive-primary" : "primary"}
            onClick={() => void handleConfirm()}
            disabled={pending || !canConfirm}
          >
            {pending && (
              <Button.LeftIcon>
                <Icon name="loader-circle" className="animate-spin" />
              </Button.LeftIcon>
            )}
            <Button.Text>{confirmLabel}</Button.Text>
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}

export interface UseConfirmOptions {
  title: React.ReactNode;
  description?: React.ReactNode;
  impact?: React.ReactNode;
  confirmLabel?: string;
  cancelLabel?: string;
  destructive?: boolean;
  confirmValue?: string;
}

interface PendingConfirmation {
  options: UseConfirmOptions;
  resolve: (confirmed: boolean) => void;
}

/**
 * Imperative replacement for `window.confirm`. Render `dialog` once near the
 * root of the component that calls `confirm`, then await `confirm(options)`
 * wherever a `window.confirm(...)` call site used to live:
 *
 * ```tsx
 * const { confirm, dialog } = useConfirm();
 *
 * async function handleDelete() {
 *   if (!(await confirm({ title: "Delete API key?", destructive: true }))) return;
 *   await deleteKey();
 * }
 *
 * return (
 *   <>
 *     <Button onClick={handleDelete}>Delete</Button>
 *     {dialog}
 *   </>
 * );
 * ```
 */
export function useConfirm(): {
  confirm: (options: UseConfirmOptions) => Promise<boolean>;
  dialog: React.ReactNode;
} {
  const [pendingConfirmation, setPendingConfirmation] =
    React.useState<PendingConfirmation | null>(null);

  const confirm = React.useCallback(
    (options: UseConfirmOptions): Promise<boolean> => {
      return new Promise<boolean>((resolve) => {
        setPendingConfirmation({ options, resolve });
      });
    },
    [],
  );

  const handleOpenChange = React.useCallback((open: boolean): void => {
    if (open) return;
    setPendingConfirmation((current) => {
      current?.resolve(false);
      return null;
    });
  }, []);

  const handleConfirm = React.useCallback((): void => {
    // Resolving here (rather than also clearing state) lets ConfirmDialog's
    // own post-confirm onOpenChange(false) call drive the close — it lands
    // on handleOpenChange above, which is a no-op resolve since the promise
    // already settled true.
    pendingConfirmation?.resolve(true);
  }, [pendingConfirmation]);

  const dialog = pendingConfirmation ? (
    <ConfirmDialog
      open
      onOpenChange={handleOpenChange}
      onConfirm={handleConfirm}
      title={pendingConfirmation.options.title}
      description={pendingConfirmation.options.description}
      impact={pendingConfirmation.options.impact}
      confirmLabel={pendingConfirmation.options.confirmLabel}
      cancelLabel={pendingConfirmation.options.cancelLabel}
      destructive={pendingConfirmation.options.destructive}
      confirmValue={pendingConfirmation.options.confirmValue}
    />
  ) : null;

  return { confirm, dialog };
}
