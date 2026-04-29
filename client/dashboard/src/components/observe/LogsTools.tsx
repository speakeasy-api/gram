import { EditServerNameDialog } from "@/pages/hooks/EditServerNameDialog";
import { EnableLoggingOverlay } from "@/components/EnableLoggingOverlay";
import { EnterpriseGate } from "@/components/enterprise-gate";
import { InsightsConfig } from "@/components/insights-sidebar";
import { ObservabilitySkeleton } from "@/components/ObservabilitySkeleton";
import { ErrorAlert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { Checkbox } from "@/components/ui/checkbox";
import { MultiSearch } from "@/components/ui/multi-search";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { useSlugs } from "@/contexts/Sdk";
import { useLogsEnabledErrorCheck } from "@/hooks/useLogsEnabled";
import { useObservabilityMcpConfig } from "@/hooks/useObservabilityMcpConfig";
import { useServerNameMappings } from "@/hooks/useServerNameMappings";
import { cn } from "@/lib/utils";
import { useOrgRoutes } from "@/routes";
import {
  getPresetRange,
  TimeRangePicker,
  type DateRangePreset,
} from "@gram-ai/elements";
import { telemetryListHooksTraces } from "@gram/client/funcs/telemetryListHooksTraces";
import type {
  HookTraceSummary as HookTrace,
  LogFilter,
  TelemetryLogRecord,
  TypesToInclude,
} from "@gram/client/models/components";
import { useGramContext } from "@gram/client/react-query";
import { unwrapAsync } from "@gram/client/types/fp";
import { Icon } from "@speakeasy-api/moonshine";
import { useInfiniteQuery } from "@tanstack/react-query";
import {
  BarElement,
  BarController,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Filler,
  Tooltip,
  Legend,
  Chart as ChartJS,
} from "chart.js";
import { Settings } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link, useSearchParams } from "react-router";
import { LogDetailSheet } from "@/pages/logs/LogDetailSheet";
import { TraceLogsList } from "@/pages/logs/TraceLogsList";
import { HooksEmptyState } from "@/pages/hooks/HooksEmptyState";
import { HookSourceIcon } from "@/pages/hooks/HookSourceIcon";
import { HooksSetupButton } from "@/pages/hooks/HooksSetupDialog";

ChartJS.register(
  CategoryScale,
  LinearScale,
  BarElement,
  BarController,
  PointElement,
  LineElement,
  Filler,
  Tooltip,
  Legend,
);

const validPresets: DateRangePreset[] = [
  "15m",
  "1h",
  "4h",
  "1d",
  "2d",
  "3d",
  "7d",
  "15d",
  "30d",
  "90d",
];

function isValidPreset(value: string | null): value is DateRangePreset {
  return value !== null && validPresets.includes(value as DateRangePreset);
}

interface FilterChip {
  display: string;
  filters: string[];
  path: string;
}

function safeBase64Encode(str: string): string {
  try {
    return btoa(str);
  } catch {
    return btoa(encodeURIComponent(str));
  }
}

function safeBase64Decode(str: string): string | null {
  try {
    const decoded = atob(str);
    try {
      return decodeURIComponent(decoded);
    } catch {
      return decoded;
    }
  } catch {
    return null;
  }
}

const perPage = 100;

