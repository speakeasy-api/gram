import { useMutation, useQueryClient } from "@tanstack/react-query";
import { PowerOffIcon } from "lucide-react";
import { useId, useRef, useState, type JSX } from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  cancelOrganizationFetches,
  invalidateOrganizations,
} from "@/lib/adminQueries";
import { errorMessage } from "@/lib/gramAdminApi";
import {
  bulkDisableOrganizations,
  resolveOrganizations,
} from "@/lib/gramAdminClient";
import type { AdminBulkDisableOrganizationsResult } from "@gram/admin-client/models/components/adminbulkdisableorganizationsresult";
import type { AdminResolveOrganizationsResult } from "@gram/admin-client/models/components/adminresolveorganizationsresult";

import {
  MAX_BULK_DISABLE_CSV_BYTES,
  parseBulkDisableCsv,
  type BulkDisableTargets,
} from "./bulkDisableCsv";
import type { WriteReporter } from "./OrganizationActions";

function organizationCount(count: number): string {
  return `${count} ${count === 1 ? "organization" : "organizations"}`;
}

/**
 * Disables every organization a CSV names. Three steps, and only the last one
 * writes: the file is checked for an id or slug column, the server says what
 * each identifier points at, and the operator confirms that list.
 *
 * What is disabled is the reviewed list's ids, never the file's slugs, so the
 * write acts on exactly the rows the operator read.
 */
