import * as React from "react";

import { ConfirmDialog } from "./confirm-dialog";

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
 * wherever a `window.confirm(...)` call site used to live.
 *
 * The no-restricted-globals lint rule flags any identifier named `confirm`
 * (it does not track shadowing), so rename it when destructuring:
 *
 * ```tsx
 * const { confirm: requestConfirm, dialog } = useConfirm();
 *
 * async function handleDelete() {
 *   if (!(await requestConfirm({ title: "Delete API key?", destructive: true }))) return;
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

  const requestConfirm = React.useCallback(
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

  return { confirm: requestConfirm, dialog };
}
