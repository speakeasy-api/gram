import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { Dialog } from "@/components/ui/dialog";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { cn } from "@/lib/utils";
import type { Proposal } from "@gram/client/models/components";
import {
  invalidateAllInsightsListProposals,
  useInsightsApplyProposalMutation,
  useInsightsDismissProposalMutation,
  useInsightsListProposals,
  useInsightsRollbackProposalMutation,
} from "@gram/client/react-query";
import { useQueryClient } from "@tanstack/react-query";
import { ChevronDown, CheckCircle2, XCircle, Undo2 } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";

/**
 * ProposalsPanel renders pending and historical AI proposals inline within
 * the InsightsSidebar. Users review a diff per card and Apply / Dismiss
 * pending proposals, or Roll back applied ones.
 *
 * Drift handling: when rollback returns a 409 conflict (live state has drifted
 * from applied_value), we show a <DriftModal> with a force-overwrite confirm.
 */
export function ProposalsPanel() {
  const queryClient = useQueryClient();

  // throwOnError: false — symmetric with listMemories. A 401/5xx must
  // degrade to "no proposals" so the dashboard's global error boundary
  // doesn't replace the whole page.
  const { data, isLoading } = useInsightsListProposals(undefined, undefined, {
    throwOnError: false,
    // Poll every 10s — the agent's MCP mutations create proposals that the
    // dashboard would otherwise only see on the next manual refresh. Cheap
    // network traffic for a much better "I just asked the agent — where's
    // my proposal?" experience.
    refetchInterval: 10_000,
  });
  const proposals = data?.proposals ?? [];

  const pending = useMemo(
    () => proposals.filter((p) => p.status === "pending"),
    [proposals],
  );
  const history = useMemo(
    () =>
      proposals.filter(
        (p) =>
          p.status === "applied" ||
          p.status === "dismissed" ||
          p.status === "rolled_back" ||
          p.status === "superseded",
      ),
    [proposals],
  );

  // The collapsible's initial value is computed before data lands. We
  // auto-open ONCE the first time we see pending proposals; after that the
  // user's manual collapse/expand sticks. The ref tracks whether we've
  // already auto-opened so reopening the sidebar later doesn't override
  // their choice.
  const [isOpen, setIsOpen] = useState(false);
  const autoOpenedRef = useRef(false);
  useEffect(() => {
    if (!autoOpenedRef.current && pending.length > 0) {
      autoOpenedRef.current = true;
      setIsOpen(true);
    }
  }, [pending.length]);
  const [driftProposal, setDriftProposal] = useState<Proposal | null>(null);

  // Shared error handler for proposal mutations. Always invalidates the
  // proposals list so a stale-pending view (e.g. one user's tab showing a
  // proposal another user already applied) refreshes immediately and the
  // dead button disappears. Friendly-translates the lifecycle 409 messages
  // ("not pending", "not applied", "superseded") into a single "no longer
  // available, refreshing" toast so the user understands why their click
  // was rejected.
  const handleApiError = (verb: string, err: { message?: string }) => {
    void invalidateAllInsightsListProposals(queryClient);
    const m = err.message ?? "";
    if (/not pending|not applied|already|superseded/i.test(m)) {
      toast.warning(
        `This proposal is no longer in a state that can be ${verb}. Refreshing the list.`,
      );
    } else {
      toast.error(
        `${verb.charAt(0).toUpperCase() + verb.slice(1)} failed: ${m}`,
      );
    }
  };

  const applyMutation = useInsightsApplyProposalMutation({
    onSuccess: () => {
      void invalidateAllInsightsListProposals(queryClient);
      toast.success("Proposal applied");
    },
    onError: (err) => handleApiError("applied", err),
  });

  const dismissMutation = useInsightsDismissProposalMutation({
    onSuccess: () => {
      void invalidateAllInsightsListProposals(queryClient);
      toast.success("Proposal dismissed");
    },
    onError: (err) => handleApiError("dismissed", err),
  });

  // Drift detection for rollback: only the specific drift-conflict message
  // ("resource has drifted from applied value") should open the DriftModal.
  // Generic lifecycle 409s ("not applied", "superseded") fall through to
  // handleApiError so the user gets the refresh-and-explain UX, not a
  // mysterious empty modal.
  const isDriftError = (msg: string | undefined) =>
    !!msg && /drifted|drift/i.test(msg);

  const rollbackMutation = useInsightsRollbackProposalMutation({
    onError: (err) => {
      if (isDriftError(err.message)) return; // handled by DriftModal flow
      handleApiError("rolled back", err);
    },
  });

  const handleApply = (proposal: Proposal) => {
    applyMutation.mutate({
      request: {
        applyProposalForm: { proposalId: proposal.id },
      },
    });
  };

  const handleDismiss = (proposal: Proposal) => {
    dismissMutation.mutate({
      request: {
        dismissProposalForm: { proposalId: proposal.id },
      },
    });
  };

  const handleRollback = (proposal: Proposal, force: boolean) => {
    rollbackMutation.mutate(
      {
        request: {
          applyProposalForm: { proposalId: proposal.id, force },
        },
      },
      {
        onSuccess: () => {
          void invalidateAllInsightsListProposals(queryClient);
          setDriftProposal(null);
          toast.success("Proposal rolled back");
        },
        onError: (err) => {
          if (!force && isDriftError(err.message)) {
            setDriftProposal(proposal);
            return;
          }
          // Force-rollback or non-drift error: refresh and explain. Without
          // this the global mutation onError above would have run, which
          // already invalidates — but we still need to exit cleanly here.
          handleApiError("rolled back", err);
        },
      },
    );
  };

  if (isLoading) {
    return null;
  }

  // Empty state — keep the affordance visible so users on a fresh project
  // discover the feature exists. Collapses to a single muted line.
  if (proposals.length === 0) {
    return (
      <div className="border-border bg-background mx-4 mt-3 rounded-md border px-3 py-2 text-sm">
        <div className="text-muted-foreground flex items-center gap-2">
          <span className="font-medium">AI Proposals</span>
          <span className="text-xs">
            · None yet — ask the assistant to investigate something.
          </span>
        </div>
      </div>
    );
  }

  return (
    <div className="border-border bg-background mx-4 mt-3 rounded-md border">
      <Collapsible open={isOpen} onOpenChange={setIsOpen}>
        <CollapsibleTrigger asChild>
          <button
            type="button"
            className="hover:bg-muted/50 flex w-full items-center justify-between px-3 py-2 text-left text-sm"
          >
            <div className="flex items-center gap-2">
              <span className="font-medium">AI Proposals</span>
              {pending.length > 0 && (
                <Badge variant="default" size="sm">
                  {pending.length} pending
                </Badge>
              )}
            </div>
            <ChevronDown
              className={cn(
                "size-4 transition-transform",
                isOpen && "rotate-180",
              )}
            />
          </button>
        </CollapsibleTrigger>
        <CollapsibleContent className="border-border border-t">
          <Tabs defaultValue="pending" className="p-3">
            <TabsList>
              <TabsTrigger value="pending">
                Pending
                {pending.length > 0 && (
                  <span className="text-muted-foreground ml-1">
                    ({pending.length})
                  </span>
                )}
              </TabsTrigger>
              <TabsTrigger value="history">
                History
                {history.length > 0 && (
                  <span className="text-muted-foreground ml-1">
                    ({history.length})
                  </span>
                )}
              </TabsTrigger>
            </TabsList>
            <TabsContent value="pending" className="space-y-3 pt-3">
              {pending.length === 0 && (
                <p className="text-muted-foreground px-2 py-4 text-xs">
                  No pending proposals.
                </p>
              )}
              {pending.map((p) => (
                <ProposalCard
                  key={p.id}
                  proposal={p}
                  actions={
                    <>
                      <Button
                        size="sm"
                        variant="default"
                        onClick={() => handleApply(p)}
                        disabled={applyMutation.isPending}
                      >
                        <CheckCircle2 className="size-3.5" />
                        Apply
                      </Button>
                      <Button
                        size="sm"
                        variant="secondary"
                        onClick={() => handleDismiss(p)}
                        disabled={dismissMutation.isPending}
                      >
                        <XCircle className="size-3.5" />
                        Dismiss
                      </Button>
                    </>
                  }
                />
              ))}
            </TabsContent>
            <TabsContent value="history" className="space-y-3 pt-3">
              {history.length === 0 && (
                <p className="text-muted-foreground px-2 py-4 text-xs">
                  No historical proposals.
                </p>
              )}
              {history.map((p) => (
                <ProposalCard
                  key={p.id}
                  proposal={p}
                  actions={
                    p.status === "applied" ? (
                      <Button
                        size="sm"
                        variant="secondary"
                        onClick={() => handleRollback(p, false)}
                        disabled={rollbackMutation.isPending}
                      >
                        <Undo2 className="size-3.5" />
                        Roll back
                      </Button>
                    ) : null
                  }
                />
              ))}
            </TabsContent>
          </Tabs>
        </CollapsibleContent>
      </Collapsible>

      <DriftModal
        proposal={driftProposal}
        onClose={() => setDriftProposal(null)}
        onForce={(p) => handleRollback(p, true)}
        isPending={rollbackMutation.isPending}
      />
    </div>
  );
}

