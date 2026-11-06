import {
  StatTile,
  StatTileGroup,
  StatTileSkeleton,
} from "@/components/chart/stat-tile";
import { TimeRangePicker } from "@/components/DashboardTimeRangePicker";
import { useFilterState } from "@/components/filters";
import { IdentityLink } from "@/components/identity-link";
import { InlineEmptyState } from "@/components/inline-empty-state";
import {
  formatDateRangeLabel,
  useDateRangeFilter,
} from "@/components/observe/useDateRangeFilter";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Avatar, AvatarFallback } from "@/components/ui/Avatar";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { SkeletonParagraph } from "@/components/ui/Skeleton";
import { Spinner } from "@/components/ui/Spinner";
import { Text } from "@/components/ui/Text";
import { useSdkClient } from "@/contexts/Sdk";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { pluralize } from "@/lib/format";
import { ChatDetailSheet } from "@/pages/chatLogs/ChatDetailPanel";
import { useRoutes } from "@/routes";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import { useRiskListResults } from "@gram/client/react-query/riskListResults.js";
import { useRiskSignals } from "@gram/client/react-query/riskSignals.js";
import { keepPreviousData } from "@tanstack/react-query";
import { formatDistanceToNow } from "date-fns";
import { useLayoutEffect, useMemo, useRef, useState } from "react";
import { useLocation, useNavigate, useParams } from "react-router";
import { toast } from "sonner";
import { ExclusionSheet, type ExclusionSheetState } from "../exclusion-sheet";
import { MCPFindingContext } from "../MCPFindingContext";
import { isMCPFinding, type MCPFindingNames } from "../mcp-finding-context";
import {
  EventMatchDialog,
  MaskedMatch,
  RevealAllProvider,
  RevealAllToggle,
} from "../risk-ui";
import {
  evidenceShowsRuleTitle,
  getCategoryCodeForFinding,
  getRuleTitleFallback,
  hasJudgeSource,
  hasOnlyRationaleSources,
  isJudgeSource,
  isRationaleSource,
  scoreToRating,
} from "../risk-utils";
import { useDismissFinding } from "../useDismissFinding";
import { collectFindingsForRules } from "./collect-findings";
import { EvidenceTitle } from "./EvidenceTitle";
import {
  SCORE_TEXT_COLOR,
  SEVERITY_GROUP_LABEL,
  riskScoreTone,
} from "./signals-helpers";
import { SignalTimeline } from "./SignalTimeline";
import { SuppressFindingsDialog } from "./SuppressFindingsDialog";
import { SuppressMenu } from "./SuppressMenu";
import {
  useMCPFindingNames,
  WATCHDOG_PRESETS,
  WATCHDOG_SERVER_FILTERS,
} from "./watchdog-shared";

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
  mcpFindingNames,
  onExclude,
  onDismiss,
  onOpenChat,
}: {
  result: RiskResult;
  mcpFindingNames?: MCPFindingNames;
  onExclude: (result: RiskResult) => void;
  onDismiss: (result: RiskResult) => void;
  onOpenChat: (chatId: string, chatMessageId?: string) => void;
}): JSX.Element {
  // A judge finding's "match" is the entire flagged event (often absent on
  // the realtime path), and its description carries the verdict rationale —
  // so the evidence cell shows the rationale with the audited event dialog
  // behind it instead of a redaction chip over content that may not exist.
  // Judge findings also can't be excluded (their rule id is a constant for
  // the whole detector), so the row offers only suppression. Every other
  // detector gets the shared Suppress menu: a one-off manual suppression or
  // exclusion rule creation, same affordance as the drawer and list actions.
  //
  // LLM analyzer findings sit in between: their evidence is the model's
  // rationale (no match to redact), and their single rule per category
  // restates the category code, so the footer names the category alone
  // (except the dead-letter sentinel, whose title is the only hint that the
  // analysis never ran) — but a rule exclusion silences just that category,
  // so they keep the menu.
  const judge = isJudgeSource(result.source);
  const rationale = isRationaleSource(result.source);
  const showRuleTitle = evidenceShowsRuleTitle(result.source, result.ruleId);
  return (
    <div className="border-border overflow-hidden rounded-md border">
      {isMCPFinding(result) ? (
        <div className="flex items-start justify-between gap-4 px-3 py-2">
          <MCPFindingContext
            showOutcome
            finding={result}
            names={mcpFindingNames}
            className="min-w-0"
          />
          <span className="text-muted-foreground shrink-0 font-mono text-xs">
            {formatDistanceToNow(result.createdAt, { addSuffix: true })}
          </span>
        </div>
      ) : (
        <EvidenceTitle
          title={result.chatTitle || getRuleTitleFallback(result.ruleId)}
          createdAt={result.createdAt}
          chatId={result.chatId}
          chatMessageId={result.chatMessageId}
          onOpenChat={onOpenChat}
        />
      )}
      {rationale ? (
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
              for judge or LLM analyzer category findings, whose single rule
              restates the category. */}
          Triggered: {getCategoryCodeForFinding(result.source, result.ruleId)}
          {showRuleTitle &&
            ` · ${getRuleTitleFallback(result.ruleId)}`} (conf{" "}
          {(result.confidence ?? 0).toFixed(2)})
        </Text>
        <span className="flex shrink-0 gap-1">
          {judge ? (
            <Button
              variant="tertiary"
              size="sm"
              onClick={() => onDismiss(result)}
            >
              <Button.Text>Suppress</Button.Text>
            </Button>
          ) : (
            <SuppressMenu
              variant="tertiary"
              size="sm"
              onSuppressOnce={() => onDismiss(result)}
              onCreateRule={() => onExclude(result)}
            />
          )}
        </span>
      </div>
    </div>
  );
}

/** Evidence rows the page loads; the drawer showed 5 behind an expander. */
const EVIDENCE_LIMIT = 25;
const EVIDENCE_ROWS = 10;

/**
 * One signal's page: when its findings happened across the selected window,
 * who they came from, and the latest redacted evidence — with the
 * signal-level suppress / exclude action in the header. Same frame as the
 * Watchdog list it opens from.
 */
export default function SignalDetail(): JSX.Element {
  return (
    <RequireScope scope="org:admin" level="page">
      <SignalDetailPage />
    </RequireScope>
  );
}

function SignalDetailPage(): JSX.Element {
  const { ruleId = "" } = useParams();
  const title = getRuleTitleFallback(ruleId);
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          substitutions={{
            [ruleId]: title,
            [encodeURIComponent(ruleId)]: title,
          }}
        />
      </Page.Header>
      <Page.Body fullWidth>
        <SignalDetailContent ruleId={ruleId} title={title} />
      </Page.Body>
    </Page>
  );
}

