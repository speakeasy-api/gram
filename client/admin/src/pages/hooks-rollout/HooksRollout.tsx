import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState, type JSX } from "react";

import { useConfirmDialog } from "@/components/ConfirmDialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { hooksRolloutQuery, invalidateHooksRollout } from "@/lib/adminQueries";
import {
  errorMessage,
  setHooksRolloutDefault,
  type AdminHooksRollout,
  type AdminHooksRolloutChange,
} from "@/lib/gramAdminApi";
import {
  HOOKS_ROLLOUT_PROPAGATION_NOTE,
  validHooksRolloutVersion,
} from "@/lib/hooksRollout";
import { fmtDateShort } from "@/lib/utils";

/**
 * The hooks version rollout: which hooks generator version every customer
 * organization is cleared to receive. A new hooks release ships with the
 * server but reaches only canary organizations until the default pin, or an
 * organization's own override, is raised to it here.
 */
export function HooksRollout(): JSX.Element {
  const query = useQuery({ ...hooksRolloutQuery, throwOnError: false });

  if (query.isPending) return <p role="status">Loading hooks rollout…</p>;
  if (!query.data)
    return (
      <div className="space-y-3">
        <h1 className="text-2xl font-semibold">Hooks rollout</h1>
        <p role="alert">{errorMessage(query.error)}</p>
        <Button onClick={() => void query.refetch()}>Retry</Button>
      </div>
    );

  const rollout = query.data;
  return (
    <div className="space-y-6">
      <div className="space-y-1">
        <h1 className="text-2xl font-semibold">Hooks rollout</h1>
        <p className="text-muted-foreground text-sm">
          Controls which observability hooks plugin version customer
          organizations receive. A hooks release reaches canary organizations
          straight away and everyone else once their pin reaches it.{" "}
          {HOOKS_ROLLOUT_PROPAGATION_NOTE}
        </p>
      </div>
      <DefaultPin key={rollout.current_version} rollout={rollout} />
      <Overrides rollout={rollout} />
      <RecentChanges changes={rollout.recent_changes} />
    </div>
  );
}

function DefaultPin({ rollout }: { rollout: AdminHooksRollout }): JSX.Element {
  const queryClient = useQueryClient();
  const [confirm, confirmDialog] = useConfirmDialog();
  const [value, setValue] = useState(String(rollout.current_version));
  const current = rollout.current_version;
  const pin = rollout.default_pin;
  const version = validHooksRolloutVersion(value, current);

  const mutation = useMutation({
    mutationFn: (pin: number) => setHooksRolloutDefault(pin),
    onSuccess: (updated) => {
      queryClient.setQueryData(hooksRolloutQuery.queryKey, updated);
      invalidateHooksRollout(queryClient);
    },
  });

  const submit = async () => {
    if (version === undefined) return;
    const confirmed = await confirm({
      title: `Set the default hooks pin to ${version}?`,
      description:
        version >= current
          ? `Every organization without an override will receive hooks version ${current} on the next rollout sweep.`
          : `Organizations without an override will be held at hooks version ${version} or below; version ${current} will not reach them.`,
      confirmLabel: "Set default pin",
    });
    if (confirmed) mutation.mutate(version);
  };

  return (
    <section className="border-border overflow-hidden rounded-md border">
      <div className="border-border bg-muted/50 space-y-1 border-b p-4">
        <h2 className="text-base font-semibold">Default pin</h2>
        <p className="text-muted-foreground text-sm">
          Applies to every organization without an override.
        </p>
      </div>
      <div className="space-y-4 p-4">
        <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 text-sm">
          <dt className="text-muted-foreground">Current hooks version</dt>
          <dd className="font-medium">{current}</dd>
          <dt className="text-muted-foreground">Default pin</dt>
          <dd className="font-medium">
            {pin ? (
              <span className="flex items-center gap-2">
                {pin.version}
                <PinStatusBadge version={pin.version} current={current} />
                <span className="text-muted-foreground font-normal">
                  set by {pin.set_by} on {fmtDateShort(pin.set_at)}
                </span>
              </span>
            ) : (
              "Not set"
            )}
          </dd>
          <dt className="text-muted-foreground">Canary organizations</dt>
          <dd>
            {rollout.canary_organization_slugs.join(", ") || "None"}
            <span className="text-muted-foreground">
              {" "}
              (always receive the current version)
            </span>
          </dd>
        </dl>
        <p className="text-sm" role="status">
          {defaultPinSummary(pin?.version, current)}
        </p>
        <div className="flex items-center gap-2">
          <Input
            className="w-28"
            type="number"
            min={1}
            max={current}
            step={1}
            aria-label="Default hooks version pin"
            value={value}
            disabled={mutation.isPending}
            onChange={(event) => setValue(event.target.value)}
          />
          <Button
            size="sm"
            disabled={mutation.isPending || version === undefined}
            onClick={() => void submit()}
          >
            {mutation.isPending ? "Saving…" : "Set default pin"}
          </Button>
        </div>
        {version === undefined && (
          <p className="text-destructive text-sm">
            The pin must be a whole number from 1 to {current}.
          </p>
        )}
        {mutation.isError && (
          <p className="text-destructive text-sm">
            {errorMessage(mutation.error)}
          </p>
        )}
      </div>
      {confirmDialog}
    </section>
  );
}