interface ProposalCardProps {
  proposal: Proposal;
  actions: React.ReactNode;
}

function ProposalCard({ proposal, actions }: ProposalCardProps) {
  const kindLabel =
    proposal.kind === "tool_variation" ? "Tool variation" : "Toolset change";
  const statusVariant = statusBadgeVariant(proposal.status);

  return (
    <div className="border-border bg-card rounded-md border p-3">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <Badge variant="outline" size="sm">
              {kindLabel}
            </Badge>
            <Badge variant={statusVariant} size="sm">
              {proposal.status}
            </Badge>
            <span className="text-foreground truncate font-mono text-xs">
              {proposal.targetRef}
            </span>
          </div>
          {proposal.reasoning && (
            <p className="text-muted-foreground mt-2 text-xs">
              {proposal.reasoning}
            </p>
          )}
        </div>
      </div>

      <JsonDiff
        before={proposal.currentValue}
        after={proposal.appliedValue ?? proposal.proposedValue}
      />

      {actions && <div className="mt-3 flex items-center gap-2">{actions}</div>}
    </div>
  );
}

function statusBadgeVariant(
  status: string,
): "default" | "secondary" | "outline" | "destructive" | "warning" {
  switch (status) {
    case "pending":
      return "default";
    case "applied":
      return "secondary";
    case "dismissed":
      return "outline";
    case "rolled_back":
      return "outline";
    case "superseded":
      return "warning";
    default:
      return "outline";
  }
}

