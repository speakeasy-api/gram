import { Dialog } from "@/components/ui/Dialog";
import type { ReactNode } from "react";

/**
 * Everything this issuer admits, in one table. The catalog Speakeasy
 * verifies and the URLs this project allows itself were two separate
 * surfaces — a cramped popover and an inline box nested under the radios —
 * which made "Verified clients" read as one setting doing two jobs.
 */
export function AllowedClientsDialog({
  trigger,
  children,
}: {
  trigger: ReactNode;
  /** The allowed-clients table, with its add and remove controls. */
  children: ReactNode;
}): JSX.Element {
  return (
    <Dialog>
      <Dialog.Trigger asChild>{trigger}</Dialog.Trigger>
      <Dialog.Content className="flex max-h-[70vh] max-w-5xl flex-col">
        <Dialog.Header>
          <Dialog.Title>Allowed clients</Dialog.Title>
          <Dialog.Description>
            Clients that may identify themselves by URL while this server admits
            verified clients. The ones Speakeasy verifies are always allowed;
            add your own to the same list.
          </Dialog.Description>
        </Dialog.Header>
        <div className="min-h-0 flex-1 overflow-hidden">{children}</div>
      </Dialog.Content>
    </Dialog>
  );
}