export function LogsTools() {
  const [searchParams, setSearchParams] = useSearchParams();
  const { projectSlug } = useSlugs();

  const mcpConfig = useObservabilityMcpConfig({
    toolsToInclude: ({ toolName }) =>
      toolName.includes("logs") || toolName.includes("hooks"),
  });

  const serverNameMappings = useServerNameMappings();

  const initialServer = searchParams.get("server");
  const initialUserEmail = searchParams.get("user");

  const initialHookTypes = searchParams.get("hookTypes");
  const defaultHookTypes: TypesToInclude[] = ["mcp", "skill"];
  const parsedHookTypes: TypesToInclude[] = initialHookTypes
    ? (initialHookTypes
        .split(",")
        .filter((t) =>
          ["mcp", "local", "skill"].includes(t),
        ) as TypesToInclude[])
    : defaultHookTypes;

  const [activeFilters, setActiveFilters] = useState<FilterChip[]>(() => {
    const filters: FilterChip[] = [];

    if (initialServer) {
      const serverValues = initialServer
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean);
      serverValues.forEach((value) => {
        filters.push({
          display: value,
          filters: [value],
          path: "gram.tool_call.source",
        });
      });
    }

    if (initialUserEmail) {
      const userValues = initialUserEmail
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean);
      userValues.forEach((value) => {
        filters.push({
          display: value,
          filters: [value],
          path: "user.email",
        });
      });
    }

    return filters;
  });

  const [serverInput, setServerInput] = useState("");
  const [userEmailInput, setUserEmailInput] = useState("");
  const [selectedHookTypes, setSelectedHookTypes] =
    useState<TypesToInclude[]>(parsedHookTypes);
  const [expandedTraceId, setExpandedTraceId] = useState<string | null>(null);
  const [selectedLog, setSelectedLog] = useState<TelemetryLogRecord | null>(
    null,
  );
  const containerRef = useRef<HTMLDivElement>(null);

  const client = useGramContext();

  const urlRange = searchParams.get("range");
  const urlFrom = searchParams.get("from");
  const urlTo = searchParams.get("to");
  const urlLabelEncoded = searchParams.get("label");
  const urlLabel = useMemo(() => {
    if (!urlLabelEncoded) return null;
    return safeBase64Decode(urlLabelEncoded);
  }, [urlLabelEncoded]);

  const dateRange: DateRangePreset = isValidPreset(urlRange) ? urlRange : "7d";

  const customRange = useMemo(() => {
    if (urlFrom && urlTo) {
      const from = new Date(urlFrom);
      const to = new Date(urlTo);
      if (!isNaN(from.getTime()) && !isNaN(to.getTime())) {
        return { from, to };
      }
    }
    return null;
  }, [urlFrom, urlTo]);

  const updateSearchParams = useCallback(
    (updates: Record<string, string | null>) => {
      setSearchParams((prev) => {
        const next = new URLSearchParams(prev);
        for (const [key, value] of Object.entries(updates)) {
          if (value === null) {
            next.delete(key);
          } else {
            next.set(key, value);
          }
        }
        return next;
      });
    },
    [setSearchParams],
  );

  const setDateRangeParam = useCallback(
    (preset: DateRangePreset) => {
      updateSearchParams({ range: preset, from: null, to: null, label: null });
    },
    [updateSearchParams],
  );

  const setCustomRangeParam = useCallback(
    (from: Date, to: Date, label?: string) => {
      updateSearchParams({
        range: null,
        from: from.toISOString(),
        to: to.toISOString(),
        label: label ? safeBase64Encode(label) : null,
      });
    },
    [updateSearchParams],
  );

  const clearCustomRange = useCallback(() => {
    updateSearchParams({ from: null, to: null, label: null });
  }, [updateSearchParams]);

  const { from, to } = useMemo(
    () => customRange ?? getPresetRange(dateRange),
    [customRange, dateRange],
  );

  const logFilters = useMemo(() => {
    const filters: LogFilter[] = [];
    for (const chip of activeFilters) {
      filters.push({
        path: chip.path,
        operator: chip.filters.length > 1 ? "in" : "contains",
        values: chip.filters,
      });
    }
    return filters.length > 0 ? filters : undefined;
  }, [activeFilters]);

  const {
    data: tracesData,
    error,
    fetchNextPage,
    hasNextPage,
    isFetching,
    isFetchingNextPage,
    refetch: refetchLogs,
    isLogsDisabled: isLogsLogsDisabled,
  } = useLogsEnabledErrorCheck(
    useInfiniteQuery({
      queryKey: [
        "hooks-traces",
        activeFilters,
        selectedHookTypes,
        from.toISOString(),
        to.toISOString(),
      ],
      queryFn: ({ pageParam }) =>
        unwrapAsync(
          telemetryListHooksTraces(client, {
            listHooksTracesPayload: {
              from,
              to,
              filters: logFilters,
              typesToInclude:
                selectedHookTypes.length > 0 ? selectedHookTypes : undefined,
              cursor: pageParam,
              limit: perPage,
              sort: "desc",
            },
          }),
        ),
      initialPageParam: undefined as string | undefined,
      getNextPageParam: (lastPage) => lastPage.nextCursor ?? undefined,
      throwOnError: false,
    }),
  );

  const groupedTraces = useMemo(() => {
    return tracesData?.pages.flatMap((page) => page.traces) ?? [];
  }, [tracesData]);

  const addFilter = useCallback(
    (chip: FilterChip) => {
      setActiveFilters((prev) => {
        const exists = prev.some(
          (f) => f.path === chip.path && f.display === chip.display,
        );
        if (exists) return prev;

        const newFilters = [...prev, chip];

        setSearchParams(
          (urlPrev) => {
            const next = new URLSearchParams(urlPrev);
            if (chip.path === "gram.tool_call.source") {
              const serverFilters = newFilters
                .filter((f) => f.path === "gram.tool_call.source")
                .map((f) => f.display);
              next.set("server", serverFilters.join(","));
            } else if (chip.path === "user.email") {
              const userFilters = newFilters
                .filter((f) => f.path === "user.email")
                .map((f) => f.display);
              next.set("user", userFilters.join(","));
            }
            return next;
          },
          { replace: true },
        );

        return newFilters;
      });
    },
    [setSearchParams],
  );

  const removeFilter = useCallback(
    (path: string, display?: string) => {
      setActiveFilters((prev) => {
        const newFilters = display
          ? prev.filter((f) => !(f.path === path && f.display === display))
          : prev.filter((f) => f.path !== path);

        setSearchParams(
          (urlPrev) => {
            const next = new URLSearchParams(urlPrev);
            if (path === "gram.tool_call.source") {
              const serverFilters = newFilters
                .filter((f) => f.path === "gram.tool_call.source")
                .map((f) => f.display);
              if (serverFilters.length > 0) {
                next.set("server", serverFilters.join(","));
              } else {
                next.delete("server");
              }
            } else if (path === "user.email") {
              const userFilters = newFilters
                .filter((f) => f.path === "user.email")
                .map((f) => f.display);
              if (userFilters.length > 0) {
                next.set("user", userFilters.join(","));
              } else {
                next.delete("user");
              }
            }
            return next;
          },
          { replace: true },
        );

        return newFilters;
      });
    },
    [setSearchParams],
  );

  useEffect(() => {
    if (!serverInput.trim()) return;

    const timeoutId = setTimeout(() => {
      addFilter({
        display: serverInput,
        filters: [serverInput],
        path: "gram.tool_call.source",
      });
      setServerInput("");
    }, 500);
    return () => clearTimeout(timeoutId);
  }, [serverInput, addFilter]);

  useEffect(() => {
    if (!userEmailInput.trim()) return;

    const timeoutId = setTimeout(() => {
      addFilter({
        display: userEmailInput,
        filters: [userEmailInput],
        path: "user.email",
      });
      setUserEmailInput("");
    }, 500);
    return () => clearTimeout(timeoutId);
  }, [userEmailInput, addFilter]);

  const handleScroll = (e: React.UIEvent<HTMLDivElement>) => {
    const container = e.currentTarget;
    const scrollTop = container.scrollTop;
    const scrollHeight = container.scrollHeight;
    const clientHeight = container.clientHeight;
    const distanceFromBottom = scrollHeight - (scrollTop + clientHeight);

    if (isFetchingNextPage || isFetching) return;
    if (!hasNextPage) return;

    if (distanceFromBottom < 200) {
      fetchNextPage();
    }
  };

  const handleLogClick = (log: TelemetryLogRecord) => {
    setSelectedLog(log);
  };

  const toggleExpand = (traceId: string) => {
    setExpandedTraceId((prev) => (prev === traceId ? null : traceId));
  };

  const handleHookTypesChange = useCallback(
    (types: TypesToInclude[]) => {
      setSelectedHookTypes(types);
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          const isDefault =
            types.length === 2 &&
            types.includes("mcp") &&
            types.includes("skill") &&
            !types.includes("local");
          if (isDefault) {
            next.delete("hookTypes");
          } else if (types.length > 0) {
            next.set("hookTypes", types.join(","));
          } else {
            next.set("hookTypes", "");
          }
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  const refetch = useCallback(() => {
    refetchLogs();
  }, [refetchLogs]);

  const isLogsDisabled = isLogsLogsDisabled;
  const isLoading = isFetching && groupedTraces.length === 0;

  return (
    <>
      <InsightsConfig
        mcpConfig={mcpConfig}
        title="Explore Tool Logs"
        subtitle="Ask me about your tool logs! Powered by Elements + Gram MCP"
        hideTrigger={isLogsDisabled}
      />
      {isLogsDisabled ? (
        <div className="min-h-0 w-full flex-1 space-y-6 overflow-y-auto p-8 pb-24">
          <div className="flex min-w-0 flex-col gap-1">
            <h1 className="text-xl font-semibold">Tool Logs</h1>
            <p className="text-muted-foreground text-sm">
              Monitor tool traces across all servers
            </p>
          </div>
          <div className="relative flex-1">
            <div
              className="pointer-events-none h-full select-none"
              aria-hidden="true"
            >
              <ObservabilitySkeleton />
            </div>
            <EnableLoggingOverlay onEnabled={refetch} />
          </div>
        </div>
      ) : (
        <EnterpriseGate
          icon="workflow"
          description="Tools are available on the Enterprise plan. Book a time to get started."
        >
          <HooksInnerContent
            isLogsDisabled={isLogsDisabled}
            isLoading={isLoading}
            isFetching={isFetching}
            error={error}
            groupedTraces={groupedTraces}
            serverInput={serverInput}
            setServerInput={setServerInput}
            userEmailInput={userEmailInput}
            setUserEmailInput={setUserEmailInput}
            activeFilters={activeFilters}
            addFilter={addFilter}
            removeFilter={removeFilter}
            selectedHookTypes={selectedHookTypes}
            onHookTypesChange={handleHookTypesChange}
            expandedTraceId={expandedTraceId}
            toggleExpand={toggleExpand}
            selectedLog={selectedLog}
            handleLogClick={handleLogClick}
            setSelectedLog={setSelectedLog}
            containerRef={containerRef}
            handleScroll={handleScroll}
            hasNextPage={hasNextPage}
            isFetchingNextPage={isFetchingNextPage}
            dateRange={dateRange}
            customRange={customRange}
            customRangeLabel={urlLabel}
            onDateRangeChange={setDateRangeParam}
            onCustomRangeChange={setCustomRangeParam}
            onClearCustomRange={clearCustomRange}
            projectSlug={projectSlug}
            serverNameMappings={serverNameMappings}
          />
        </EnterpriseGate>
      )}
    </>
  );
}

function HooksInnerContent({
  isLoading,
  isFetching,
  error,
  groupedTraces,
  serverInput,
  setServerInput,
  userEmailInput,
  setUserEmailInput,
  activeFilters,
  removeFilter,
  selectedHookTypes,
  onHookTypesChange,
  expandedTraceId,
  toggleExpand,
  selectedLog,
  handleLogClick,
  setSelectedLog,
  containerRef,
  handleScroll,
  hasNextPage,
  isFetchingNextPage,
  dateRange,
  customRange,
  customRangeLabel,
  onDateRangeChange,
  onCustomRangeChange,
  onClearCustomRange,
  projectSlug,
  serverNameMappings,
}: {
  isLogsDisabled: boolean;
  isLoading: boolean;
  isFetching: boolean;
  error: Error | null;
  groupedTraces: HookTrace[];
  serverInput: string;
  setServerInput: (value: string) => void;
  userEmailInput: string;
  setUserEmailInput: (value: string) => void;
  activeFilters: FilterChip[];
  addFilter: (chip: FilterChip) => void;
  removeFilter: (path: string, display?: string) => void;
  selectedHookTypes: TypesToInclude[];
  onHookTypesChange: (types: TypesToInclude[]) => void;
  expandedTraceId: string | null;
  toggleExpand: (traceId: string) => void;
  selectedLog: TelemetryLogRecord | null;
  handleLogClick: (log: TelemetryLogRecord) => void;
  setSelectedLog: (log: TelemetryLogRecord | null) => void;
  containerRef: React.RefObject<HTMLDivElement | null>;
  handleScroll: (e: React.UIEvent<HTMLDivElement>) => void;
  hasNextPage: boolean;
  isFetchingNextPage: boolean;
  dateRange: DateRangePreset;
  customRange: { from: Date; to: Date } | null;
  customRangeLabel: string | null;
  onDateRangeChange: (preset: DateRangePreset) => void;
  onCustomRangeChange: (from: Date, to: Date, label?: string) => void;
  onClearCustomRange: () => void;
  projectSlug?: string;
  serverNameMappings: ReturnType<typeof useServerNameMappings>;
}) {
  const orgRoutes = useOrgRoutes();
  return (
    <>
      <div className="flex min-h-0 w-full flex-1 flex-col">
        <div className="flex min-h-0 flex-1 flex-col gap-6 px-8 pt-8">
          <div className="flex shrink-0 items-start justify-between gap-4">
            <div className="flex min-w-0 flex-col gap-1">
              <h1 className="text-xl font-semibold">Tool Logs</h1>
              <p className="text-muted-foreground text-sm">
                Monitor tool traces across all servers
              </p>
            </div>
            <div className="flex items-center gap-2">
              <HooksSetupButton />
              <Button variant="outline" size="sm" asChild>
                <Link to={orgRoutes.logs.href()}>
                  <Settings className="h-4 w-4" />
                  Configure settings
                </Link>
              </Button>
            </div>
          </div>

          <div className="flex shrink-0 flex-wrap items-center gap-2">
            <MultiSearch
              value={serverInput}
              onChange={setServerInput}
              placeholder="Filter by server name"
              className="min-w-[200px] flex-1"
              chips={activeFilters
                .filter((f) => f.path === "gram.tool_call.source")
                .map((f) => ({ display: f.display, value: f.display }))}
              onRemoveChip={(display) =>
                removeFilter("gram.tool_call.source", display)
              }
            />
            <MultiSearch
              value={userEmailInput}
              onChange={setUserEmailInput}
              placeholder="Filter by user email"
              className="min-w-[200px] flex-1"
              chips={activeFilters
                .filter((f) => f.path === "user.email")
                .map((f) => ({ display: f.display, value: f.display }))}
              onRemoveChip={(display) => removeFilter("user.email", display)}
            />
            <HookTypeFilter
              selectedHookTypes={selectedHookTypes}
              onHookTypesChange={onHookTypesChange}
            />
            <div className="ml-auto">
              <TimeRangePicker
                preset={customRange ? null : dateRange}
                customRange={customRange}
                customRangeLabel={customRangeLabel}
                onPresetChange={onDateRangeChange}
                onCustomRangeChange={onCustomRangeChange}
                onClearCustomRange={onClearCustomRange}
                projectSlug={projectSlug}
              />
            </div>
          </div>

          <div className="flex min-h-0 flex-1 overflow-hidden">
            <div className="min-h-0 flex-1 overflow-y-auto border">
              <div className="bg-background flex h-full flex-col">
                {isFetching && groupedTraces.length > 0 && (
                  <div className="bg-primary/20 absolute top-0 right-0 left-0 z-20 h-1">
                    <div className="bg-primary h-full animate-pulse" />
                  </div>
                )}

                <div className="bg-muted/30 text-muted-foreground flex shrink-0 items-center gap-3 border-b px-5 py-2.5 text-xs font-medium tracking-wide uppercase">
                  <div className="w-[150px] shrink-0">Timestamp</div>
                  <div className="w-5 shrink-0" />
                  <div className="min-w-0 flex-1">Server / Tool</div>
                  <div className="w-[260px] shrink-0">User</div>
                  <div className="w-[120px] shrink-0">Source</div>
                  <div className="w-24 shrink-0 text-right">Status</div>
                </div>

                <div
                  ref={containerRef}
                  className="flex-1 overflow-y-auto"
                  onScroll={handleScroll}
                >
                  <LogsToolsContent
                    error={error}
                    isLoading={isLoading}
                    groupedTraces={groupedTraces}
                    activeFilters={activeFilters}
                    expandedTraceId={expandedTraceId}
                    isFetchingNextPage={isFetchingNextPage}
                    onToggleExpand={toggleExpand}
                    onLogClick={handleLogClick}
                    serverNameMappings={serverNameMappings}
                  />
                </div>

                {groupedTraces.length > 0 && (
                  <div className="bg-muted/30 text-muted-foreground flex shrink-0 items-center gap-4 border-t px-5 py-3 text-sm">
                    <span>
                      {groupedTraces.length}{" "}
                      {groupedTraces.length === 1 ? "trace" : "traces"}
                      {hasNextPage && " • Scroll to load more"}
                    </span>
                  </div>
                )}
              </div>
            </div>
          </div>
        </div>
      </div>

      <LogDetailSheet
        log={selectedLog}
        open={!!selectedLog}
        onOpenChange={(open) => !open && setSelectedLog(null)}
      />
    </>
  );
}

const HOOK_TYPE_OPTIONS = [
  {
    label: "MCP Servers",
    labelShort: "Servers",
    value: "mcp" as TypesToInclude,
  },
  {
    label: "Local Tools",
    labelShort: "Local",
    value: "local" as TypesToInclude,
  },
  { label: "Skills", labelShort: "Skills", value: "skill" as TypesToInclude },
];

function HookTypeFilter({
  selectedHookTypes,
  onHookTypesChange,
}: {
  selectedHookTypes: TypesToInclude[];
  onHookTypesChange: (types: TypesToInclude[]) => void;
}) {
  const getButtonText = () => {
    if (selectedHookTypes.length === 3) {
      return "Showing all types";
    }

    if (selectedHookTypes.length === 0) {
      return "No types selected";
    }

    if (selectedHookTypes.length === 1) {
      const selected = HOOK_TYPE_OPTIONS.find(
        (opt) => opt.value === selectedHookTypes[0],
      );
      return `Showing ${selected?.labelShort || selectedHookTypes[0]}`;
    }

    const labels = HOOK_TYPE_OPTIONS.filter((opt) =>
      selectedHookTypes.includes(opt.value),
    ).map((opt) => opt.labelShort);
    return `Showing ${labels.join(" & ")}`;
  };

  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          size="sm"
          className="h-[42px] w-[200px] shrink-0 justify-between"
        >
          <span className="text-sm">{getButtonText()}</span>
          <Icon name="chevron-down" className="ml-2 size-4" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-[200px] p-3" align="start">
        <div className="space-y-2">
          {HOOK_TYPE_OPTIONS.map((option) => (
            <div key={option.value} className="flex items-center space-x-2">
              <Checkbox
                id={`hook-type-${option.value}`}
                checked={selectedHookTypes.includes(option.value)}
                onCheckedChange={(checked) => {
                  if (checked) {
                    onHookTypesChange([...selectedHookTypes, option.value]);
                  } else {
                    onHookTypesChange(
                      selectedHookTypes.filter((t) => t !== option.value),
                    );
                  }
                }}
              />
              <label
                htmlFor={`hook-type-${option.value}`}
                className="cursor-pointer text-sm leading-none font-medium"
              >
                {option.label}
              </label>
            </div>
          ))}
        </div>
      </PopoverContent>
    </Popover>
  );
}

