import {
  StatTile,
  StatTileGroup,
  StatTileSkeleton,
} from "@/components/chart/stat-tile";
import { TimeRangePicker } from "@/components/DashboardTimeRangePicker";
import { defineFilters, useFilterState } from "@/components/filters";
import {
  formatDateRangeLabel,
  useDateRangeFilter,
} from "@/components/observe/useDateRangeFilter";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Icon } from "@/components/ui/Icon";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { useSdkClient } from "@/contexts/Sdk";
import { useRowSelection, type RowSelection } from "@/hooks/useRowSelection";
import { Loader2 } from "lucide-react";
import { type DateRangePreset } from "@/elements";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import type { RiskSignal } from "@gram/client/models/components/risksignal.js";
import { useRiskCreateExclusionMutation } from "@gram/client/react-query/riskCreateExclusion.js";
import { useRiskSignals } from "@gram/client/react-query/riskSignals.js";
import { keepPreviousData, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { useSearchParams } from "react-router";
import { toast } from "sonner";
import { cn } from "@/lib/utils";
import {
  getRuleTitleFallback,
  hasJudgeSource,
  scoreToRating,
} from "../risk-utils";
import { invalidateExclusionSurfaces } from "../exclusion-invalidation";
import { useDismissFinding } from "../useDismissFinding";
import {
  SEVERITY_ACCENT,
  SEVERITY_ORDER,
  filterSignalsByCategory,
  filterSignalsBySeverity,
  groupSignals,
  toggleFilterValue,
  type SignalGroupMode,
  type SignalSeverity,
} from "./signals-helpers";
import { collectFindingsForRules } from "./collect-findings";
import { SuppressFindingsDialog } from "./SuppressFindingsDialog";
import { SuppressMenu } from "./SuppressMenu";
import { ExposureBar } from "./ExposureBar";
import { SignalDrawer } from "./SignalDrawer";
import { SignalsList } from "./SignalsList";
import { SuppressedFindings } from "./SuppressedFindings";

const WATCHDOG_PRESETS: DateRangePreset[] = ["1d", "7d", "30d"];

const WATCHDOG_FILTERS = defineFilters([
  { id: "severity", label: "Severity", kind: "multiselect", pinned: true },
  { id: "category", label: "Data type", kind: "multiselect", pinned: true },
]);

const GROUP_OPTIONS: { value: SignalGroupMode; label: string }[] = [
  { value: "severity", label: "Severity" },
  { value: "category", label: "Data type" },
  { value: "team", label: "Team" },
  { value: "app", label: "App" },
];

const GROUP_MODES = new Set<SignalGroupMode>(
  GROUP_OPTIONS.map((option) => option.value),
);

export default function Watchdog(): JSX.Element {
  return (
    <RequireScope scope="org:admin" level="page">
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        <Page.Body>
          <WatchdogContent />
        </Page.Body>
      </Page>
    </RequireScope>
  );
}

function WatchdogContent(): JSX.Element {
  const [searchParams, setSearchParams] = useSearchParams();
  const selectedSignalKey = searchParams.get("signal");
  const groupModeParam = searchParams.get("group");
  const groupMode: SignalGroupMode = GROUP_MODES.has(
    groupModeParam as SignalGroupMode,
  )
    ? (groupModeParam as SignalGroupMode)
    : "severity";

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

  const window = useMemo(() => ({ from, to }), [from, to]);

  const setUrlParam = (key: string, value: string | null) => {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        if (value === null) {
          next.delete(key);
        } else {
          next.set(key, value);
        }
        return next;
      },
      { replace: true },
    );
  };

  const { values, setValue } = useFilterState(WATCHDOG_FILTERS);
  const severityFilter = useMemo(
    () => values.severity ?? [],
    [values.severity],
  );
  const categoryFilter = useMemo(
    () => values.category ?? [],
    [values.category],
  );

  const signalsQuery = useRiskSignals(
    { from: window.from, to: window.to },
    undefined,
    { placeholderData: keepPreviousData, throwOnError: false },
  );
  const data = signalsQuery.data;

  const visibleSignals = useMemo(
    () =>
      filterSignalsByCategory(
        filterSignalsBySeverity(data?.signals ?? [], severityFilter),
        categoryFilter,
      ),
    [data?.signals, severityFilter, categoryFilter],
  );
  const groups = useMemo(
    () => groupSignals(visibleSignals, groupMode),
    [visibleSignals, groupMode],
  );
  const selectedSignal = useMemo(
    () =>
      (data?.signals ?? []).find((s) => s.key === selectedSignalKey) ?? null,
    [data?.signals, selectedSignalKey],
  );

  // Bulk selection across signal rows, mirroring the Risk Events pattern:
  // checkboxes + a floating BulkActionBar with the same two batch actions.
  const client = useSdkClient();
  const queryClient = useQueryClient();
  const { dismiss } = useDismissFinding();
  const createExclusionMutation = useRiskCreateExclusionMutation();
  const selection = useRowSelection(visibleSignals, (signal) => signal.key);
  const hasSelection = selection.selectedCount > 0;
  const [pendingDismiss, setPendingDismiss] = useState<{
    results: RiskResult[];
    signalCount: number;
  } | null>(null);
  const [collecting, setCollecting] = useState(false);
  const [pendingExclusions, setPendingExclusions] = useState<{
    signals: RiskSignal[];
    // Judge-backed signals in the selection, dropped from exclusion creation:
    // their findings can't be excluded (constant rule id per detector), only
    // dismissed. The dialog names how many were set aside.
    skippedJudge: number;
  } | null>(null);
  const [creatingExclusions, setCreatingExclusions] = useState(false);

  const handleDismissSelected = async () => {
    const selected = selection.selectedItems;
    if (selected.length === 0) return;
    setCollecting(true);
    try {
      const results = await collectFindingsForRules(
        client,
        selected.map((signal) => signal.ruleId),
        { from: window.from, to: window.to },
      );
      setPendingDismiss({ results, signalCount: selected.length });
    } catch {
      toast.error("Failed to load the selected signals' findings.");
    } finally {
      setCollecting(false);
    }
  };

  const confirmDismissSelected = () => {
    if (!pendingDismiss) return;
    dismiss(pendingDismiss.results);
    setPendingDismiss(null);
    selection.clear();
  };

  const handleExcludeSelected = () => {
    const selected = selection.selectedItems;
    if (selected.length === 0) return;
    const excludable = selected.filter(
      (signal) => !hasJudgeSource(signal.detectionSources),
    );
    if (excludable.length === 0) {
      toast.info(
        "Prompt-based findings can't be excluded — suppress them instead.",
      );
      return;
    }
    setPendingExclusions({
      signals: excludable,
      skippedJudge: selected.length - excludable.length,
    });
  };

  const confirmCreateExclusions = async () => {
    if (!pendingExclusions) return;
    setCreatingExclusions(true);
    let created = 0;
    const failed: string[] = [];
    for (const signal of pendingExclusions.signals) {
      try {
        await createExclusionMutation.mutateAsync({
          request: {
            createRiskExclusionRequestBody: {
              matchType: "rule_id",
              matchValue: signal.ruleId,
              ruleIdFilter: "",
              sourceFilter: "",
              riskPolicyId: undefined,
              enabled: true,
            },
          },
        });
        created++;
      } catch {
        failed.push(signal.ruleId);
      }
    }
    void invalidateExclusionSurfaces(queryClient);
    if (created > 0) {
      toast.success(
        `Created ${created} exclusion ${created === 1 ? "rule" : "rules"}. Matching findings will update shortly.`,
      );
    }
    if (failed.length > 0) {
      toast.error(`Failed to create exclusions for: ${failed.join(", ")}`);
    }
    setCreatingExclusions(false);
    setPendingExclusions(null);
    selection.clear();
  };

  const rangeLabel = formatDateRangeLabel(dateRange, customRangeLabel);

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
    </span>
  );

  const criticalCount = data?.criticalSignals ?? 0;
  const subtitleSummary = data
    ? `${data.openSignals} open · ${criticalCount} critical`
    : undefined;

  return (
    <Page.Section>
      <Page.Section.Title>Watchdog</Page.Section.Title>
      <Page.Section.Description>
        Your riskiest AI usage, clustered and ranked
        {subtitleSummary ? ` — ${subtitleSummary}` : ""} across {rangeLabel}.
      </Page.Section.Description>
      <Page.Section.CTA>{controls}</Page.Section.CTA>
      <Page.Section.Body>
        <div className="space-y-6">
          {signalsQuery.error ? (
            <WatchdogError message={signalsQuery.error.message} />
          ) : (
            <>
              <KPIRow data={data} isLoading={signalsQuery.isLoading} />
              {/* The exposure bar doubles as the category filter control, so
                  it hides with the other filter controls while a selection is
                  active. */}
              {data && !hasSelection && (
                <ExposureBar
                  slices={data.exposure}
                  totalFindings={data.signals.reduce(
                    (sum, signal) => sum + signal.findings,
                    0,
                  )}
                  activeCategories={categoryFilter}
                  onToggleCategory={(category) =>
                    setValue(
                      "category",
                      toggleFilterValue(categoryFilter, category),
                    )
                  }
                />
              )}
              {/* The "Active signals" bar, after the mockup: heading and
                  shown-count on the left, group-by segmented control plus the
                  severity chip filters on the right. Selection mode swaps the
                  right side for the bulk actions in place, so nothing floats
                  over the list and the select-all checkbox stays put. */}
              <div className="flex flex-wrap items-center justify-between gap-x-6 gap-y-3">
                <div className="flex items-center gap-3">
                  <span className="font-display text-3xl leading-none font-thin">
                    Active signals
                  </span>
                  {hasSelection ? (
                    <Text small className="font-medium whitespace-nowrap">
                      {selection.selectedCount} selected
                    </Text>
                  ) : (
                    <span className="text-muted-foreground font-mono text-sm whitespace-nowrap">
                      {visibleSignals.length} of {data?.signals.length ?? 0}{" "}
                      shown
                    </span>
                  )}
                </div>
                {hasSelection ? (
                  <div className="flex items-center gap-2">
                    <SuppressMenu
                      variant="secondary"
                      size="sm"
                      busy={collecting || creatingExclusions}
                      onSuppressOnce={() => void handleDismissSelected()}
                      onCreateRule={handleExcludeSelected}
                    />
                    <Button
                      variant="secondary"
                      size="sm"
                      onClick={selection.clear}
                    >
                      <Button.Text>Clear</Button.Text>
                    </Button>
                  </div>
                ) : (
                  <div className="flex flex-wrap items-center gap-3">
                    <span className="text-muted-foreground font-mono text-xs tracking-wide uppercase">
                      Group
                    </span>
                    <SegmentedControl
                      value={groupMode}
                      onChange={(mode) =>
                        setUrlParam("group", mode === "severity" ? null : mode)
                      }
                      options={GROUP_OPTIONS}
                    />
                    <div className="flex items-center gap-2">
                      {SEVERITY_ORDER.map((severity) => (
                        <SeverityChip
                          key={severity}
                          severity={severity}
                          active={
                            severityFilter.length === 0 ||
                            severityFilter.includes(severity)
                          }
                          onToggle={() =>
                            setValue(
                              "severity",
                              toggleFilterValue(severityFilter, severity),
                            )
                          }
                        />
                      ))}
                    </div>
                  </div>
                )}
              </div>
              <SignalsBody
                isLoading={signalsQuery.isLoading}
                groups={groups}
                groupMode={groupMode}
                selectedSignalKey={selectedSignalKey}
                selection={selection}
                onSelect={(signal: RiskSignal) =>
                  setUrlParam("signal", signal.key)
                }
              />
            </>
          )}
          {/* Everything the list above deliberately omits. Unfiltered and
              unwindowed on purpose: it's the audit trail for what is being
              hidden, not another view of the current window — and outside the
              signals branch on purpose too, since it reads a different endpoint
              and has its own loading, error, and empty handling. A failed
              signals query must not take the audit trail down with it. */}
          <SuppressedFindings />
          {/* Inside Body on purpose: Page.Section slot-extracts only its known
              child components and silently drops anything else, so the drawer
              must live under a slot to render at all. */}
          <SignalDrawer
            signal={selectedSignal}
            window={window}
            onClose={() => setUrlParam("signal", null)}
          />
          <SuppressFindingsDialog
            results={pendingDismiss?.results ?? null}
            subject={
              pendingDismiss?.signalCount === 1
                ? "the selected signal"
                : `${pendingDismiss?.signalCount ?? 0} selected signals`
            }
            onCancel={() => setPendingDismiss(null)}
            onConfirm={confirmDismissSelected}
          />
          <CreateExclusionsDialog
            signals={pendingExclusions?.signals ?? null}
            skippedJudge={pendingExclusions?.skippedJudge ?? 0}
            creating={creatingExclusions}
            onCancel={() => setPendingExclusions(null)}
            onConfirm={() => void confirmCreateExclusions()}
          />
        </div>
      </Page.Section.Body>
    </Page.Section>
  );
}