/**
 * Minimal inline JSON diff. Renders before vs after side-by-side with changed
 * keys highlighted. The spec calls out reusing the audit-log diff component
 * but that one is driven by `AuditLog` (with typed before/after snapshots),
 * whereas proposals carry JSON-stringified blobs. A shared diff primitive
 * is a future refactor; for v1 we keep this minimal.
 */
function JsonDiff({ before, after }: { before: string; after: string }) {
  const parsed = useMemo(() => {
    try {
      return {
        before: JSON.parse(before) as unknown,
        after: JSON.parse(after) as unknown,
      };
    } catch {
      return null;
    }
  }, [before, after]);

  if (
    !parsed ||
    typeof parsed.before !== "object" ||
    parsed.before === null ||
    typeof parsed.after !== "object" ||
    parsed.after === null
  ) {
    // Fall back to raw-text display when either side isn't a JSON object.
    return (
      <div className="mt-3 space-y-1 text-xs">
        <div className="rounded bg-red-50 px-2 py-1 font-mono break-all text-red-700 dark:bg-red-950 dark:text-red-400">
          {before || "(none)"}
        </div>
        <div className="rounded bg-emerald-50 px-2 py-1 font-mono break-all text-emerald-700 dark:bg-emerald-950 dark:text-emerald-400">
          {after || "(none)"}
        </div>
      </div>
    );
  }

  const beforeObj = parsed.before as Record<string, unknown>;
  const afterObj = parsed.after as Record<string, unknown>;
  const keys = Array.from(
    new Set([...Object.keys(beforeObj), ...Object.keys(afterObj)]),
  ).sort();

  const changed = keys.filter(
    (k) => JSON.stringify(beforeObj[k]) !== JSON.stringify(afterObj[k]),
  );

  if (changed.length === 0) {
    return (
      <p className="text-muted-foreground mt-3 text-xs italic">
        No changes detected.
      </p>
    );
  }

  return (
    <div className="divide-border border-border/50 mt-3 divide-y rounded border">
      {changed.map((key) => (
        <div key={key} className="flex items-start gap-3 px-2 py-1.5">
          <span className="text-muted-foreground w-32 shrink-0 pt-0.5 font-mono text-xs font-medium">
            {key}
          </span>
          <div className="flex min-w-0 flex-1 flex-wrap items-start gap-2">
            <span className="max-w-full rounded bg-red-50 px-1.5 py-0.5 font-mono text-xs break-all text-red-700 line-through dark:bg-red-950 dark:text-red-400">
              {formatValue(beforeObj[key])}
            </span>
            <span className="text-muted-foreground pt-0.5 text-xs">→</span>
            <span className="max-w-full rounded bg-emerald-50 px-1.5 py-0.5 font-mono text-xs break-all text-emerald-700 dark:bg-emerald-950 dark:text-emerald-400">
              {formatValue(afterObj[key])}
            </span>
          </div>
        </div>
      ))}
    </div>
  );
}

function formatValue(v: unknown): string {
  if (v === undefined) return "(unset)";
  if (v === null) return "null";
  if (typeof v === "string") return v;
  return JSON.stringify(v);
}

interface DriftModalProps {
  proposal: Proposal | null;
  onClose: () => void;
  onForce: (proposal: Proposal) => void;
  isPending: boolean;
}

function DriftModal({
  proposal,
  onClose,
  onForce,
  isPending,
}: DriftModalProps) {
  return (
    <Dialog open={proposal !== null} onOpenChange={(o) => !o && onClose()}>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>This has changed since you applied</Dialog.Title>
          <Dialog.Description>
            The underlying resource has drifted from the value that was written
            when this proposal was applied. Rolling back now will overwrite the
            newer state with the pre-apply snapshot. Continue?
          </Dialog.Description>
        </Dialog.Header>
        {proposal && (
          <div className="mt-3">
            <JsonDiff
              before={proposal.appliedValue ?? proposal.proposedValue}
              after={proposal.currentValue}
            />
          </div>
        )}
        <Dialog.Footer>
          <Button variant="secondary" onClick={onClose} disabled={isPending}>
            Cancel
          </Button>
          <Button
            variant="destructive"
            onClick={() => proposal && onForce(proposal)}
            disabled={isPending}
          >
            Overwrite anyway
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
