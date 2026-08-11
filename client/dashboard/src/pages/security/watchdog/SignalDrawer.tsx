import { Avatar, AvatarFallback } from "@/components/ui/Avatar";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { Separator } from "@/components/ui/Separator";
import { Text } from "@/components/ui/Text";
import { useSdkClient } from "@/contexts/Sdk";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import type { RiskSignal } from "@gram/client/models/components/risksignal.js";
import { useRiskListResults } from "@gram/client/react-query/riskListResults.js";
import { cn } from "@/lib/utils";
import { formatDistanceToNow } from "date-fns";
import { Loader2 } from "lucide-react";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import { ExclusionEditor, type ExclusionSheetState } from "../exclusion-sheet";
import { MaskedMatch, RevealAllProvider, RevealAllToggle } from "../risk-ui";
import { getRuleTitleFallback, scoreToRating } from "../risk-utils";
import { useDismissFinding } from "../useDismissFinding";
import {
  collectFindingsForRules,
  SIGNAL_DISMISS_CAP,
} from "./collect-findings";
import { SCORE_TEXT_COLOR } from "./signals-helpers";
import { SignalTrend } from "./SignalsList";

const EVIDENCE_ROWS = 5;

function StatCell({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}): JSX.Element {
  return (
    <div className="border-border rounded-md border p-3">
      <Text small muted>
        {label}
      </Text>
      <div className="pt-1 text-lg font-semibold tabular-nums">{children}</div>
    </div>
  );
}

/**
 * Shows only the first item with the rest behind a "Show N more" control, so
 * the drawer stays scannable for signals with many grouped events. Collapses
 * again via "Show less". Keyed by the host on the signal so switching signals
 * resets to collapsed.
 */
function ExpandableList<T>({
  items,
  itemLabel,
  renderItem,
}: {
  items: T[];
  /** Plural noun for the expander label, e.g. "users" or "findings". */
  itemLabel: string;
  renderItem: (item: T) => React.ReactNode;
}): JSX.Element {
  const [expanded, setExpanded] = useState(false);
  const visible = expanded ? items : items.slice(0, 1);
  const hidden = items.length - 1;
  return (
    <>
      {visible.map((item) => renderItem(item))}
      {hidden > 0 && (
        <Button
          variant="tertiary"
          size="sm"
          onClick={() => setExpanded((prev) => !prev)}
        >
          <Button.LeftIcon>
            <Icon name={expanded ? "chevron-up" : "chevron-down"} />
          </Button.LeftIcon>
          <Button.Text>
            {expanded ? "Show less" : `Show ${hidden} more ${itemLabel}`}
          </Button.Text>
        </Button>
      )}
    </>
  );
}

function userInitials(email: string): string {
  const cleaned = email.replace(/@.*/, "");
  const parts = cleaned.split(/[._-]+/).filter(Boolean);
  if (parts.length === 0) return "?";
  return parts
    .slice(0, 2)
    .map((part) => part[0]!.toUpperCase())
    .join("");
}

function EvidenceRow({
  result,
  onExclude,
  onDismiss,
}: {
  result: RiskResult;
  onExclude: (result: RiskResult) => void;
  onDismiss: (result: RiskResult) => void;
}): JSX.Element {
  return (
    <div className="border-border overflow-hidden rounded-md border">
      <div className="flex items-center justify-between gap-2 px-3 py-2">
        <span className="text-muted-foreground truncate font-mono text-xs">
          {result.chatTitle || getRuleTitleFallback(result.ruleId)}
        </span>
        <span className="text-muted-foreground shrink-0 font-mono text-xs">
          {formatDistanceToNow(result.createdAt, { addSuffix: true })}
        </span>
      </div>
      {/* The redacted match sits on an inverse code-block backdrop so the
          red redaction chip carries the reference design's contrast. */}
      <div className="bg-foreground px-3 py-4">
        <MaskedMatch
          tone="contrast"
          resultId={result.id}
          matchRedacted={result.matchRedacted}
        />
      </div>
      <div className="flex items-center justify-between gap-2 px-3 py-2">
        <Text small muted className="truncate font-mono">
          Triggered: {result.source} · {getRuleTitleFallback(result.ruleId)}{" "}
          (conf {(result.confidence ?? 0).toFixed(2)})
        </Text>
        <span className="flex shrink-0 gap-1">
          <Button
            variant="tertiary"
            size="sm"
            onClick={() => onExclude(result)}
          >
            <Button.Text>Exclude</Button.Text>
          </Button>
          <Button
            variant="tertiary"
            size="sm"
            onClick={() => onDismiss(result)}
          >
            <Button.Text>False positive</Button.Text>
          </Button>
        </span>
      </div>
    </div>
  );
}

