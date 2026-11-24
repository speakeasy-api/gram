import { useState, type JSX } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2Icon } from "lucide-react";
import { toast } from "sonner";

import type { AdminTailscaleOAuthMigrationOrganization } from "@gram/admin-client/models/components/admintailscaleoauthmigrationorganization";
import type { AdminTailscaleOAuthMigrationOrganizations } from "@gram/admin-client/models/components/admintailscaleoauthmigrationorganizations";
import type { Step } from "@gram/admin-client/models/components/runtailscaleoauthmigrationsteprequestbody";

import { useConfirmDialog } from "@/components/ConfirmDialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { errorMessage } from "@/lib/gramAdminApi";
import {
  runTailscaleOAuthMigrationStep,
  tailscaleOAuthMigrationQuery,
} from "@/lib/gramAdminClient";

type StepCopy = {
  label: string;
  title: (org: string) => string;
  description: string;
  variant?: "outline" | "destructive";
};

const STEPS: Record<Step, StepCopy> = {
  enroll: {
    label: "Enroll",
    title: (org) => `Enroll ${org}?`,
    description:
      "Provisions the organization's key and starts dual_write: new credentials are sealed with both keys and reads prefer the organization key. A failing organization key then fails credential reads and writes instead of falling back.",
  },
  backfill: {
    label: "Backfill",
    title: (org) => `Backfill ${org}?`,
    description:
      "Seals every saved credential that has only a platform-key copy with the organization key, then verifies that both copies of every credential decrypt to the same value. Safe to run again.",
    variant: "outline",
  },
  finalize: {
    label: "Finalize",
    title: (org) => `Finalize ${org}?`,
    description:
      "Moves to org_kms: from now on only the organization-key copy is written and read. Refused until nothing is waiting, a clean verification ran after the last backfill, and the minimum time in dual_write has passed. There is no rollback after this.",
  },
  clear_platform_copies: {
    label: "Clear platform copies",
    title: (org) => `Destroy ${org}'s platform-key copies?`,
    description:
      "Deletes the platform-key copy of every saved credential. Credentials recorded as unreadable are destroyed with them. Safe to run again until it finishes.",
    variant: "destructive",
  },
  rollback: {
    label: "Roll back",
    title: (org) => `Roll ${org} back to the platform key?`,
    description:
      "Returns to platform_key. Both copies still exist in dual_write, so nothing is lost; the organization-key copies stay until the next enrollment.",
    variant: "outline",
  },
};

function stepsFor(org: AdminTailscaleOAuthMigrationOrganization): Step[] {
  switch (org.state) {
    case "platform_key":
      return ["enroll"];
    case "dual_write":
      return ["backfill", "finalize", "rollback"];
    case "org_kms":
      return org.platformCopiesClearedAt ? [] : ["clear_platform_copies"];
    default:
      return [];
  }
}

function formatTime(value: Date | undefined): string {
  return value ? value.toLocaleString() : "—";
}

function StateBadge({
  org,
}: {
  org: AdminTailscaleOAuthMigrationOrganization;
}): JSX.Element {
  if (org.state === "org_kms" && org.platformCopiesClearedAt) {
    return <Badge>Done</Badge>;
  }
  return <Badge variant="secondary">{org.state}</Badge>;
}

