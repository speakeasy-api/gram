import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useId, useState, type FormEvent, type JSX } from "react";

import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
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
  invalidateOrganizations,
  invalidateOrganizationStats,
  organizationQuery,
  organizationsListQuery,
} from "@/lib/adminQueries";
import {
  createOrganization,
  errorMessage,
  GramAdminError,
  type AdminOrganization,
  type CreateOrganizationRequest,
} from "@/lib/gramAdminApi";

import type { WriteReporter } from "./OrganizationActions";
import {
  organizationSuggestionsFromHostname,
  organizationUrlPreview,
} from "./organizationUrl";

/**
 * Creates one organization from an operator-confirmed domain, with editable
 * suggestions for its display name and URL slug.
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
  const urlField = useId();
  const nameField = useId();
  const nameHintID = useId();
  const slugField = useId();
  const slugHintID = useId();
  const ownershipField = useId();
  const previewID = useId();
  const messageID = useId();

  const [open, setOpen] = useState(false);
  const [url, setURL] = useState("");
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [nameEdited, setNameEdited] = useState(false);
  const [slugEdited, setSlugEdited] = useState(false);
  const [ownershipConfirmed, setOwnershipConfirmed] = useState(false);
  // What the toolbar shows after a create lands. It is spoken through the
  // page's region, which is the only live region on this page.
  const [created, setCreated] = useState("");

  const create = useMutation({
    retry: false,
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
      // Ahead of the close, so the order the two reach the accessibility tree
      // in is never a question.
      setOpen(false);
      setURL("");
      setName("");
      setSlug("");
      setNameEdited(false);
      setSlugEdited(false);
      setOwnershipConfirmed(false);
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

  const preview = organizationUrlPreview(url);
  const slugValid =
    slug.length >= 2 &&
    slug.length <= 100 &&
    /^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(slug);
  const canSubmit =
    Boolean(preview.hostname) &&
    Boolean(name.trim()) &&
    slugValid &&
    ownershipConfirmed &&
    !create.isPending;
  const failure = create.error
    ? create.error instanceof GramAdminError && create.error.status < 500
      ? errorMessage(create.error)
      : "Creation could not be confirmed. Check existing organizations before retrying."
    : preview.error;

  const submit = (event: FormEvent<HTMLFormElement>): void => {
    event.preventDefault();
    // Not the button's `disabled` alone: Enter in the field submits the form,
    // so a second press mid-write would create a second organization.
    if (!canSubmit) return;
    create.mutate({
      url: url.trim(),
      name: name.trim(),
      slug,
      ownership_confirmed: ownershipConfirmed,
    });
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
            setURL("");
            setName("");
            setSlug("");
            setNameEdited(false);
            setSlugEdited(false);
            setOwnershipConfirmed(false);
            create.reset();
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
          <form onSubmit={submit}>
            <DialogHeader>
              <DialogTitle>Create an organization</DialogTitle>
              <DialogDescription>
                It is created with no members, no trial and the free account
                type. Members are invited from the organization itself.
              </DialogDescription>
            </DialogHeader>

            <div className="my-4 grid gap-2">
              <label htmlFor={urlField} className="text-sm font-medium">
                Company URL
              </label>
              <Input
                id={urlField}
                value={url}
                type="text"
                inputMode="url"
                placeholder="https://example.com"
                disabled={create.isPending}
                required
                autoComplete="off"
                autoCapitalize="none"
                spellCheck={false}
                aria-invalid={Boolean(failure)}
                aria-describedby={
                  failure ? `${previewID} ${messageID}` : previewID
                }
                onChange={(event) => {
                  const nextURL = event.target.value;
                  const nextPreview = organizationUrlPreview(nextURL);
                  const suggestions = organizationSuggestionsFromHostname(
                    nextPreview.hostname,
                  );
                  setURL(nextURL);
                  if (!nameEdited) setName(suggestions.name);
                  if (!slugEdited) setSlug(suggestions.slug);
                  setOwnershipConfirmed(false);
                  create.reset();
                }}
              />
              <div id={previewID} className="text-muted-foreground text-sm">
                <p className="break-all">
                  Trusted email domain:{" "}
                  {preview.hostname || "Enter a company URL"}
                </p>
                <p>Invitations must match this domain exactly.</p>
              </div>
            </div>

            <div className="my-4 grid gap-2">
              <label htmlFor={nameField} className="text-sm font-medium">
                Organization name
              </label>
              <Input
                id={nameField}
                value={name}
                type="text"
                disabled={create.isPending}
                required
                maxLength={100}
                autoComplete="off"
                aria-invalid={Boolean(failure)}
                aria-describedby={
                  failure ? `${nameHintID} ${messageID}` : nameHintID
                }
                onChange={(event) => {
                  setName(event.target.value);
                  setNameEdited(true);
                  create.reset();
                }}
              />
              <p id={nameHintID} className="text-muted-foreground text-sm">
                Suggested from the company domain. Edit it if the customer uses
                a different name.
              </p>
            </div>

            <div className="my-4 grid gap-2">
              <label htmlFor={slugField} className="text-sm font-medium">
                Organization slug
              </label>
              <Input
                id={slugField}
                value={slug}
                type="text"
                disabled={create.isPending}
                required
                minLength={2}
                maxLength={100}
                pattern="[a-z0-9]+(?:-[a-z0-9]+)*"
                autoComplete="off"
                autoCapitalize="none"
                spellCheck={false}
                aria-invalid={Boolean(failure) || (Boolean(slug) && !slugValid)}
                aria-describedby={
                  failure ? `${slugHintID} ${messageID}` : slugHintID
                }
                onChange={(event) => {
                  setSlug(event.target.value);
                  setSlugEdited(true);
                  create.reset();
                }}
              />
              <p
                id={slugHintID}
                className={
                  slug && !slugValid
                    ? "text-destructive text-sm"
                    : "text-muted-foreground text-sm"
                }
              >
                {slug && !slugValid
                  ? "Use 2–100 lowercase letters, numbers or single hyphens between words."
                  : "Suggested from the company domain. If unavailable, a short suffix is added."}
              </p>
            </div>

            <div className="my-4 grid gap-2">
              <div className="flex items-start gap-2">
                <Checkbox
                  id={ownershipField}
                  className="mt-0.5"
                  checked={ownershipConfirmed}
                  disabled={create.isPending}
                  required
                  onCheckedChange={(checked) =>
                    setOwnershipConfirmed(checked === true)
                  }
                />
                <label htmlFor={ownershipField} className="text-sm leading-5">
                  I have confirmed that this organization owns this domain.
                </label>
              </div>
              <p className="text-muted-foreground text-sm">
                Establish ownership outside this form. Entering a URL does not
                prove ownership.
              </p>
            </div>

            {failure && (
              <p
                id={messageID}
                role="alert"
                className="text-destructive text-sm"
              >
                {failure}
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