function SignalDetailContent({
  ruleId,
  title,
}: {
  ruleId: string;
  title: string;
}): JSX.Element {
  const routes = useRoutes();
  const navigate = useNavigate();
  const location = useLocation();
  const client = useSdkClient();
  const { names: mcpFindingNames } = useMCPFindingNames();
  const { dismiss, isOptimisticallyDismissed } = useDismissFinding();

  // Same URL params and defaults as the list, so the numbers here match the
  // row the user clicked and the back link returns to the same view.
  const {
    dateRange,
    customRange,
    customRangeLabel,
    from,
    to,
    setDateRangeParam,
    setCustomRangeParam,
    clearCustomRange,
  } = useDateRangeFilter("1d");
  const rangeLabel = formatDateRangeLabel(dateRange, customRangeLabel);
  const mcpScoped =
    useFeatureFlag(FEATURE_FLAGS.mcpScopedPolicies).status === "enabled";
  const { values: serverValues } = useFilterState(WATCHDOG_SERVER_FILTERS);
  const mcpServerId = mcpScoped ? (serverValues.mcp_server_id ?? "") : "";

  const signalsQuery = useRiskSignals(
    { from, to, mcpServerId: mcpServerId || undefined },
    undefined,
    { placeholderData: keepPreviousData, throwOnError: false },
  );
  const signal = useMemo(
    () => signalsQuery.data?.signals.find((s) => s.ruleId === ruleId) ?? null,
    [signalsQuery.data, ruleId],
  );

  const [exclusionState, setExclusionState] =
    useState<ExclusionSheetState | null>(null);
  const [pendingDismiss, setPendingDismiss] = useState<RiskResult[] | null>(
    null,
  );
  const [collecting, setCollecting] = useState(false);
  const [openChat, setOpenChat] = useState<{
    chatId: string;
    chatMessageId: string | undefined;
  } | null>(null);

  // The list endpoint's rule filter is substring-match, so an id that is a
  // strict prefix of another could over-fetch; the exact-match filter below
  // keeps evidence scoped to this rule.
  //
  // Deliberately unwindowed: signals count findings by scan time while the
  // list endpoint filters by message event time, so a windowed evidence query
  // can come back empty for a signal that clearly has findings (scans of
  // older messages). Latest evidence for the rule is what the page wants.
  const evidenceQuery = useRiskListResults(
    { ruleId, mcpServerId: mcpServerId || undefined, limit: EVIDENCE_LIMIT },
    undefined,
    { throwOnError: false },
  );
  const evidence = useMemo(
    () =>
      (evidenceQuery.data?.results ?? [])
        .filter((result) => result.ruleId === ruleId)
        .filter((result) => !isOptimisticallyDismissed(result.id))
        .slice(0, EVIDENCE_ROWS),
    [evidenceQuery.data, ruleId, isOptimisticallyDismissed],
  );

  const backHref = `${routes.watchdog.href()}${location.search}`;

  // A scope change while a collection is in flight must not open a dialog for
  // the old scope.
  const collectionToken = useRef(0);
  useLayoutEffect(() => {
    collectionToken.current += 1;
    setPendingDismiss(null);
    setCollecting(false);
  }, [ruleId, mcpServerId]);

  const openSignalDismiss = async () => {
    const token = collectionToken.current;
    setCollecting(true);
    try {
      // Unwindowed on purpose, like the evidence query above.
      const results = await collectFindingsForRules(client, [ruleId], {
        from: undefined,
        to: undefined,
        mcpServerId: mcpServerId || undefined,
      });
      if (collectionToken.current !== token) return;
      if (results.length === 0) {
        toast.error("No suppressible findings found for this signal.");
        return;
      }
      setPendingDismiss(results);
    } catch {
      if (collectionToken.current !== token) return;
      toast.error("Failed to load this signal's findings.");
    } finally {
      if (collectionToken.current === token) setCollecting(false);
    }
  };

  const confirmSignalDismiss = () => {
    if (!pendingDismiss) return;
    dismiss(pendingDismiss);
    setPendingDismiss(null);
    // The whole signal is gone, so return to the list it came from.
    void navigate(backHref);
  };

  // Judge-backed signals can't be excluded (their rule id is a constant for
  // the whole detector), so they get suppression alone.
  const judgeSignal =
    signal !== null && hasJudgeSource(signal.detectionSources);
  // Judge and LLM analyzer evidence shows rationales, not redacted matches,
  // so the "redacted" label and reveal-all toggle would both mislead.
  const rationaleSignal =
    signal !== null && hasOnlyRationaleSources(signal.detectionSources);

  const controls = (
    <span className="flex items-center gap-2">
      <TimeRangePicker
        preset={customRange ? null : dateRange}
        customRange={customRange}
        customRangeLabel={customRangeLabel}
        availablePresets={WATCHDOG_PRESETS}
        onPresetChange={(preset) => setDateRangeParam(preset)}
        onCustomRangeChange={(rangeFrom, rangeTo, label) =>
          setCustomRangeParam(rangeFrom, rangeTo, label)
        }
        onClearCustomRange={clearCustomRange}
      />
      {signal &&
        (judgeSignal ? (
          <Button
            variant="primary"
            disabled={collecting}
            onClick={() => void openSignalDismiss()}
          >
            {collecting && (
              <Button.LeftIcon>
                <Spinner className="mr-0" />
              </Button.LeftIcon>
            )}
            <Button.Text>Suppress all</Button.Text>
          </Button>
        ) : (
          <SuppressMenu
            variant="primary"
            busy={collecting}
            onSuppressOnce={() => void openSignalDismiss()}
            onCreateRule={() =>
              // Evidence that has arrived rides along for the custom branch;
              // the rule preset works before it lands.
              setExclusionState({
                mode: "create",
                results: evidence,
                presetRuleId: ruleId,
              })
            }
          />
        ))}
    </span>
  );

  const rating = signal ? scoreToRating(signal.riskScore) : null;

  return (
    <Page.Section>
      <Page.Section.Title>{title}</Page.Section.Title>
      <Page.Section.Description>
        {signal?.description || "Findings clustered on this detection rule."}
      </Page.Section.Description>
      <Page.Section.CTA>{controls}</Page.Section.CTA>
      <Page.Section.Body>
        <div className="space-y-10">
          {signalsQuery.error ? (
            <InlineEmptyState
              icon="circle-alert"
              heading="Couldn't load this signal"
              description={signalsQuery.error.message}
              action={
                <Button
                  variant="secondary"
                  onClick={() => void signalsQuery.refetch()}
                >
                  <Button.Text>Retry</Button.Text>
                </Button>
              }
            />
          ) : !signal && signalsQuery.isLoading ? (
            <StatTileGroup>
              <StatTileSkeleton />
              <StatTileSkeleton />
              <StatTileSkeleton />
            </StatTileGroup>
          ) : !signal ? (
            <InlineEmptyState
              icon="radar"
              heading={`No findings for this rule in the ${rangeLabel}`}
              description="Pick a longer time range, or go back to the signals that are active now."
              action={
                <Button
                  variant="secondary"
                  onClick={() => void navigate(backHref)}
                >
                  <Button.LeftIcon>
                    <Icon name="arrow-left" />
                  </Button.LeftIcon>
                  <Button.Text>All signals</Button.Text>
                </Button>
              }
            />
          ) : (
            <>
              <StatTileGroup>
                <StatTile
                  title="Risk score"
                  value={signal.riskScore}
                  displayValue={signal.riskScore.toFixed(1)}
                  tone={riskScoreTone(signal.riskScore)}
                  icon="gauge"
                  subtext={rating ? SEVERITY_GROUP_LABEL[rating] : undefined}
                />
                <StatTile
                  title="Findings"
                  value={signal.findings}
                  previousValue={signal.previousFindings}
                  invertDelta
                  tone="neutral"
                  icon="flag"
                  comparisonLabel="vs previous period"
                />
                <StatTile
                  title="Users exposed"
                  value={signal.users}
                  tone="neutral"
                  icon="users"
                  subtext={
                    signal.teams > 0
                      ? `across ${pluralize(signal.teams, "team")}`
                      : undefined
                  }
                />
              </StatTileGroup>

              <section className="space-y-4">
                <SectionHeading
                  title="Findings over time"
                  meta={rangeLabel}
                  action={
                    <span
                      className="text-muted-foreground font-mono text-xs"
                      title={`First seen ${signal.firstSeen.toLocaleString()} · last seen ${signal.lastSeen.toLocaleString()}`}
                    >
                      First seen{" "}
                      {formatDistanceToNow(signal.firstSeen, {
                        addSuffix: true,
                      })}{" "}
                      · last{" "}
                      {formatDistanceToNow(signal.lastSeen, {
                        addSuffix: true,
                      })}
                    </span>
                  }
                />
                <div className="bg-card border p-4">
                  <SignalTimeline
                    sparkline={signal.sparkline}
                    from={from}
                    to={to}
                    color={SCORE_TEXT_COLOR[scoreToRating(signal.riskScore)]}
                    height={240}
                  />
                </div>
              </section>

              <div className="grid gap-10 lg:grid-cols-[minmax(0,1fr)_minmax(0,2fr)]">
                <section className="space-y-4">
                  <SectionHeading
                    title="Top affected users"
                    meta={rangeLabel}
                  />
                  {signal.topUsers.length === 0 ? (
                    <Text small muted>
                      No user attribution on these findings.
                    </Text>
                  ) : (
                    <div className="bg-card divide-border divide-y border">
                      {signal.topUsers.map((user) => (
                        <div
                          key={`${user.externalUserId}|${user.email}`}
                          className="flex items-center gap-3 px-4 py-3"
                        >
                          <Avatar className="size-7">
                            <AvatarFallback>
                              {userInitials(user.email)}
                            </AvatarFallback>
                          </Avatar>
                          <div className="min-w-0 flex-1">
                            <Text small className="truncate">
                              {/* Risk keys on the reported agent id when it
                                  has one; the address is the fallback. */}
                              <IdentityLink
                                identifier={
                                  user.externalUserId
                                    ? { externalUserId: user.externalUserId }
                                    : user.email.includes("@")
                                      ? { email: user.email }
                                      : null
                                }
                              >
                                {user.email}
                              </IdentityLink>
                            </Text>
                            {user.team && (
                              <Text small muted className="truncate">
                                {user.team}
                              </Text>
                            )}
                          </div>
                          <span className="text-muted-foreground shrink-0 font-mono text-xs tabular-nums">
                            {pluralize(user.findings, "finding")}
                          </span>
                        </div>
                      ))}
                    </div>
                  )}
                </section>

                <RevealAllProvider>
                  <section className="space-y-4">
                    {/* Evidence is deliberately unwindowed (see the query
                        above), so it says so instead of borrowing the range. */}
                    <SectionHeading
                      title="Latest evidence"
                      meta={
                        rationaleSignal ? "any time" : "any time · redacted"
                      }
                      action={!rationaleSignal ? <RevealAllToggle /> : null}
                    />
                    {evidenceQuery.isLoading ? (
                      <SkeletonParagraph label="Loading evidence" />
                    ) : evidenceQuery.isError ? (
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
                    ) : evidence.length === 0 ? (
                      <Text small muted>
                        No evidence rows for this rule.
                      </Text>
                    ) : (
                      <div className="space-y-3">
                        {evidence.map((result) => (
                          <EvidenceRow
                            key={result.id}
                            result={result}
                            mcpFindingNames={mcpFindingNames}
                            onExclude={(r) =>
                              setExclusionState({
                                mode: "create",
                                results: [r],
                              })
                            }
                            onDismiss={(r) => dismiss([r])}
                            onOpenChat={(chatId, chatMessageId) =>
                              setOpenChat({ chatId, chatMessageId })
                            }
                          />
                        ))}
                      </div>
                    )}
                  </section>
                </RevealAllProvider>
              </div>
            </>
          )}
          {/* Inside Body on purpose: Page.Section slot-extracts only its known
              child components and silently drops anything else. */}
          <ExclusionSheet
            state={exclusionState}
            onOpenChange={(open) => {
              if (!open) setExclusionState(null);
            }}
          />
          <ChatDetailSheet
            chatId={openChat?.chatId ?? null}
            focusedMessageId={openChat?.chatMessageId}
            onClose={() => setOpenChat(null)}
            onDelete={() => setOpenChat(null)}
            riskFocus
          />
          <SuppressFindingsDialog
            results={pendingDismiss}
            subject="this signal"
            onCancel={() => setPendingDismiss(null)}
            onConfirm={confirmSignalDismiss}
          />
        </div>
      </Page.Section.Body>
    </Page.Section>
  );
}

/** Section head in the list page's idiom: thin serif title, mono meta. */
function SectionHeading({
  title,
  meta,
  action,
}: {
  title: string;
  meta?: string;
  action?: React.ReactNode;
}): JSX.Element {
  return (
    <div className="flex flex-wrap items-center justify-between gap-3">
      <div className="flex items-baseline gap-3">
        <span className="font-display text-3xl leading-none font-thin">
          {title}
        </span>
        {meta && (
          <span className="text-muted-foreground font-mono text-sm">
            {meta}
          </span>
        )}
      </div>
      {action}
    </div>
  );
}
