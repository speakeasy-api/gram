import { Dialog } from "@/components/ui/Dialog";
import type { ReactNode } from "react";

/**
 * Setup as a large modal over the server's Overview: a task the admin
 * finishes and leaves, not a place they stay. Closing it returns to Overview.
 * Its body is the scroll container, so a setup's side column stays in view.
 */
export function SetupModal({
  onClose,
  children,
}: {
  onClose: () => void;
  children: ReactNode;
}): JSX.Element {
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <Dialog.Content className="flex h-[80vh] w-[80vw] max-w-none flex-col gap-0 p-0 max-sm:h-[calc(100dvh-1rem)] max-sm:w-[calc(100vw-1rem)]">
        <Dialog.Title className="sr-only">Setup</Dialog.Title>
        <Dialog.Description className="sr-only">
          Steps to finish setting up this server.
        </Dialog.Description>
        <div
          data-setup-scroll=""
          className="min-h-0 flex-1 overflow-y-auto px-6 pt-8 pb-12 sm:px-10"
        >
          {children}
        </div>
      </Dialog.Content>
    </Dialog>
  );
}