export function TailscaleOAuthMigrationPage(): JSX.Element {
  const client = useQueryClient();
  const [confirm, confirmDialog] = useConfirmDialog();
  const migration = useQuery(tailscaleOAuthMigrationQuery());
  // The step each busy organization is running, by id.
  const [running, setRunning] = useState<Map<string, Step>>(new Map());

  const organizations = migration.data?.organizations ?? [];
  const minimumHours = (migration.data?.minimumDualWriteSeconds ?? 0) / 3600;

  async function run(
    org: AdminTailscaleOAuthMigrationOrganization,
    step: Step,
  ) {
    const copy = STEPS[step];
    const ok = await confirm({
      title: copy.title(org.name),
      description: copy.description,
      confirmLabel: copy.label,
      destructive: copy.variant === "destructive",
    });
    if (!ok) return;
    setRunning((current) => new Map(current).set(org.organizationId, step));
    try {
      const updated = await runTailscaleOAuthMigrationStep(
        org.organizationId,
        step,
      );
      client.setQueryData<AdminTailscaleOAuthMigrationOrganizations>(
        tailscaleOAuthMigrationQuery().queryKey,
        (current) =>
          current && {
            ...current,
            organizations: current.organizations.map((o) =>
              o.organizationId === updated.organizationId ? updated : o,
            ),
          },
      );
      if (step === "backfill" && updated.lastBackfillError) {
        toast.warning(`${org.name}: ${updated.lastBackfillError}`);
      } else {
        toast.success(`${org.name}: ${copy.label.toLowerCase()} done`);
      }
    } catch (e) {
      toast.error(`${org.name}: ${errorMessage(e)}`);
      // A failed step may still have changed state on the server.
      void client.invalidateQueries({
        queryKey: tailscaleOAuthMigrationQuery().queryKey,
      });
    } finally {
      setRunning((current) => {
        const next = new Map(current);
        next.delete(org.organizationId);
        return next;
      });
    }
  }

  return (
    <div className="space-y-6 p-6">
      <div>
        <h1 className="text-2xl font-semibold">Tailscale OAuth migration</h1>
        <p className="text-muted-foreground max-w-3xl">
          Moves each organization's saved Tailscale OAuth client from the
          platform key to its own organization key. Per organization: enroll,
          backfill, wait at least {minimumHours} hour
          {minimumHours === 1 ? "" : "s"} in dual_write, backfill again so a
          clean verification follows, finalize, then clear the platform-key
          copies. Temporary: this page goes away once every organization is
          done.
        </p>
      </div>

      {migration.isLoading ? (
        <div className="text-muted-foreground flex items-center gap-2">
          <Loader2Icon className="size-4 animate-spin" /> Loading
        </div>
      ) : migration.error ? (
        <p className="text-destructive">{errorMessage(migration.error)}</p>
      ) : organizations.length === 0 ? (
        <p className="text-muted-foreground">
          No organization has saved Tailscale OAuth credentials.
        </p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Organization</TableHead>
              <TableHead>State</TableHead>
              <TableHead className="text-right">Waiting</TableHead>
              <TableHead className="text-right">Unreadable</TableHead>
              <TableHead className="text-right">Platform copies</TableHead>
              <TableHead>In state since</TableHead>
              <TableHead>Last backfill</TableHead>
              <TableHead>Last clean verification</TableHead>
              <TableHead />
            </TableRow>
          </TableHeader>
          <TableBody>
            {organizations.map((org) => {
              const busy = running.get(org.organizationId);
              return (
                <TableRow key={org.organizationId}>
                  <TableCell>
                    <div className="font-medium">{org.name}</div>
                    <div className="text-muted-foreground text-xs">
                      {org.slug}
                    </div>
                  </TableCell>
                  <TableCell>
                    <StateBadge org={org} />
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {org.waiting}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {org.unreadable}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {org.platformCopies}
                  </TableCell>
                  <TableCell>{formatTime(org.stateChangedAt)}</TableCell>
                  <TableCell>
                    <div>{formatTime(org.lastBackfillCompletedAt)}</div>
                    {org.lastBackfillError && (
                      <div className="text-destructive text-xs">
                        {org.lastBackfillError}
                      </div>
                    )}
                  </TableCell>
                  <TableCell>{formatTime(org.lastVerifiedAt)}</TableCell>
                  <TableCell>
                    <div className="flex justify-end gap-2">
                      {stepsFor(org).map((step) => (
                        <Button
                          key={step}
                          size="sm"
                          variant={STEPS[step].variant}
                          disabled={busy !== undefined}
                          onClick={() => void run(org, step)}
                        >
                          {busy === step && (
                            <Loader2Icon className="size-4 animate-spin" />
                          )}
                          {STEPS[step].label}
                        </Button>
                      ))}
                    </div>
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      )}
      {confirmDialog}
    </div>
  );
}