/**
 * One severity filter chip, after the mockup's legend-style toggles: outlined
 * in the severity's accent color with a matching swatch, dimmed while filtered
 * out. An empty severity filter means "show all", so every chip renders active
 * until one is toggled.
 */
function SeverityChip({
  severity,
  active,
  onToggle,
}: {
  severity: SignalSeverity;
  active: boolean;
  onToggle: () => void;
}): JSX.Element {
  const color = SEVERITY_ACCENT[severity];
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onToggle}
      className={cn(
        "inline-flex h-10 cursor-pointer items-center gap-2 rounded-sm border px-3 font-mono text-xs tracking-wide uppercase transition-opacity",
        !active && "opacity-40",
      )}
      style={{ borderColor: color, color }}
    >
      <span aria-hidden className="size-2" style={{ backgroundColor: color }} />
      {severity}
    </button>
  );
}

/** StatTile tone for the org risk score, mirroring the signal table's
 * severity coding: red for high/critical bands, amber for medium, plain
 * ink for low. */
function riskScoreTone(score: number): "destructive" | "warning" | "neutral" {
  switch (scoreToRating(score)) {
    case "critical":
    case "high":
      return "destructive";
    case "medium":
      return "warning";
    case "low":
      return "neutral";
  }
}

function exclusionsDialogDescription(count: number): string {
  if (count === 1) {
    return "Creates a global exclusion rule for the selected signal, suppressing its findings retroactively and going forward.";
  }
  return `Creates ${count} global exclusion rules — one per selected signal — suppressing their findings retroactively and going forward.`;
}

