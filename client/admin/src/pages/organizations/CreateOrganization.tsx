import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useId, useState, type FormEvent, type JSX } from "react";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { DirectoryHandoffSection } from "@/pages/organization/DirectoryHandoff";
import {
  cancelOrganizationFetches,
  invalidateOrganizations,
  invalidateOrganizationStats,
  organizationQuery,
  organizationsListQuery,
} from "@/lib/adminQueries";
import {
  createOrganization,
  errorMessage,
  type AdminOrganization,
  type CreateOrganizationRequest,
} from "@/lib/gramAdminApi";

import type { WriteReporter } from "./OrganizationActions";

/**
 * Creates one organization from a name.
 *
 * The confirmation is worded from the response, because the server normalises
 * the name it stores.
 */
export function CreateOrganization({
  reporter,
}: {
  // Handed down rather than taken from context: this control is drawn above the
  // table by the page that owns the live region, not from inside a row.
  reporter: WriteReporter;
}): JSX.Element {
  const { announce, showFailure } = reporter;
  const qc = useQueryClient();
  const nameField = useId();
  const messageID = useId();

  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  // What the toolbar shows after a create lands. It is spoken through the
  // page's region, which is the only live region on this page.
  const [created, setCreated] = useState("");
  // The organization the dialog is now on its second step for. A create is the
  // moment the operator has the WorkOS tab open and the endpoint in hand, so
  // the directory handoff is offered here rather than left for a later visit to
  // the record. Held rather than read from the cache: this is the one record
  // the dialog is about, and it cannot be the wrong one.
  const [createdOrg, setCreatedOrg] = useState<AdminOrganization | null>(null);

  const create = useMutation({
    mutationFn: (body: CreateOrganizationRequest) => createOrganization(body),
    // A list read already in flight was asked before this write and lands
    // after it, so it would answer the refetch below with pre-write rows.
    onMutate: () => cancelOrganizationFetches(qc),
    onSuccess: (org: AdminOrganization) => {
      // Named, because the row is not proof: a filtered list is right to leave
      // a new free-tier organization out.
      const done = `Created ${org.name}. The list may not show it under the current filters.`;
      setCreated(done);
      announce(done);
      // The dialog stays open on its second step rather than closing: the
      // handoff is the other half of creating a customer. Announced first, so
      // the order the two reach the accessibility tree in is never a question.
      setCreatedOrg(org);
      setName("");
      // The page's banner belongs to the last write that failed, and this one
      // succeeded. Every sibling write clears it the same way.
      showFailure(null);

      // The detail route takes either, and the response carries both, so
      // opening the new organization paints without a fetch first.
      qc.setQueryData(organizationQuery(org.id).queryKey, org);
      if (org.slug) qc.setQueryData(organizationQuery(org.slug).queryKey, org);

      // Invalidated rather than spliced into the page on screen: the sort, the
      // filters and the cursor decide where the new row belongs.
      invalidateOrganizationStats(qc);
      return qc.invalidateQueries({
        queryKey: organizationsListQuery().queryKey,
      });
    },
    // Everything onMutate cancelled. A cancelled read reverts and nothing
    // restarts it, so the table would report no organizations at all.
    onError: () => invalidateOrganizations(qc),
  });

  // The server normalises whitespace itself, so the trim only decides what
  // counts as empty and keeps the request the same as the check.
  const trimmed = name.trim();
  const canSubmit = trimmed !== "" && !create.isPending;

  const submit = (event: FormEvent<HTMLFormElement>): void => {
    event.preventDefault();
    // Not the button's `disabled` alone: Enter in the field submits the form,
    // so a second press mid-write would create a second organization.
    if (!canSubmit) return;
    create.mutate({ name: trimmed });
  };

  return (
    <>
      {/* Read as well as heard. The created row need not appear on the page, so
          without this a sighted operator has no account of the write at all. */}
      <span
        title={created}
        className="text-muted-foreground min-w-0 truncate text-xs"
      >
        {created}
      </span>

      <Dialog
        open={open}
        onOpenChange={(next) => {
          // Escape and the overlay both come through here, and neither may
          // take the dialog away from a write that is still open.
          if (!next && create.isPending) return;
          if (next) {
            setName("");
            create.reset();
            // The last organization's handoff step would otherwise be what a
            // second create opens on.
            setCreatedOrg(null);
            // The last create's confirmation would otherwise sit beside this
            // one's refusal.
            setCreated("");
          }
          setOpen(next);
        }}
      >
        <DialogTrigger asChild>
          <Button size="sm">Create organization</Button>
        </DialogTrigger>
        {/* No focus rescue, unlike the dialogs in OrganizationActions.tsx:
            those have no trigger to restore through and this one is mounted
            for the life of the page. */}
        <DialogContent showCloseButton={!create.isPending}>
          {createdOrg ? (
            // Outside the form deliberately. A button with no type submits the
            // form that encloses it, and every control on the handoff step
            // would otherwise create a second organization.
            <div>
              <DialogHeader>
                <DialogTitle>Organization created</DialogTitle>
                <DialogDescription>{created}</DialogDescription>
              </DialogHeader>

              <div className="my-4 max-h-[60vh] overflow-y-auto pr-1">
                <h3 className="mb-3 text-sm font-semibold">
                  Directory handoff
                </h3>
                {/* The page's live region is above the table, outside the
                    provider the record pages use, so the reporter is handed
                    down the way it is to every other write here. */}
                <DirectoryHandoffSection org={createdOrg} reporter={reporter} />
              </div>

              <DialogFooter className="mt-4">
                <Button size="sm" onClick={() => setOpen(false)}>
                  Done
                </Button>
              </DialogFooter>
            </div>
          ) : (
            <form onSubmit={submit}>
              <DialogHeader>
                <DialogTitle>Create an organization</DialogTitle>
                <DialogDescription>
                  It is created with no members, no trial and the free account
                  type. Members are invited from the organization itself.
                </DialogDescription>
              </DialogHeader>

              <div className="my-4 grid gap-2">
                <label htmlFor={nameField} className="text-sm font-medium">
                  Organization name
                </label>
                <Input
                  id={nameField}
                  value={name}
                  required
                  autoComplete="off"
                  aria-invalid={Boolean(create.error)}
                  // The alert fires once. An operator who tabs back to edit the
                  // rejected name would otherwise be told nothing at all.
                  aria-describedby={create.error ? messageID : undefined}
                  onChange={(event) => setName(event.target.value)}
                />
              </div>

              {/* Assertive and beside the field it belongs to, which is where
                the operator is. The name stays put: a rejected name is one they
                want to edit, not retype. */}
              {create.error && (
                <p
                  id={messageID}
                  role="alert"
                  className="text-destructive text-sm"
                >
                  {errorMessage(create.error)}
                </p>
              )}

              <DialogFooter className="mt-4">
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  disabled={create.isPending}
                  onClick={() => setOpen(false)}
                >
                  Cancel
                </Button>
                <Button type="submit" size="sm" disabled={!canSubmit}>
                  {create.isPending ? "Creating..." : "Create"}
                </Button>
              </DialogFooter>
            </form>
          )}
        </DialogContent>
      </Dialog>
    </>
  );
}