export function BulkDisable({
  reporter,
}: {
  // Handed down rather than taken from context: this control is drawn above the
  // table by the page that owns the live region, not from inside a row.
  reporter: WriteReporter;
}): JSX.Element {
  const { announce, showFailure } = reporter;
  const qc = useQueryClient();
  const fileField = useId();
  const fileHintID = useId();

  const [open, setOpen] = useState(false);
  const [fileError, setFileError] = useState("");
  const [review, setReview] = useState<AdminResolveOrganizationsResult>();
  const [outcome, setOutcome] = useState<AdminBulkDisableOrganizationsResult>();
  // A file picked while an earlier one is still being read or resolved
  // supersedes it; only the latest pick may land.
  const pick = useRef(0);

  const resolve = useMutation({
    retry: false,
    mutationFn: (targets: BulkDisableTargets) =>
      resolveOrganizations({ ids: targets.ids, slugs: targets.slugs }),
  });

  const disable = useMutation({
    retry: false,
    mutationFn: (ids: string[]) => bulkDisableOrganizations({ ids }),
    onMutate: () => cancelOrganizationFetches(qc),
    onSuccess: (result) => {
      setOutcome(result);
      // A banner from an earlier write on this page no longer describes it.
      showFailure(null);
      const done = `Disabled ${organizationCount(result.disabledIds.length)}.`;
      announce(
        result.missingIds.length
          ? `${done} ${organizationCount(result.missingIds.length)} no longer existed and stayed unchanged.`
          : done,
      );
    },
    // The dialog stays open holding the reason, because that is where the
    // operator is looking; the page's live region hears it too.
    onError: (error) =>
      announce(`Could not disable organizations: ${errorMessage(error)}`),
    // Settled rather than success: a batch that failed may still have raced a
    // change from someone else, and the list should show what is there now.
    onSettled: () => invalidateOrganizations(qc),
  });

  const busy = resolve.isPending || disable.isPending;

  function reset(): void {
    pick.current++;
    setFileError("");
    setReview(undefined);
    setOutcome(undefined);
    resolve.reset();
    disable.reset();
  }

  function changeOpen(next: boolean): void {
    // A disable in flight cannot be called back, so the dialog that reports it
    // stays up until it lands.
    if (disable.isPending) return;
    reset();
    setOpen(next);
  }

  async function readFile(file: File | undefined): Promise<void> {
    reset();
    const current = pick.current;
    if (!file) return;
    if (file.size > MAX_BULK_DISABLE_CSV_BYTES) {
      setFileError("CSV must be 1 MB or smaller.");
      return;
    }
    let targets: BulkDisableTargets;
    try {
      targets = parseBulkDisableCsv(await file.text());
    } catch (error) {
      if (current === pick.current) setFileError(errorMessage(error));
      return;
    }
    if (current !== pick.current) return;
    resolve.mutate(targets, {
      onSuccess: (result) => {
        if (current === pick.current) setReview(result);
      },
    });
  }

  const missing = review ? [...review.missingIds, ...review.missingSlugs] : [];
  const targets = review?.organizations ?? [];

  return (
    <>
      <Button variant="outline" onClick={() => changeOpen(true)}>
        <PowerOffIcon aria-hidden="true" />
        Bulk disable
      </Button>
      <Dialog open={open} onOpenChange={changeOpen}>
        <DialogContent
          showCloseButton={!disable.isPending}
          className="max-h-[90vh] overflow-y-auto sm:max-w-4xl"
        >
          <DialogHeader>
            <DialogTitle>Bulk disable organizations</DialogTitle>
            <DialogDescription>
              Upload a CSV with an <code>id</code> or <code>slug</code> column.
              Each organization it names is removed from the whitelist and
              disabled. Nothing changes until you confirm the list.
            </DialogDescription>
          </DialogHeader>

          {outcome ? (
            <Outcome outcome={outcome} />
          ) : (
            // min-w-0: the dialog lays its children out on a grid, and a grid
            // item grows to its widest content, so without this a long id
            // pushes the table, and the footer with it, past the dialog's edge.
            <div className="min-w-0 space-y-4">
              <div className="space-y-1.5">
                <label htmlFor={fileField} className="text-sm font-medium">
                  CSV file
                </label>
                <Input
                  id={fileField}
                  type="file"
                  accept=".csv,text/csv"
                  aria-describedby={fileHintID}
                  disabled={busy}
                  // Cleared on every open of the picker, so choosing the same
                  // file again after fixing it, or after a failed lookup, still
                  // fires a change.
                  onClick={(event) => {
                    event.currentTarget.value = "";
                  }}
                  onChange={(event) => {
                    void readFile(event.target.files?.[0]);
                  }}
                />
                <p id={fileHintID} className="text-muted-foreground text-xs">
                  Other columns are ignored. A row with both uses its id. Up to
                  1,000 organizations per file.
                </p>
              </div>

              {resolve.isPending && (
                <p role="status" className="text-sm">
                  Looking up organizations…
                </p>
              )}
              {(fileError || resolve.error) && (
                <p role="alert" className="text-destructive text-sm">
                  {fileError ||
                    `Could not look up organizations: ${errorMessage(resolve.error)}`}
                </p>
              )}

              {review && <Review organizations={targets} missing={missing} />}

              {disable.error && (
                <p role="alert" className="text-destructive text-sm">
                  Nothing was disabled: {errorMessage(disable.error)}
                </p>
              )}
            </div>
          )}

          <DialogFooter>
            {outcome ? (
              <Button onClick={() => changeOpen(false)}>Done</Button>
            ) : (
              <>
                <Button
                  variant="ghost"
                  disabled={disable.isPending}
                  onClick={() => changeOpen(false)}
                >
                  Cancel
                </Button>
                <Button
                  variant="destructive"
                  disabled={busy || targets.length === 0}
                  onClick={() => disable.mutate(targets.map((org) => org.id))}
                >
                  {disable.isPending
                    ? "Disabling…"
                    : targets.length
                      ? `Disable ${organizationCount(targets.length)}`
                      : "Disable"}
                </Button>
              </>
            )}
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}

function Review({
  organizations,
  missing,
}: {
  organizations: AdminResolveOrganizationsResult["organizations"];
  missing: string[];
}): JSX.Element {
  return (
    <div className="space-y-4">
      {missing.length > 0 && (
        <div
          role="status"
          className="border-destructive/40 bg-destructive/5 space-y-1 rounded-md border p-3"
        >
          <p className="text-sm font-medium">
            {organizationCount(missing.length)} from the CSV could not be found
            and will be skipped:
          </p>
          <ul className="text-muted-foreground max-h-32 overflow-y-auto font-mono text-xs">
            {missing.map((identifier, index) => (
              // An id and a slug can be spelled the same, so the text alone is
              // not a unique key.
              <li key={`${index}:${identifier}`}>{identifier}</li>
            ))}
          </ul>
        </div>
      )}

      {organizations.length === 0 ? (
        <p className="text-sm">
          No organization in the CSV was found, so there is nothing to disable.
        </p>
      ) : (
        <div className="space-y-1.5">
          <p className="text-sm font-medium">
            {organizationCount(organizations.length)} will be disabled
          </p>
          <div className="max-h-80 overflow-auto rounded-md border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>ID</TableHead>
                  <TableHead>Slug</TableHead>
                  <TableHead>Name</TableHead>
                  <TableHead>Account type</TableHead>
                  <TableHead>Status</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {organizations.map((org) => (
                  <TableRow key={org.id}>
                    <TableCell className="font-mono text-xs">
                      {org.id}
                    </TableCell>
                    <TableCell className="font-mono text-xs">
                      {org.slug}
                    </TableCell>
                    <TableCell>{org.name}</TableCell>
                    <TableCell>{org.accountType}</TableCell>
                    <TableCell>
                      {org.disabledAt ? (
                        <Badge variant="secondary">Already disabled</Badge>
                      ) : (
                        <Badge variant="outline">Active</Badge>
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        </div>
      )}
    </div>
  );
}

function Outcome({
  outcome,
}: {
  outcome: AdminBulkDisableOrganizationsResult;
}): JSX.Element {
  return (
    <div role="status" className="space-y-2 text-sm">
      <p>
        Disabled {organizationCount(outcome.disabledIds.length)} and removed{" "}
        {outcome.disabledIds.length === 1 ? "it" : "them"} from the whitelist.
      </p>
      {/* Reviewed, then gone by the time the write ran. Said rather than
          dropped, or the count above reads as the whole list. */}
      {outcome.missingIds.length > 0 && (
        <div>
          <p>
            {organizationCount(outcome.missingIds.length)} no longer existed and
            stayed unchanged:
          </p>
          <ul className="text-muted-foreground font-mono text-xs">
            {outcome.missingIds.map((id) => (
              <li key={id}>{id}</li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}
