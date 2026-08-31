import { useRef, type JSX, type Ref } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";

import { useConfirmDialog } from "@/components/ConfirmDialog";
import { CopyValue } from "@/components/CopyValue";
import { TrialFacts } from "@/pages/organization/TrialFacts";
import { OrganizationActions } from "@/pages/organizations/OrganizationActions";
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
  invalidateOrganizationDetails,
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
    <div className="grid grid-cols-1 gap-1 py-1 sm:grid-cols-[7.5rem_minmax(0,1fr)] sm:items-center sm:gap-3">
      <span data-slot="field-label" className="text-muted-foreground text-sm">
        {label}
      </span>
      <div>{children}</div>
    </div>
  );
}

function Panel({
  title,
  children,
  className,
  headingRef,
}: {
  title: string;
  children: React.ReactNode;
  className?: string;
  headingRef?: Ref<HTMLHeadingElement>;
}) {
  return (
    <section className={cn("bg-card rounded-lg border", className)}>
      {/* h5 follows the record name's h4 in RecordHeader. */}
      <h5
        ref={headingRef}
        tabIndex={headingRef ? -1 : undefined}
        className="border-b px-5 py-2.5 text-sm font-semibold"
      >
        {title}
      </h5>
      <div className="p-5">{children}</div>
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
  const detailsHeading = useRef<HTMLHeadingElement>(null);

  const mut = useMutation({
    mutationFn: (change: FactChange) =>
      updateOrganization({ id: org.id, ...change }),
    // Through `adminQueries`, like every other admin write. A copy of the cache
    // path spelled out here is how one of its caches gets left out: this one
    // cancelled nothing, so a list read already in flight put the pre-write row
    // back, and the stats kept their old totals.
    onMutate: () => cancelOrganizationFetches(qc),
    onSuccess: (updated) => {
      writeOrganizationToCache(qc, updated);
      invalidateOrganizationDetails(qc, updated);
    },
    // A failed write replaces nothing it cancelled, so the totals have to be
    // asked for again. The record needs nothing: it was never repainted.
    onError: () => invalidateOrganizationStats(qc),
  });

  const commit = async (
    change: FactChange,
    // Old → new, in the operator's words. The dialog asks it and the
    // announcement reports it, so the two cannot name different changes.
    describe: string,
    control: React.RefObject<HTMLButtonElement | null>,
  ): Promise<void> => {
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
    mut.mutate(change, {
      onSuccess: () => announce(`${org.name} updated. ${describe}.`),
      onError: (error) => {
        const text = `Could not update ${org.name}: ${errorMessage(error)}`;
        announce(text);
        showFailure(text);
      },
      onSettled: () => {
        setTimeout(function restoreControlFocus() {
          const target = control.current;
          if (!target?.isConnected) return;
          if (target.disabled) {
            setTimeout(restoreControlFocus);
            return;
          }
          target.focus();
        });
      },
    });
  };

  const showTrialPanel =
    org.trial_state === "running" ||
    org.trial_state === "ending_soon" ||
    org.trial_state === "expired" ||
    org.trial_state === "demoted";

  return (
    <div
      data-slot="organization-overview"
      className="flex flex-wrap items-start gap-4"
    >
      <div
        data-slot="organization-overview-main"
        className="min-w-[min(100%,32rem)] flex-[2_1_32rem] space-y-4"
      >
        <Panel title="Details" headingRef={detailsHeading}>
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
          {!showTrialPanel && (
            <Row label="Trial">
              <TrialFacts org={org} />
            </Row>
          )}
        </Panel>

        <Panel
          title="Danger zone"
          className="border-destructive [&>h5]:text-destructive"
        >
          <div className="flex flex-wrap items-center justify-between gap-4">
            <div className="min-w-0 flex-1">
              <p className="text-sm font-medium">
                {org.disabled_at
                  ? "Re-enable organization"
                  : "Disable organization"}
              </p>
              <p className="text-muted-foreground mt-0.5 text-sm">
                {org.disabled_at
                  ? `Disabled ${fmtDateShort(org.disabled_at)}. Re-enabling restores organization access for every member and takes effect at once. Model provider keys with admin, billing, or unknown disable causes remain disabled.`
                  : "Every member loses access to Gram until the organization is re-enabled. Sessions end immediately; nothing is deleted."}
              </p>
            </div>
            <OrganizationActions
              org={org}
              layout="buttons"
              actions="lifecycle"
              focusFallbackRef={detailsHeading}
              buttonClassName={
                org.disabled_at
                  ? undefined
                  : "border-destructive bg-destructive text-white hover:bg-destructive/90 hover:text-white"
              }
            />
          </div>
        </Panel>
      </div>

      {showTrialPanel && (
        <aside
          data-slot="organization-overview-trial"
          className="bg-card w-full max-w-80 flex-[1_1_16rem] rounded-lg border"
        >
          <h5 className="border-b px-5 py-2.5 text-sm font-semibold">
            Enterprise trial
          </h5>
          <div className="space-y-4 p-5">
            <TrialFacts org={org} />
            <OrganizationActions
              org={org}
              layout="buttons"
              actions="trial"
              focusFallbackRef={detailsHeading}
            />
          </div>
        </aside>
      )}

      {confirmDialog}
    </div>
  );
}
