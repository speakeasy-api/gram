import { useEffect, useRef, type JSX } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";

import { useConfirmDialog } from "@/components/ConfirmDialog";
import { CopyValue } from "@/components/CopyValue";
import { Trial } from "@/components/Trial";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { ACCOUNT_TYPE_OPTIONS, isAccountType } from "@/lib/accountTypes";
import {
  cancelOrganizationFetches,
  invalidateOrganizationStats,
  organizationQuery,
  writeOrganizationToCache,
} from "@/lib/adminQueries";
import {
  errorMessage,
  updateOrganization,
  type AdminOrganization,
} from "@/lib/gramAdminApi";
import { cn, fmtDateShort } from "@/lib/utils";
import { useWriteReport } from "@/pages/organizations/writeReport";

function Row({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="grid grid-cols-[12rem_1fr] items-baseline gap-3 py-1">
      <span data-slot="field-label" className="text-muted-foreground text-sm">
        {label}
      </span>
      <div>{children}</div>
    </div>
  );
}

function Group({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}) {
  return (
    <section className="mt-5 first:mt-0">
      {/* h5 under the record name's h4 in RecordHeader. A group is part of the
          record, not a sibling of it. */}
      <h5 className="text-muted-foreground mb-1 text-xs font-medium">
        {title}
      </h5>
      {children}
    </section>
  );
}

function yesNo(v: boolean): string {
  return v ? "yes" : "no";
}

// The view reads the record from the same query the layout above it reads, so
// the two hold one answer per render. A file route renders through `<Outlet/>`
// and cannot be handed a prop.
export function OverviewRoute(): JSX.Element | null {
  const { idOrSlug } = useParams({ from: "/organizations/$idOrSlug" });
  const { data } = useQuery(organizationQuery(idOrSlug));
  if (!data) return null;
  // Keyed, so a write in flight and the dialog asking about it belong to the
  // record they were started on. `RecordLayout` keys this whole subtree the
  // same way, which is what actually enforces it today; this one holds if the
  // view is ever mounted somewhere that does not.
  return <Overview key={data.id} org={data} />;
}

// One field per write. The endpoint takes both as optional, and a type that
// allows both would let a write carry a field the operator never touched.
type FactChange = { account_type: string } | { whitelisted: boolean };