export function LogsToolsContent({
  error,
  isLoading,
  groupedTraces,
  activeFilters,
  expandedTraceId,
  isFetchingNextPage,
  onToggleExpand,
  onLogClick,
  serverNameMappings,
}: {
  error: Error | null;
  isLoading: boolean;
  groupedTraces: HookTrace[];
  activeFilters: FilterChip[];
  expandedTraceId: string | null;
  isFetchingNextPage: boolean;
  onToggleExpand: (traceId: string) => void;
  onLogClick: (log: TelemetryLogRecord) => void;
  serverNameMappings: ReturnType<typeof useServerNameMappings>;
}) {
  if (error) {
    return (
      <ErrorAlert
        error={error}
        title="Error loading hook events"
        className="m-4"
      />
    );
  }

  if (isLoading) {
    return (
      <div className="text-muted-foreground flex items-center justify-center gap-2 py-12">
        <Spinner className="mr-0 size-5" />
        <span>Loading hook events...</span>
      </div>
    );
  }

  if (groupedTraces.length === 0) {
    const hasFilters = activeFilters.length > 0;

    if (!hasFilters) {
      return <HooksEmptyState />;
    }

    return (
      <div className="py-12 text-center">
        <div className="flex flex-col items-center gap-3">
          <div className="bg-muted flex size-12 items-center justify-center rounded-full">
            <Icon name="inbox" className="text-muted-foreground size-6" />
          </div>
          <span className="text-foreground font-medium">
            No matching hook events
          </span>
          <span className="text-muted-foreground max-w-sm text-sm">
            Try adjusting your search query or time range
          </span>
        </div>
      </div>
    );
  }

  return (
    <>
      {groupedTraces.map((trace) => (
        <LogsToolsTraceRow
          key={trace.traceId}
          trace={trace}
          isExpanded={expandedTraceId === trace.traceId}
          onToggle={() => onToggleExpand(trace.traceId)}
          onLogClick={onLogClick}
          serverNameMappings={serverNameMappings}
        />
      ))}

      {isFetchingNextPage && (
        <div className="text-muted-foreground flex items-center justify-center gap-2 border-t py-4">
          <Icon name="loader-circle" className="size-4 animate-spin" />
          <span className="text-sm">Loading more events...</span>
        </div>
      )}
    </>
  );
}

