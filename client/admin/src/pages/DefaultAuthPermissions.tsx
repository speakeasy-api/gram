import { useState, type JSX } from "react";
import {
  useQueries,
  useQuery,
  useQueryClient,
  type UseQueryResult,
} from "@tanstack/react-query";
import { CheckCircle2Icon, CircleDashedIcon, Loader2Icon } from "lucide-react";
import { toast } from "sonner";

import type { AdminDefaultAuthPermissionStatus } from "@gram/admin-client/models/components/admindefaultauthpermissionstatus";

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
import { organizationsListQuery } from "@/lib/adminQueries";
import { errorMessage } from "@/lib/gramAdminApi";
import {
  defaultAuthPermissionStatusQuery,
  runDefaultAuthPermissions,
} from "@/lib/gramAdminClient";

const PAGE_SIZE = 50;

// Each status is a dry run, so only a few load at once.
const STATUS_CONCURRENCY = 4;

const KIND_LABELS: Record<string, string> = {
  personal_default: "personal connections",
  shared_client: "shared clients",
  api_key: "API key policies",
};

type Organization = { id: string; name: string; slug: string };

export function DefaultAuthPermissionsPage(): JSX.Element {
  const client = useQueryClient();
  const [confirm, confirmDialog] = useConfirmDialog();
  const [search, setSearch] = useState("");
  const [page, setPage] = useState(1);
  const [running, setRunning] = useState<Set<string>>(new Set());
  const [bulk, setBulk] = useState<{ done: number; total: number } | null>(
    null,
  );

  const organizations = useQuery(
    organizationsListQuery({
      sort: "name",
      direction: "asc",
      page,
      limit: PAGE_SIZE,
      q: search.trim() || undefined,
    }),
  );
  const rows: Organization[] = organizations.data?.organizations ?? [];
  const total = organizations.data?.total ?? 0;
  const pages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  const settled = rows.filter((org) => {
    const state = client.getQueryState(
      defaultAuthPermissionStatusQuery(org.id).queryKey,
    );
    return (
      state?.fetchStatus === "idle" &&
      (state.status === "success" || state.status === "error")
    );
  }).length;
  const statuses = useQueries({
    queries: rows.map((org, index) => ({
      ...defaultAuthPermissionStatusQuery(org.id),
      enabled: index < settled + STATUS_CONCURRENCY,
      // A cached status counts as settled, so refetching it on mount or focus
      // would bypass the window above.
      staleTime: Infinity,
      refetchOnWindowFocus: false,
    })),
  });
  const statusByID = new Map(rows.map((org, i) => [org.id, statuses[i]]));
  const pending = rows.filter(
    (org) => (statusByID.get(org.id)?.data?.pendingGrants ?? 0) > 0,
  );

  async function grant(org: Organization) {
    setRunning((current) => new Set(current).add(org.id));
    try {
      const run = await runDefaultAuthPermissions(org.id);
      client.setQueryData(
        defaultAuthPermissionStatusQuery(org.id).queryKey,
        run.status,
      );
      return run;
    } finally {
      setRunning((current) => {
        const next = new Set(current);
        next.delete(org.id);
        return next;
      });
    }
  }

  async function grantOne(org: Organization) {
    const ok = await confirm({
      title: `Grant default permissions in ${org.name}?`,
      description:
        "Adds the organization's missing default auth permissions: everyone's and each agent's use of their own personal connections, shared clients for whoever can connect to a server using them, and policies for legacy API keys. It only adds permissions, and restores any default since removed.",
      confirmLabel: "Grant",
    });
    if (!ok) return;
    try {
      const run = await grant(org);
      if (run.status.complete) {
        toast.success(`${org.name}: ${run.grantsWritten} granted`);
      } else {
        toast.warning(`${org.name} still needs attention`);
      }
    } catch (e) {
      toast.error(`${org.name}: ${errorMessage(e)}`);
    }
  }

  async function grantPage() {
    if (pending.length === 0) return;
    const ok = await confirm({
      title: `Grant default permissions in ${pending.length} ${pending.length === 1 ? "organization" : "organizations"}?`,
      description:
        "Runs for every organization on this page with permissions to grant, one at a time. It only adds permissions, and restores any default since removed. Keep this tab open until it finishes.",
      confirmLabel: "Grant all",
    });
    if (!ok) return;
    setBulk({ done: 0, total: pending.length });
    let failed = 0;
    let incomplete = 0;
    for (const org of pending) {
      try {
        const run = await grant(org);
        if (!run.status.complete) incomplete++;
      } catch (e) {
        failed++;
        toast.error(`${org.name}: ${errorMessage(e)}`);
      }
      setBulk((current) =>
        current ? { ...current, done: current.done + 1 } : current,
      );
    }
    setBulk(null);
    if (incomplete > 0) {
      toast.warning(
        `${incomplete} ${incomplete === 1 ? "organization still needs" : "organizations still need"} attention`,
      );
    } else if (failed === 0) {
      toast.success(
        `Granted in ${pending.length} ${pending.length === 1 ? "organization" : "organizations"}`,
      );
    }
  }

  return (
    <div className="space-y-6 p-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold">Default auth permissions</h1>
          <p className="text-muted-foreground max-w-3xl">
            Grants the permissions an organization needs before the auth client
            check is enforced, so nobody loses access they have today. Counts
            only cover auth clients already recorded; run the upstream OAuth
            migration first where it applies.
          </p>
        </div>
        <Button
          onClick={() => void grantPage()}
          disabled={
            bulk !== null || settled < rows.length || pending.length === 0
          }
        >
          {bulk
            ? `Granting ${bulk.done} of ${bulk.total}`
            : "Grant all on this page"}
        </Button>
      </div>

      <div className="flex items-center justify-between gap-4">
        <Input
          aria-label="Search organizations"
          placeholder="Search by name, slug or ID"
          value={search}
          onChange={(e) => {
            setSearch(e.target.value);
            setPage(1);
          }}
          className="max-w-sm"
        />
        <div className="flex items-center gap-2 text-sm">
          <span className="text-muted-foreground">
            Page {page} of {pages}
          </span>
          <Button
            size="sm"
            variant="outline"
            onClick={() => setPage((p) => p - 1)}
            disabled={page <= 1 || bulk !== null}
          >
            Previous
          </Button>
          <Button
            size="sm"
            variant="outline"
            onClick={() => setPage((p) => p + 1)}
            disabled={page >= pages || bulk !== null}
          >
            Next
          </Button>
        </div>
      </div>

      {organizations.isError ? (
        <p className="text-destructive">{errorMessage(organizations.error)}</p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Organization</TableHead>
              <TableHead>Status</TableHead>
              <TableHead className="w-0" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {organizations.isPending ? (
              <TableRow>
                <TableCell colSpan={3} className="text-muted-foreground">
                  Loading organizations…
                </TableCell>
              </TableRow>
            ) : rows.length === 0 ? (
              <TableRow>
                <TableCell colSpan={3} className="text-muted-foreground">
                  No organization matches.
                </TableCell>
              </TableRow>
            ) : (
              rows.map((org) => {
                const status = statusByID.get(org.id);
                const busy = running.has(org.id);
                return (
                  <TableRow key={org.id}>
                    <TableCell>
                      <div className="font-medium">{org.name}</div>
                      <div className="text-muted-foreground font-mono text-xs">
                        {org.slug}
                      </div>
                    </TableCell>
                    <TableCell>
                      <PermissionStatus status={status} busy={busy} />
                    </TableCell>
                    <TableCell>
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => void grantOne(org)}
                        disabled={
                          busy ||
                          bulk !== null ||
                          !(status?.data && status.data.pendingGrants > 0)
                        }
                      >
                        Grant
                      </Button>
                    </TableCell>
                  </TableRow>
                );
              })
            )}
          </TableBody>
        </Table>
      )}
      {confirmDialog}
    </div>
  );
}