/** Header description line: the confirmation question while the
 * false-positive decision is pending, the rule description otherwise. */
function signalDescription(signal: RiskSignal, confirming: boolean): string {
  if (confirming) return "False positive?";
  return signal.description || "Findings clustered on this detection rule.";
}

/**
 * Inline confirmation shown in place of the drawer's action buttons after
 * "Mark false positive" is clicked — the rest of the signal detail stays
 * visible behind it.
 */
function FalsePositiveConfirm({
  count,
  onConfirm,
  onCancel,
}: {
  /** Findings actually collected for dismissal (may have hit the cap). */
  count: number;
  onConfirm: () => void;
  onCancel: () => void;
}): JSX.Element {
  return (
    <div className="space-y-6">
      {/* Pulled up against the sheet header so the divider reads as the
          header's bottom edge rather than part of the confirmation block. */}
      <Separator className="-mt-2" />
      <div className="flex items-start gap-3">
        <Icon
          name="circle-alert"
          className="text-destructive mt-0.5 size-5 shrink-0"
        />
        <Text className="text-base">
          {count === 0
            ? "No findings to mark in the selected window."
            : `This will mark ${count.toLocaleString()} ${
                count === 1 ? "finding" : "findings"
              } as false positive; they won't be displayed in the watchdog view again.`}
          {count >= SIGNAL_DISMISS_CAP &&
            " There are more findings than can be marked at once; run this again to continue."}
        </Text>
      </div>
      <div className="flex items-center gap-2">
        <Button
          variant="primary"
          className="w-28"
          disabled={count === 0}
          onClick={onConfirm}
        >
          <Button.Text>OK</Button.Text>
        </Button>
        <Button variant="tertiary" className="w-28" onClick={onCancel}>
          <Button.Text>Cancel</Button.Text>
        </Button>
      </div>
      <Separator />
    </div>
  );
}

/**
 * Slide-over detail for one signal: stats, top affected users, redacted
 * evidence, and the two signal-level actions — create an exclusion rule for
 * the whole rule cluster, or mark every finding in the window false positive.
 */