function LogsToolsTraceRow({
  trace,
  isExpanded,
  onToggle,
  onLogClick,
  serverNameMappings,
}: {
  trace: HookTrace;
  isExpanded: boolean;
  onToggle: () => void;
  onLogClick: (log: TelemetryLogRecord) => void;
  serverNameMappings: ReturnType<typeof useServerNameMappings>;
}) {
  const [editDialogOpen, setEditDialogOpen] = useState(false);
  const timestamp = new Date(
    Number(BigInt(trace.startTimeUnixNano) / 1_000_000n),
  );
  const now = new Date();
  const diff = now.getTime() - timestamp.getTime();
  const seconds = Math.floor(diff / 1000);
  const minutes = Math.floor(seconds / 60);
  const hours = Math.floor(minutes / 60);
  const days = Math.floor(hours / 24);
  const timeAgo =
    days > 0
      ? `${days}d ago`
      : hours > 0
        ? `${hours}h ago`
        : minutes > 0
          ? `${minutes}m ago`
          : `${seconds}s ago`;

  const serverName = trace.toolSource;
  const toolName = trace.toolName;
  const skillName = trace.skillName;
  const userEmail = trace.userEmail;
  const hookSource = trace.hookSource;

  const displayServerName = useMemo(() => {
    if (!serverName) return serverNameMappings.rawToDisplay.get("") ?? null;
    return serverNameMappings.rawToDisplay.get(serverName) ?? serverName;
  }, [serverName, serverNameMappings.rawToDisplay]);

  const editDialogProps = useMemo(() => {
    if (!serverName) return null;
    const overrides =
      serverNameMappings.displayToOverrides.get(displayServerName ?? "") ?? [];
    const hasOverride = overrides.some((o) => o.rawServerName === serverName);
    return {
      serverName: displayServerName ?? serverName,
      groupedOverrides: overrides,
      unmappedRawName: hasOverride ? null : serverName,
    };
  }, [serverName, displayServerName, serverNameMappings.displayToOverrides]);

  const serverNameBadge = useMemo(() => {
    if (toolName === "Skill" && skillName) {
      return (
        <span className="shrink-0 truncate rounded-xs bg-purple-500/10 px-2 py-1 font-mono text-xs font-medium text-purple-600 dark:text-purple-400">
          Skill
        </span>
      );
    }

    const isLocal = !serverName;
    return (
      <span
        className={cn(
          "shrink-0 truncate rounded-xs px-2 py-1 font-mono text-xs",
          isLocal
            ? "bg-muted/50 text-muted-foreground"
            : "bg-primary/10 text-primary font-medium",
        )}
      >
        {displayServerName || "local"}
      </span>
    );
  }, [displayServerName, serverName, toolName, skillName]);

  const statusConfig = useMemo(() => {
    if (trace.hookStatus === "blocked") {
      return {
        color: "text-amber-600 dark:text-amber-400",
        bgColor: "bg-amber-500/10",
        label: "Blocked",
        icon: "shield-alert" as const,
      };
    } else if (trace.hookStatus === "failure") {
      return {
        color: "text-destructive",
        bgColor: "bg-destructive/10",
        label: "Failure",
        icon: null,
      };
    } else if (trace.hookStatus === "success") {
      return {
        color: "text-emerald-500",
        bgColor: "bg-emerald-500/10",
        label: "Success",
        icon: null,
      };
    }
    return {
      color: "text-muted-foreground",
      bgColor: "bg-muted",
      label: "Pending",
      icon: null,
    };
  }, [trace.hookStatus]);

  return (
    <div className="border-border/50 border-b last:border-b-0">
      <div
        role="button"
        tabIndex={0}
        onClick={onToggle}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") onToggle();
        }}
        className="hover:bg-muted/50 flex w-full cursor-pointer items-center gap-3 px-5 py-2.5 text-left transition-colors"
      >
        <div className="text-muted-foreground w-[150px] shrink-0 font-mono text-sm">
          {timeAgo}
        </div>

        <div className="flex w-5 shrink-0 items-center justify-center">
          <Icon
            name={isExpanded ? "chevron-down" : "chevron-right"}
            className="text-muted-foreground size-4"
          />
        </div>

        <div className="flex min-w-0 flex-1 items-center gap-2">
          <div className="group/server relative flex shrink-0 items-center">
            {serverNameBadge}
            {serverName && (
              <button
                type="button"
                onClick={(e) => {
                  e.stopPropagation();
                  setEditDialogOpen(true);
                }}
                className="text-muted-foreground hover:text-foreground bg-card hover:bg-muted border-border invisible absolute -right-6 size-6 rounded border p-1 shadow-sm transition-colors group-hover/server:visible"
                aria-label="Edit display name"
              >
                <Icon name="pencil" className="size-3" />
              </button>
            )}
          </div>
          <span className="truncate font-mono text-sm">
            {toolName === "Skill" && skillName
              ? skillName
              : toolName || "unknown"}
          </span>
        </div>

        <div className="text-muted-foreground w-[260px] shrink-0 truncate text-sm">
          {userEmail || "—"}
        </div>

        <div className="flex w-[120px] shrink-0 items-center gap-2">
          <HookSourceIcon source={hookSource} className="size-4 shrink-0" />
          {hookSource && (
            <span className="text-foreground truncate text-xs font-medium">
              {hookSource}
            </span>
          )}
        </div>

        <div className="flex w-24 shrink-0 justify-end">
          <div
            className={cn(
              "inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-xs font-medium",
              statusConfig.bgColor,
              statusConfig.color,
            )}
          >
            {statusConfig.icon ? (
              <Icon name={statusConfig.icon} className="size-3" />
            ) : (
              <div
                className={cn(
                  "size-1.5 rounded-full",
                  statusConfig.color === "text-muted-foreground"
                    ? "bg-muted-foreground"
                    : "bg-current",
                )}
              />
            )}
            {statusConfig.label}
          </div>
        </div>
      </div>

      {isExpanded && (
        <>
          {trace.hookStatus === "blocked" && (
            <div className="flex items-start gap-3 border-y border-amber-500/30 bg-amber-500/10 px-5 py-3">
              <Icon
                name="shield-alert"
                className="mt-0.5 size-4 shrink-0 text-amber-600 dark:text-amber-400"
              />
              <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                <div className="text-xs font-semibold tracking-wide text-amber-700 uppercase dark:text-amber-300">
                  Blocked
                </div>
                <div className="text-foreground wrap-break-words text-sm">
                  {trace.blockReason || "No reason provided"}
                </div>
              </div>
            </div>
          )}
          <TraceLogsList
            traceId={trace.traceId}
            toolName={toolName || "unknown"}
            isExpanded={isExpanded}
            onLogClick={onLogClick}
            parentTimestamp={trace.startTimeUnixNano}
          />
        </>
      )}

      {editDialogProps && (
        <EditServerNameDialog
          open={editDialogOpen}
          onOpenChange={setEditDialogOpen}
          serverName={editDialogProps.serverName}
          groupedOverrides={editDialogProps.groupedOverrides}
          unmappedRawName={editDialogProps.unmappedRawName}
          upsert={serverNameMappings.upsert}
          remove={serverNameMappings.remove}
          isUpserting={serverNameMappings.isUpserting}
          isDeleting={serverNameMappings.isDeleting}
        />
      )}
    </div>
  );
}
