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
import {
  cancelOrganizationFetches,
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
 * What the server makes is deliberately plain: no members, not whitelisted, no
 * trial, free tier. It also normalises the name, so the confirmation is worded
 * from the response rather than from what was typed.
 */
export function CreateOrganization({
  reporter,
}: {
  // Handed down rather than taken from context: this control is drawn above the
  // table by the page that owns the live region, not from inside a row.
  reporter: WriteReporter;
}): JSX.Element {
  const { announce } = reporter;
  const qc = useQueryClient();
  const nameField = useId();

  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  // What the toolbar shows after a create lands. Spoken through the page's live
  // region instead, which is the one region on this page.
  const [created, setCreated] = useState("");

  const create = useMutation({
    mutationFn: (body: CreateOrganizationRequest) => createOrganization(body),
    // A list read already in flight was asked before this write and lands
    // after it, so it would answer the refetch below with pre-write rows.
    onMutate: () => cancelOrganizationFetches(qc),
    onSuccess: (org: AdminOrganization) => {
      setOpen(false);
      setName("");
      // Named, because the row itself is not proof: the record is free tier
      // with no trial, so a list filtered to running trials correctly does not
      // show it, and the sort and the cursor decide the rest.
      const done = `Created ${org.name}. The list may not show it under the current filters.`;
      setCreated(done);
      announce(done);

      // The detail route takes either, and the response carries both, so
      // opening the new organization paints without a fetch first.
      qc.setQueryData(organizationQuery(org.id).queryKey, org);
      if (org.slug) qc.setQueryData(organizationQuery(org.slug).queryKey, org);

      // Invalidated rather than written into the page on screen. Where the new
      // row belongs depends on the sort, the filter and the cursor, none of
      // which this control can evaluate, so splicing it in would put it
      // somewhere the server would not have.
      invalidateOrganizationStats(qc);
      return qc.invalidateQueries({
        queryKey: organizationsListQuery().queryKey,
      });
    },
    // The cancelled read has nothing to replace it, so the strip would keep
    // three dashes until something else asked.
    onError: () => invalidateOrganizationStats(qc),
  });

  // The server normalises surrounding and repeated whitespace away and answers
  // with what it stored, so the trim only decides what counts as empty and
  // keeps the request the same as the check.
  const trimmed = name.trim();
  const canSubmit = trimmed !== "" && !create.isPending;

  const submit = (event: FormEvent<HTMLFormElement>): void => {
    event.preventDefault();
    // Not the button's `disabled` alone: Enter in the field submits the form,
    // and a second press while the first write is open would create a second
    // organization.
    if (!canSubmit) return;
    create.mutate({ name: trimmed });
  };

  return (
    <>
      {/* Read as well as heard. The created row need not appear on the page, so
          without this a sighted operator has no account of the write at all. */}
      <span className="text-muted-foreground min-w-0 truncate text-xs">
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
          }
          setOpen(next);
        }}
      >
        <DialogTrigger asChild>
          <Button size="sm">Create organization</Button>
        </DialogTrigger>
        <DialogContent showCloseButton={!create.isPending}>
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
                autoComplete="off"
                onChange={(event) => setName(event.target.value)}
              />
            </div>

            {/* A modal takes the rest of the page out of the accessibility
                tree, the page's live region included, so a refusal the operator
                is looking at needs its own. The name stays in the field: a
                rejected name is one they want to edit, not retype. */}
            {create.error && (
              <p role="alert" className="text-destructive text-sm">
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
        </DialogContent>
      </Dialog>
    </>
  );
}