export function SignalDrawer({
  signal,
  from,
  to,
  onClose,
}: {
  signal: RiskSignal | null;
  from: Date;
  to: Date;
  onClose: () => void;
}): JSX.Element {
  const client = useSdkClient();
  const { dismiss, isOptimisticallyDismissed } = useDismissFinding();
  const [exclusionState, setExclusionState] =
    useState<ExclusionSheetState | null>(null);
  const [pendingDismissAll, setPendingDismissAll] = useState<
    RiskResult[] | null
  >(null);
  const [collecting, setCollecting] = useState(false);

  const ruleId = signal?.ruleId ?? "";
  // The list endpoint's rule filter is substring-match, so an id that is a
  // strict prefix of another could over-fetch; the exact-match filter below
  // keeps evidence and dismissal scoped to this signal's rule only.
  //
  // Deliberately unwindowed: signals count findings by scan time while the
  // list endpoint filters by message event time, so a windowed evidence query
  // can come back empty for a signal that clearly has findings (scans of
  // older messages). Latest evidence for the rule is what the drawer wants
  // anyway; only the bulk false-positive action stays window-scoped.
  const evidenceQuery = useRiskListResults({ ruleId, limit: 25 }, undefined, {
    enabled: signal !== null,
    throwOnError: false,
  });
  const evidence = useMemo(
    () =>
      (evidenceQuery.data?.results ?? [])
        .filter((result) => result.ruleId === ruleId)
        .filter((result) => !isOptimisticallyDismissed(result.id))
        .slice(0, EVIDENCE_ROWS),
    [evidenceQuery.data, ruleId, isOptimisticallyDismissed],
  );

  const openSignalExclusion = () => {
    if (!signal) return;
    // The sheet derives its ready-made rule options from the findings it is
    // handed; the loaded evidence rows all share this signal's rule, so the
    // "Any <rule> finding" option is on offer. Before evidence loads the
    // sheet still opens, just without ready-made options.
    setExclusionState({ mode: "create", results: evidence });
  };

  const collectAllFindings = async () => {
    if (!signal) return;
    setCollecting(true);
    try {
      setPendingDismissAll(
        await collectFindingsForRules(client, [signal.ruleId], { from, to }),
      );
    } catch {
      toast.error("Failed to load the signal's findings.");
    } finally {
      setCollecting(false);
    }
  };

  const confirmDismissAll = () => {
    if (!pendingDismissAll) return;
    dismiss(pendingDismissAll);
    setPendingDismissAll(null);
    onClose();
  };

  return (
    <>
      <Sheet
        open={signal !== null}
        onOpenChange={(open) => {
          if (!open) {
            setExclusionState(null);
            setPendingDismissAll(null);
            onClose();
          }
        }}
      >
        <SheetContent
          side="right"
          className="w-full overflow-y-auto sm:max-w-3xl"
          showCloseButton={exclusionState === null}
        >
          {/* The exclusion editor replaces the drawer's contents in place —
              no second sheet stacks on top, and the sheet's close (X)
              affordance only exists on the signal view. A light Back button
              sits beside Create in the footer and returns to the signal. */}
          {signal && exclusionState && (
            <>
              <SheetHeader>
                <SheetTitle>Create exclusion rule</SheetTitle>
                <SheetDescription>
                  Suppress matching findings retroactively and going forward.
                  Does not re-run analysis.
                </SheetDescription>
              </SheetHeader>
              {/* Flex column filling the sheet so the form's footer
                  (mt-auto) pins Back/Create to the drawer's bottom edge. */}
              <div className="flex min-h-0 flex-1 flex-col px-4 pb-6">
                <ExclusionEditor
                  state={exclusionState}
                  onDone={() => setExclusionState(null)}
                  secondaryAction={
                    <Button
                      variant="tertiary"
                      onClick={() => setExclusionState(null)}
                    >
                      <Button.LeftIcon>
                        <Icon name="arrow-left" className="size-4" />
                      </Button.LeftIcon>
                      <Button.Text>Back</Button.Text>
                    </Button>
                  }
                />
              </div>
            </>
          )}
          {signal && !exclusionState && (
            <RevealAllProvider>
              <SheetHeader>
                <div className="flex items-baseline gap-2">
                  <span
                    className="font-display text-2xl leading-none font-thin"
                    style={{
                      color: SCORE_TEXT_COLOR[scoreToRating(signal.riskScore)],
                    }}
                  >
                    {signal.riskScore.toFixed(1)}
                  </span>
                  <Text small muted className="uppercase">
                    {signal.severity}
                  </Text>
                </div>
                <SheetTitle>{getRuleTitleFallback(signal.ruleId)}</SheetTitle>
                <SheetDescription>
                  {signalDescription(signal, pendingDismissAll !== null)}
                </SheetDescription>
              </SheetHeader>
              <div className="flex flex-1 flex-col gap-6 px-4 pb-6">
                {/* Clicking "Mark false positive" swaps the action buttons
                    for an inline confirmation — the rest of the signal
                    detail stays visible; no modal, no view change. */}
                {pendingDismissAll ? (
                  <FalsePositiveConfirm
                    count={pendingDismissAll.length}
                    onConfirm={confirmDismissAll}
                    onCancel={() => setPendingDismissAll(null)}
                  />
                ) : (
                  <div className="flex flex-wrap items-center gap-2">
                    <Button variant="primary" onClick={openSignalExclusion}>
                      <Button.Text>Create exclusion rule</Button.Text>
                    </Button>
                    <Button
                      variant="secondary"
                      disabled={collecting}
                      onClick={() => void collectAllFindings()}
                    >
                      {collecting && (
                        <Button.LeftIcon>
                          <Loader2 className="size-4 animate-spin" />
                        </Button.LeftIcon>
                      )}
                      <Button.Text>Mark false positive</Button.Text>
                    </Button>
                  </div>
                )}
                {/* While the confirmation is up, everything below it sits
                    under a dark scrim and is inert — the decision is the
                    only actionable thing in the drawer. */}
                <div className="relative flex-1">
                  {pendingDismissAll !== null && (
                    // Negative insets cancel the body's px-4/pb-6 padding and
                    // the gap under the divider so the scrim bleeds to the
                    // drawer's edges with no light border around it.
                    <div className="bg-foreground/50 absolute -inset-x-4 -top-6 -bottom-6 z-10" />
                  )}
                  <div
                    className={cn(
                      "space-y-6",
                      pendingDismissAll !== null &&
                        "pointer-events-none select-none",
                    )}
                    aria-hidden={pendingDismissAll !== null}
                  >
                    <Text small muted>
                      First seen {signal.firstSeen.toLocaleString()} · last{" "}
                      {signal.lastSeen.toLocaleString()}
                    </Text>

                    <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
                      <StatCell label="Users">{signal.users}</StatCell>
                      <StatCell label="Findings">
                        {signal.findings.toLocaleString()}
                      </StatCell>
                      <StatCell label="Teams">
                        {signal.teams > 0 ? signal.teams : "-"}
                      </StatCell>
                      <StatCell label="Trend">
                        <SignalTrend
                          findings={signal.findings}
                          previousFindings={signal.previousFindings}
                        />
                      </StatCell>
                    </div>

                    {signal.topUsers.length > 0 && (
                      <>
                        <Separator />
                        <div className="space-y-2">
                          <Text small muted className="font-medium uppercase">
                            Top affected users
                          </Text>
                          <ExpandableList
                            key={`users-${signal.key}`}
                            items={signal.topUsers}
                            itemLabel="users"
                            renderItem={(user) => (
                              <div
                                key={`${user.externalUserId}|${user.email}`}
                                className="flex items-center gap-3"
                              >
                                <Avatar className="size-7">
                                  <AvatarFallback>
                                    {userInitials(user.email)}
                                  </AvatarFallback>
                                </Avatar>
                                <div className="min-w-0 flex-1">
                                  <Text small className="truncate">
                                    {user.email}
                                  </Text>
                                  {user.team && (
                                    <Text small muted className="truncate">
                                      {user.team}
                                    </Text>
                                  )}
                                </div>
                                <Text
                                  small
                                  muted
                                  className="shrink-0 tabular-nums"
                                >
                                  {user.findings.toLocaleString()} findings
                                </Text>
                              </div>
                            )}
                          />
                        </div>
                        <Separator />
                      </>
                    )}

                    <div className="space-y-2">
                      <div className="flex items-center justify-between">
                        <Text small muted className="font-medium uppercase">
                          Evidence · redacted
                        </Text>
                        <RevealAllToggle />
                      </div>
                      {evidenceQuery.isLoading && (
                        <Text small muted>
                          Loading evidence…
                        </Text>
                      )}
                      {evidenceQuery.isError && (
                        <div className="flex items-center gap-2">
                          <Text small muted>
                            Failed to load evidence.
                          </Text>
                          <Button
                            variant="tertiary"
                            size="sm"
                            onClick={() => void evidenceQuery.refetch()}
                          >
                            <Button.Text>Retry</Button.Text>
                          </Button>
                        </div>
                      )}
                      {!evidenceQuery.isLoading &&
                        !evidenceQuery.isError &&
                        evidence.length === 0 && (
                          <Text small muted>
                            No evidence rows in this window.
                          </Text>
                        )}
                      <ExpandableList
                        key={`evidence-${signal.key}`}
                        items={evidence}
                        itemLabel="findings"
                        renderItem={(result) => (
                          <EvidenceRow
                            key={result.id}
                            result={result}
                            onExclude={(r) =>
                              setExclusionState({
                                mode: "create",
                                results: [r],
                              })
                            }
                            onDismiss={(r) => dismiss([r])}
                          />
                        )}
                      />
                    </div>
                  </div>
                </div>
              </div>
            </RevealAllProvider>
          )}
        </SheetContent>
      </Sheet>
    </>
  );
}