function CreateExclusionsDialog({
  signals,
  skippedJudge,
  creating,
  onCancel,
  onConfirm,
}: {
  signals: RiskSignal[] | null;
  /** Judge-backed signals dropped from the selection — not excludable. */
  skippedJudge: number;
  creating: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}): JSX.Element {
  return (
    <Dialog
      open={signals !== null}
      onOpenChange={(open) => {
        if (!open && !creating) onCancel();
      }}
    >
      <Dialog.Content>
        <Dialog.Title>
          {signals?.length === 1
            ? "Create exclusion rule?"
            : "Create exclusion rules?"}
        </Dialog.Title>
        <Dialog.Description>
          {exclusionsDialogDescription(signals?.length ?? 0)}
        </Dialog.Description>
        <ul className="text-muted-foreground list-inside list-disc text-sm">
          {signals?.map((signal) => (
            <li key={signal.key}>{getRuleTitleFallback(signal.ruleId)}</li>
          ))}
        </ul>
        {skippedJudge > 0 && (
          <Text small muted>
            {skippedJudge} prompt-based{" "}
            {skippedJudge === 1 ? "signal was" : "signals were"} left out —
            those findings can't be excluded. Suppress them instead.
          </Text>
        )}
        <Dialog.Footer>
          <Button variant="tertiary" disabled={creating} onClick={onCancel}>
            <Button.Text>Cancel</Button.Text>
          </Button>
          <Button variant="primary" disabled={creating} onClick={onConfirm}>
            {creating && (
              <Button.LeftIcon>
                <Loader2 className="size-4 animate-spin" />
              </Button.LeftIcon>
            )}
            <Button.Text>Create exclusions</Button.Text>
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}

function WatchdogError({ message }: { message: string }): JSX.Element {
  return (
    <div className="bg-muted/20 flex flex-col items-center justify-center rounded-lg border border-dashed px-8 py-16 text-center">
      <div className="bg-muted/50 mb-4 flex size-12 items-center justify-center rounded-full">
        <Icon name="circle-alert" className="text-muted-foreground size-6" />
      </div>
      <p className="text-foreground text-sm font-medium">
        Error loading watchdog signals
      </p>
      <p className="text-muted-foreground mt-1 max-w-md text-sm">{message}</p>
    </div>
  );
}

type SignalsData = NonNullable<ReturnType<typeof useRiskSignals>["data"]>;

function KPIRow({
  data,
  isLoading,
}: {
  data: SignalsData | undefined;
  isLoading: boolean;
}): JSX.Element {
  if (!data && isLoading) {
    return (
      <StatTileGroup>
        <StatTileSkeleton />
        <StatTileSkeleton />
        <StatTileSkeleton />
        <StatTileSkeleton />
      </StatTileGroup>
    );
  }
  if (!data) return <></>;

  return (
    <StatTileGroup>
      <StatTile
        title="Org risk score"
        value={data.orgRiskScore}
        displayValue={data.orgRiskScore.toFixed(1)}
        previousValue={data.previousOrgRiskScore}
        invertDelta
        tone={riskScoreTone(data.orgRiskScore)}
        icon="gauge"
        comparisonLabel="vs previous period"
        subtext={
          data.criticalSignals > 0
            ? `driven by ${data.criticalSignals} critical ${data.criticalSignals === 1 ? "signal" : "signals"}`
            : undefined
        }
      />
      <StatTile
        title="Findings"
        value={data.findings}
        previousValue={data.previousFindings}
        invertDelta
        tone="neutral"
        icon="flag"
        comparisonLabel="vs previous period"
      />
      <StatTile
        title="Open signals"
        value={data.openSignals}
        // Critical signals turn the count red — the tone-based replacement
        // for the removed accentColor prop.
        tone={data.criticalSignals > 0 ? "destructive" : "neutral"}
        icon="radar"
        subtext={`${data.criticalSignals} critical`}
      />
      <StatTile
        title="Users exposed"
        value={data.usersExposed}
        previousValue={data.previousUsersExposed}
        invertDelta
        tone="neutral"
        icon="users"
        comparisonLabel="vs previous period"
      />
    </StatTileGroup>
  );
}

function SignalsBody({
  isLoading,
  groups,
  groupMode,
  selectedSignalKey,
  selection,
  onSelect,
}: {
  isLoading: boolean;
  groups: ReturnType<typeof groupSignals>;
  groupMode: SignalGroupMode;
  selectedSignalKey: string | null;
  selection: RowSelection<RiskSignal>;
  onSelect: (signal: RiskSignal) => void;
}): JSX.Element {
  if (isLoading && groups.length === 0) {
    return (
      <div className="space-y-2">
        <Skeleton className="h-24" />
        <Skeleton className="h-24" />
        <Skeleton className="h-24" />
      </div>
    );
  }
  if (groups.length === 0) {
    return (
      <div className="bg-muted/20 flex flex-col items-center justify-center rounded-lg border border-dashed px-8 py-16 text-center">
        <Text className="font-medium">No open signals</Text>
        <Text small muted className="mt-1 max-w-md">
          No live findings match this window and filter. Widen the time range or
          clear the severity filter.
        </Text>
      </div>
    );
  }
  return (
    <SignalsList
      groups={groups}
      mode={groupMode}
      selectedKey={selectedSignalKey}
      selection={selection}
      onSelect={onSelect}
    />
  );
}
