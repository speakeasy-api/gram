import { useRef, useState, type JSX } from "react";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { ACCOUNT_TYPE_OPTIONS, type AccountType } from "@/lib/accountTypes";
import { errorMessage, type AdminOrganization } from "@/lib/gramAdminApi";

import type { WriteReporter } from "./OrganizationActions";
import { useBulkUpdateAccountType } from "./rowActions";

function organizationCount(count: number): string {
  return `${count} ${count === 1 ? "organization" : "organizations"}`;
}

/**
 * Sets one account type on every ticked organization.
 *
 * The ids come from the ticked rows and from nowhere else. The write matches an
 * id case-sensitively and the list search matches case-insensitively, so an
 * operator who pasted an id in the wrong case can find the row, and a field
 * they could type that id back into would report it missing while the row sat
 * on screen in front of them. There is no such field here and there must not
 * be one.
 */
export function BulkAccountType({
  selected,
  reporter,
  onDone,
}: {
  selected: AdminOrganization[];
  // Handed down rather than taken from context: this control is drawn by the
  // page that owns both surfaces, not from inside a table cell.
  reporter: WriteReporter;
  // Called after a write lands, so the page drops a selection that has already
  // been acted on.
  onDone: () => void;
}): JSX.Element {
  const { announce, showFailure } = reporter;
  // The type the operator picked, and the whole of what the dialog is open
  // for. Nothing is written until they confirm it.
  const [pending, setPending] = useState<AccountType>();
  const trigger = useRef<HTMLButtonElement>(null);

  const bulk = useBulkUpdateAccountType();

  const run = (accountType: AccountType): void => {
    const ids = selected.map((org) => org.id);
    bulk.mutate(
      { ids, account_type: accountType },
      {
        onSuccess: (result) => {
          setPending(undefined);
          // Named from the ticked rows, because the answer carries ids and the
          // operator picked names. An id the selection cannot name is reported
          // as itself rather than dropped.
          const names = new Map(selected.map((org) => [org.id, org.name]));
          const missing = result.missing_ids.map((id) => names.get(id) ?? id);
          const done = `${organizationCount(result.updated_ids.length)} set to ${accountType}.`;
          // A bulk write that quietly did less than it said is worse than one
          // that failed, so what was not written is shown as well as spoken.
          const report = missing.length
            ? `${done} ${organizationCount(missing.length)} matched nothing and were left unchanged: ${missing.join(", ")}.`
            : null;
          showFailure(report);
          announce(report ?? done);
          onDone();
        },
        // The dialog stays open holding the reason, because that is where the
        // operator is looking.
        onError: (error) =>
          announce(`Could not set the account type: ${errorMessage(error)}`),
      },
    );
  };

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button ref={trigger} variant="outline" size="xs">
            Set account type
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start">
          {ACCOUNT_TYPE_OPTIONS.map((option) => (
            <DropdownMenuItem
              key={option}
              // Opens the confirmation and writes nothing. The count is the
              // thing an operator gets wrong, and this is the last place they
              // can read it back before every ticked row changes.
              onSelect={() => {
                bulk.reset();
                setPending(option);
              }}
            >
              {option}
            </DropdownMenuItem>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>

      {pending !== undefined && (
        <Dialog
          open
          onOpenChange={(next) => {
            if (!next && !bulk.isPending) setPending(undefined);
          }}
        >
          <DialogContent
            onCloseAutoFocus={(event) => {
              // The control is gone once a write has cleared the selection, and
              // there is nowhere better to put the keyboard then.
              const control = trigger.current;
              if (!control?.isConnected) return;
              event.preventDefault();
              control.focus();
            }}
          >
            <DialogHeader>
              <DialogTitle>
                Set {organizationCount(selected.length)} to {pending}?
              </DialogTitle>
              <DialogDescription>
                Every ticked organization changes account type. Nothing else
                about them changes.
              </DialogDescription>
            </DialogHeader>
            {/* A modal takes the rest of the page out of the accessibility
                tree, the live region included, so a failure the operator is
                looking at needs its own. */}
            {bulk.error && (
              <p role="alert" className="text-destructive text-sm">
                {errorMessage(bulk.error)}
              </p>
            )}
            <DialogFooter>
              <Button
                variant="ghost"
                size="sm"
                disabled={bulk.isPending}
                onClick={() => setPending(undefined)}
              >
                Cancel
              </Button>
              <Button
                size="sm"
                disabled={bulk.isPending}
                onClick={() => run(pending)}
              >
                {bulk.isPending ? "Setting..." : `Set to ${pending}`}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </>
  );
}
