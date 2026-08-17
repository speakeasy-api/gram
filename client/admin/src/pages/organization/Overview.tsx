import { useState, type JSX } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";

import { useConfirmDialog } from "@/components/ConfirmDialog";
import { CopyValue } from "@/components/CopyValue";
import { Trial } from "@/components/Trial";
import { Button } from "@/components/ui/button";
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
  // Keyed, so an unsaved draft cannot follow the operator to another record.
  return <Overview key={data.id} org={data} />;
}

export function Overview({ org }: { org: AdminOrganization }): JSX.Element {
  const qc = useQueryClient();
  const [confirm, confirmDialog] = useConfirmDialog();

  // Only the fields the operator touched live here. The rest read straight
  // from the server record, so a refetch cannot discard an unsaved edit and
  // cannot leave an untouched field showing a stale value.
  const [draft, setDraft] = useState<{
    account_type?: string;
    whitelisted?: boolean;
  }>({});
  const accountType = draft.account_type ?? org.account_type;
  const whitelisted = draft.whitelisted ?? org.whitelisted;

  const mut = useMutation({
    mutationFn: () =>
      updateOrganization({
        id: org.id,
        account_type:
          accountType !== org.account_type ? accountType : undefined,
        whitelisted: whitelisted !== org.whitelisted ? whitelisted : undefined,
      }),
    // Through `adminQueries`, like every other admin write. A copy of the cache
    // path spelled out here is how one of its caches gets left out: this one
    // cancelled nothing, so a list read already in flight put the pre-write row
    // back, and the stats kept their old totals.
    onMutate: () => cancelOrganizationFetches(qc),
    onSuccess: (updated) => {
      setDraft({});
      writeOrganizationToCache(qc, updated);
    },
    // A failed write replaces nothing it cancelled, so the totals have to be
    // asked for again. The record needs nothing: it was never repainted.
    onError: () => invalidateOrganizationStats(qc),
  });

  const dirty =
    accountType !== org.account_type || whitelisted !== org.whitelisted;

  const handleCancel = () => {
    setDraft({});
  };

  const handleSave = async () => {
    const changes: string[] = [];
    if (accountType !== org.account_type) {
      changes.push(`Account type: ${org.account_type} → ${accountType}`);
    }
    if (whitelisted !== org.whitelisted) {
      changes.push(
        `Whitelisted: ${yesNo(org.whitelisted)} → ${yesNo(whitelisted)}`,
      );
    }

    const confirmed = await confirm({
      title: `Update ${org.name}?`,
      description: changes.join(". ") + ".",
      confirmLabel: "Save",
    });
    if (confirmed) {
      mut.mutate();
    }
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
            value={accountType}
            disabled={mut.isPending}
            onValueChange={(v) => setDraft((d) => ({ ...d, account_type: v }))}
          >
            <SelectTrigger className="h-auto w-auto px-2 py-1.5">
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
            checked={whitelisted}
            disabled={mut.isPending}
            onCheckedChange={(v) => setDraft((d) => ({ ...d, whitelisted: v }))}
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

      {dirty && (
        <div className="border-border mt-4 flex items-center gap-2 border-t pt-3">
          <Button
            variant="default"
            size="xs"
            disabled={mut.isPending}
            onClick={() => {
              void handleSave();
            }}
          >
            {mut.isPending ? "Saving..." : "Save"}
          </Button>
          <Button
            variant="ghost"
            size="xs"
            disabled={mut.isPending}
            onClick={handleCancel}
          >
            Cancel
          </Button>
          {mut.isError && (
            <span className="text-muted-foreground text-sm">
              Error: {errorMessage(mut.error)}
            </span>
          )}
        </div>
      )}

      {confirmDialog}
    </div>
  );
}
