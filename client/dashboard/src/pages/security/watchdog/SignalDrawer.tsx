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
import { useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { ExclusionEditor, type ExclusionSheetState } from "../exclusion-sheet";
import {
  EventMatchDialog,
  MaskedMatch,
  RevealAllProvider,
  RevealAllToggle,
} from "../risk-ui";
import {
  getCategoryCodeForFinding,
  getRuleTitleFallback,
  hasJudgeSource,
  isJudgeSource,
  scoreToRating,
} from "../risk-utils";
import { useDismissFinding } from "../useDismissFinding";
import { collectFindingsForRules } from "./collect-findings";
import { DismissFindingsDialog } from "./DismissFindingsDialog";
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
  // A judge finding's "match" is the entire flagged event (often absent on
  // the realtime path), and its description carries the verdict rationale —
  // so the evidence cell shows the rationale with the audited event dialog
  // behind it instead of a redaction chip over content that may not exist.
  // Judge findings also can't be excluded (their rule id is a constant for
  // the whole detector), so the row offers only false-positive dismissal.
  const judge = isJudgeSource(result.source);
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
      {judge ? (
        <div className="px-3 py-3">
          <EventMatchDialog
            resultId={result.id}
            matchRedacted={result.matchRedacted}
            rationale={result.description}
          />
        </div>
      ) : (
        // The redacted match sits on an inverse code-block backdrop so the
        // red redaction chip carries the reference design's contrast.
        <div className="bg-foreground px-3 py-4">
          <MaskedMatch
            tone="contrast"
            wrap
            resultId={result.id}
            matchRedacted={result.matchRedacted}
          />
        </div>
      )}
      <div className="flex items-center justify-between gap-2 px-3 py-2">
        <Text small muted className="truncate font-mono">
          {/* Category code, never the raw scanner source — and no rule title
              for judge findings, whose single rule restates the category. */}
          Triggered: {getCategoryCodeForFinding(result.source, result.ruleId)}
          {!judge && ` · ${getRuleTitleFallback(result.ruleId)}`} (conf{" "}
          {(result.confidence ?? 0).toFixed(2)})
        </Text>
        <span className="flex shrink-0 gap-1">
          {!judge && (
            <Button
              variant="tertiary"
              size="sm"
              onClick={() => onExclude(result)}
            >
              <Button.Text>Exclude</Button.Text>
            </Button>
          )}
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

/**
 * Slide-over detail for one signal: stats, top affected users, redacted
 * evidence, and the signal-level action — create an exclusion rule for the
 * whole rule cluster.
 */
export function SignalDrawer({
  signal,
  window,
  onClose,
}: {
  signal: RiskSignal | null;
  /** The page's active time window; scopes signal-level dismissal the same
   * way the list's bulk "Mark as false positive" is scoped. */
  window: { from?: Date; to?: Date };
  onClose: () => void;
}): JSX.Element {
  const client = useSdkClient();
  const { dismiss, isOptimisticallyDismissed } = useDismissFinding();
  const [exclusionState, setExclusionState] =
    useState<ExclusionSheetState | null>(null);
  const [pendingDismiss, setPendingDismiss] = useState<RiskResult[] | null>(
    null,
  );
  const [collecting, setCollecting] = useState(false);
  // Set when leaving the exclusion editor so the remounting detail view
  // slides back in from the left — but never on the drawer's first open,
  // where the Sheet's own slide already animates the content.
  const [returningFromEditor, setReturningFromEditor] = useState(false);

  const closeEditor = () => {
    setExclusionState(null);
    setReturningFromEditor(true);
  };

  const ruleId = signal?.ruleId ?? "";
  // The list endpoint's rule filter is substring-match, so an id that is a
  // strict prefix of another could over-fetch; the exact-match filter below
  // keeps evidence and dismissal scoped to this signal's rule only.
  //
  // Deliberately unwindowed: signals count findings by scan time while the
  // list endpoint filters by message event time, so a windowed evidence query
  // can come back empty for a signal that clearly has findings (scans of
  // older messages). Latest evidence for the rule is what the drawer wants.
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

  // Judge-backed signals get false-positive dismissal as the signal-level
  // action instead of an exclusion rule. Same collect-then-confirm shape as
  // the list's bulk action, scoped to this signal's rule and window.
  const judgeSignal =
    signal !== null && hasJudgeSource(signal.detectionSources);

  // The drawer stays mounted across signal switches and closes, so a
  // collection that was in flight when either happened must not open the
  // confirm dialog with the previous signal's findings. The ref tracks the
  // currently displayed signal; a finished collection only lands if it still
  // matches.
  const activeSignalKey = useRef<string | null>(null);
  useEffect(() => {
    activeSignalKey.current = signal?.key ?? null;
  }, [signal]);

  const openSignalDismiss = async () => {
    if (!signal) return;
    const requestKey = signal.key;
    setCollecting(true);
    try {
      const results = await collectFindingsForRules(
        client,
        [signal.ruleId],
        window,
      );
      if (activeSignalKey.current !== requestKey) return;
      setPendingDismiss(results);
    } catch {
      if (activeSignalKey.current !== requestKey) return;
      toast.error("Failed to load this signal's findings.");
    } finally {
      setCollecting(false);
    }
  };

  const confirmSignalDismiss = () => {
    if (!pendingDismiss) return;
    dismiss(pendingDismiss);
    setPendingDismiss(null);
  };

  return (
    <>
      <Sheet
        open={signal !== null}
        onOpenChange={(open) => {
          if (!open) {
            setExclusionState(null);
            setReturningFromEditor(false);
            setPendingDismiss(null);
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
          {signal &&
            exclusionState && (
              // The same slide-in the Sheet itself uses when opening, so
              // swapping to the editor reads as a drawer view transition.
              <div className="animate-in slide-in-from-right flex min-h-0 flex-1 flex-col duration-300 ease-in-out">
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
                    onDone={closeEditor}
                    embedded
                    secondaryAction={
                      <Button variant="tertiary" onClick={closeEditor}>
                        <Button.LeftIcon>
                          <Icon name="arrow-left" className="size-4" />
                        </Button.LeftIcon>
                        <Button.Text>Back</Button.Text>
                      </Button>
                    }
                  />
                </div>
              </div>
            )}
          {signal && !exclusionState && (
            <RevealAllProvider>
              {/* Mirrors the editor's entry: coming back slides the detail
                  view in from the left. First open animates via the Sheet
                  itself, so the class only applies after leaving the editor. */}
              <div
                className={cn(
                  "flex min-h-0 flex-1 flex-col",
                  returningFromEditor &&
                    "animate-in slide-in-from-left duration-300 ease-in-out",
                )}
              >
                <SheetHeader>
                  <div className="flex items-baseline gap-2">
                    <span
                      className="font-display text-2xl leading-none font-thin"
                      style={{
                        color:
                          SCORE_TEXT_COLOR[scoreToRating(signal.riskScore)],
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
                    {signal.description ||
                      "Findings clustered on this detection rule."}
                  </SheetDescription>
                </SheetHeader>
                <div className="flex flex-1 flex-col gap-6 px-4 pb-6">
                  <div className="flex flex-wrap items-center gap-2">
                    {judgeSignal ? (
                      <>
                        <Button
                          variant="primary"
                          disabled={collecting}
                          onClick={() => void openSignalDismiss()}
                        >
                          {collecting && (
                            <Button.LeftIcon>
                              <Loader2 className="size-4 animate-spin" />
                            </Button.LeftIcon>
                          )}
                          <Button.Text>Mark all as false positive</Button.Text>
                        </Button>
                        <Text small muted>
                          Prompt-based findings can't be excluded.
                        </Text>
                      </>
                    ) : (
                      <Button variant="primary" onClick={openSignalExclusion}>
                        <Button.Text>Create exclusion rule</Button.Text>
                      </Button>
                    )}
                  </div>
                  <div className="relative flex-1">
                    <div className="space-y-6">
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
                          <SignalTrend sparkline={signal.sparkline} />
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
                            {/* Judge evidence shows rationales, not redacted
                                matches, so the label and the reveal-all
                                toggle (which only drives MaskedMatch rows)
                                would both mislead there. */}
                            {judgeSignal ? "Evidence" : "Evidence · redacted"}
                          </Text>
                          {!judgeSignal && <RevealAllToggle />}
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
              </div>
            </RevealAllProvider>
          )}
        </SheetContent>
      </Sheet>
      <DismissFindingsDialog
        results={pendingDismiss}
        subject="this signal"
        onCancel={() => setPendingDismiss(null)}
        onConfirm={confirmSignalDismiss}
      />
    </>
  );
}