export function Overview({ org }: { org: AdminOrganization }): JSX.Element {
  const qc = useQueryClient();
  const [confirm, confirmDialog] = useConfirmDialog();
  const { announce, showFailure } = useWriteReport();

  // Where the keyboard goes when the dialog closes. `useConfirmDialog` has no
  // `DialogTrigger`, so Radix's own restore drops focus on `document.body`.
  const accountTypeControl = useRef<HTMLButtonElement>(null);
  const whitelistedControl = useRef<HTMLButtonElement>(null);
  const openedFrom = useRef<HTMLButtonElement | null>(null);

  const mut = useMutation({
    mutationFn: (change: FactChange) =>
      updateOrganization({ id: org.id, ...change }),
    // Through `adminQueries`, like every other admin write. A copy of the cache
    // path spelled out here is how one of its caches gets left out: this one
    // cancelled nothing, so a list read already in flight put the pre-write row
    // back, and the stats kept their old totals.
    onMutate: () => cancelOrganizationFetches(qc),
    onSuccess: (updated) => writeOrganizationToCache(qc, updated),
    // A failed write replaces nothing it cancelled, so the totals have to be
    // asked for again. The record needs nothing: it was never repainted.
    onError: () => invalidateOrganizationStats(qc),
  });

  // The confirmed path cannot restore focus itself: it disables the control it
  // would focus, and a browser drops focus to the body when that happens.
  // Re-enabling does not bring it back, so the restore waits for the write to
  // settle and runs from here, after React has taken `disabled` off again.
  // Asked for at `mutate` rather than read off a pending render, because a write
  // that settles fast never commits one.
  const restoreWanted = useRef(false);
  useEffect(() => {
    if (mut.isPending || !restoreWanted.current) return;
    restoreWanted.current = false;
    // A control that has left the page is not somewhere to put the keyboard.
    if (openedFrom.current?.isConnected) openedFrom.current.focus();
  }, [mut.status, mut.isPending]);

  const commit = async (
    change: FactChange,
    // Old → new, in the operator's words. The dialog asks it and the
    // announcement reports it, so the two cannot name different changes.
    describe: string,
    control: React.RefObject<HTMLButtonElement | null>,
  ): Promise<void> => {
    openedFrom.current = control.current;
    const confirmed = await confirm({
      title: `Update ${org.name}?`,
      description: `${describe}.`,
      confirmLabel: "Save",
    });
    if (!confirmed) {
      // Nothing disables the control on this exit, so it takes the keyboard now.
      control.current?.focus();
      return;
    }

    // A new write does not run under the last one's failure.
    showFailure(null);
    restoreWanted.current = true;
    mut.mutate(change, {
      onSuccess: () => announce(`${org.name} updated. ${describe}.`),
      onError: (error) => {
        const text = `Could not update ${org.name}: ${errorMessage(error)}`;
        announce(text);
        showFailure(text);
      },
    });
  };

  return (
    <div className="border-border bg-muted/10 rounded-md border p-4">
      <Group title="Identity">
        <Row label="Name">
          <span className="text-sm">{org.name}</span>
        </Row>
        <Row label="Slug">
          <CopyValue label="Slug" value={org.slug} className="text-sm" />
        </Row>
        <Row label="Organization id">
          <CopyValue
            label="Organization id"
            value={org.id}
            className="text-sm"
          />
        </Row>
        <Row label="WorkOS id">
          {/* No control over an absent value: a button that copies "-" is
              worse than no button. */}
          {org.workos_id ? (
            <CopyValue
              label="WorkOS id"
              value={org.workos_id}
              className="text-sm"
            />
          ) : (
            <span className="text-muted-foreground text-sm">-</span>
          )}
        </Row>
        <Row label="Created">
          <span className="text-sm">{fmtDateShort(org.created_at)}</span>
        </Row>
        <Row label="Updated">
          <span className="text-sm">{fmtDateShort(org.updated_at)}</span>
        </Row>
      </Group>

      <Group title="Plan">
        <Row label="Account type">
          <Select
            value={org.account_type}
            disabled={mut.isPending}
            onValueChange={(v) => {
              void commit(
                { account_type: v },
                `Account type: ${org.account_type} → ${v}`,
                accountTypeControl,
              );
            }}
          >
            <SelectTrigger
              ref={accountTypeControl}
              className="h-auto w-auto px-2 py-1.5"
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {ACCOUNT_TYPE_OPTIONS.map((t) => (
                <SelectItem key={t} value={t}>
                  {t}
                </SelectItem>
              ))}
              {!isAccountType(org.account_type) && (
                <SelectItem value={org.account_type}>
                  {org.account_type}
                </SelectItem>
              )}
            </SelectContent>
          </Select>
        </Row>
        <Row label="Trial">
          <Trial org={org} />
        </Row>
      </Group>

      {/* Access, not a setting. `whitelisted` gates the platform, so it keeps
          its own group away from anything that reads as a preference. */}
      <Group title="Access">
        <Row label="Whitelisted">
          <Switch
            ref={whitelistedControl}
            checked={org.whitelisted}
            disabled={mut.isPending}
            onCheckedChange={(v) => {
              void commit(
                { whitelisted: v },
                `Whitelisted: ${yesNo(org.whitelisted)} → ${yesNo(v)}`,
                whitelistedControl,
              );
            }}
          />
        </Row>
        <Row label="Disabled at">
          <span
            className={cn(
              "text-sm",
              !org.disabled_at && "text-muted-foreground",
            )}
          >
            {fmtDateShort(org.disabled_at)}
          </span>
        </Row>
      </Group>

      {confirmDialog}
    </div>
  );
}