function PermissionStatus({
  status,
  busy,
}: {
  status: UseQueryResult<AdminDefaultAuthPermissionStatus> | undefined;
  busy: boolean;
}): JSX.Element {
  if (busy) {
    return (
      <span className="text-muted-foreground inline-flex items-center gap-1">
        <Loader2Icon className="size-4 animate-spin" /> Granting…
      </span>
    );
  }
  if (!status || status.isPending) {
    return <span className="text-muted-foreground">Checking…</span>;
  }
  if (status.isError) {
    return (
      <span className="text-destructive">{errorMessage(status.error)}</span>
    );
  }
  const s = status.data;
  if (s.inProgress) {
    return <span className="text-muted-foreground">A run is in progress</span>;
  }
  if (s.error) {
    return <span className="text-destructive">{s.error}</span>;
  }
  if (s.complete) {
    return (
      <Badge variant="outline" className="gap-1">
        <CheckCircle2Icon className="size-3" /> Granted
      </Badge>
    );
  }
  const parts = s.kinds
    .filter((k) => k.pending > 0)
    .map((k) => `${k.pending} ${KIND_LABELS[k.kind] ?? k.kind}`);
  return (
    <span className="inline-flex items-center gap-1">
      <CircleDashedIcon className="text-muted-foreground size-4" />
      {s.pendingGrants} to grant: {parts.join(", ")}
    </span>
  );
}