function defaultPinSummary(
  pinned: number | undefined,
  current: number,
): string {
  if (pinned === undefined) {
    return "No default pin is set, so organizations without an override still follow the legacy hooks-rollout PostHog flag. Set a default pin to take over from it.";
  }
  if (pinned >= current) {
    return `Every organization without an override is cleared for hooks version ${current}.`;
  }
  return `Hooks version ${current} has not been rolled out: organizations without an override stay at version ${pinned} or below.`;
}

export function PinStatusBadge({
  version,
  current,
}: {
  version: number;
  current: number;
}): JSX.Element {
  return version >= current ? (
    <Badge variant="outline">Current</Badge>
  ) : (
    <Badge variant="secondary">{current - version} behind</Badge>
  );
}

function Overrides({ rollout }: { rollout: AdminHooksRollout }): JSX.Element {
  return (
    <section className="border-border overflow-hidden rounded-md border">
      <div className="border-border bg-muted/50 space-y-1 border-b p-4">
        <h2 className="text-base font-semibold">Organization overrides</h2>
        <p className="text-muted-foreground text-sm">
          Organizations pinned apart from the default. Set or clear an override
          from the organization&apos;s Features tab.
        </p>
      </div>
      {rollout.overrides.length === 0 ? (
        <p className="text-muted-foreground p-4 text-sm">No overrides.</p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Organization</TableHead>
              <TableHead>Pinned version</TableHead>
              <TableHead>Set by</TableHead>
              <TableHead>Set on</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rollout.overrides.map((override) => (
              <TableRow key={override.organization_id}>
                <TableCell>
                  <Link
                    to="/organizations/$idOrSlug/features"
                    params={{ idOrSlug: override.organization_id }}
                    className="font-medium hover:underline"
                  >
                    {override.organization_name}
                  </Link>
                  <span className="text-muted-foreground ml-2 text-xs">
                    {override.organization_slug}
                  </span>
                </TableCell>
                <TableCell>
                  <span className="flex items-center gap-2">
                    {override.pin.version}
                    <PinStatusBadge
                      version={override.pin.version}
                      current={rollout.current_version}
                    />
                  </span>
                </TableCell>
                <TableCell>{override.pin.set_by}</TableCell>
                <TableCell>{fmtDateShort(override.pin.set_at)}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </section>
  );
}

function changeDescription(change: AdminHooksRolloutChange): string {
  return change.version === undefined
    ? "Cleared override"
    : `Pinned to ${change.version}`;
}

function RecentChanges({
  changes,
}: {
  changes: AdminHooksRolloutChange[];
}): JSX.Element {
  return (
    <section className="border-border overflow-hidden rounded-md border">
      <div className="border-border bg-muted/50 border-b p-4">
        <h2 className="text-base font-semibold">Recent changes</h2>
      </div>
      {changes.length === 0 ? (
        <p className="text-muted-foreground p-4 text-sm">
          No pin has been set yet.
        </p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Date</TableHead>
              <TableHead>Scope</TableHead>
              <TableHead>Change</TableHead>
              <TableHead>By</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {changes.map((change, index) => (
              <TableRow key={`${change.set_at}-${index}`}>
                <TableCell>{fmtDateShort(change.set_at)}</TableCell>
                <TableCell>
                  {change.organization_id ? (
                    <Link
                      to="/organizations/$idOrSlug/features"
                      params={{ idOrSlug: change.organization_id }}
                      className="hover:underline"
                    >
                      {change.organization_slug ?? change.organization_id}
                    </Link>
                  ) : (
                    "Default pin"
                  )}
                </TableCell>
                <TableCell>{changeDescription(change)}</TableCell>
                <TableCell>{change.set_by}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </section>
  );
}
