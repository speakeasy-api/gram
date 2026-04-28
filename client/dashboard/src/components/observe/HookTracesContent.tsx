import { EnableLoggingOverlay } from "@/components/EnableLoggingOverlay";
import { EnterpriseGate } from "@/components/enterprise-gate";
import { ObservabilitySkeleton } from "@/components/ObservabilitySkeleton";
import { Page } from "@/components/page-layout";
import { MultiSearch } from "@/components/ui/multi-search";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { useSlugs } from "@/contexts/Sdk";
import { useLogsEnabledErrorCheck } from "@/hooks/useLogsEnabled";
import { useServerNameMappings } from "@/hooks/useServerNameMappings";
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
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router";
import { LogDetailSheet } from "@/pages/logs/LogDetailSheet";
import { HooksTraceContent } from "@/components/observe/HooksContent";

const perPage = 100;

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
    if (selectedHookTypes.length === 3) return "Showing all types";
    if (selectedHookTypes.length === 0) return "No types selected";
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

export function HookTracesContent() {
  const [searchParams, setSearchParams] = useSearchParams();
  const { projectSlug } = useSlugs();
  const serverNameMappings = useServerNameMappings();
  const client = useGramContext();

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
      initialServer
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean)
        .forEach((value) => {
          filters.push({
            display: value,
            filters: [value],
            path: "gram.tool_call.source",
          });
        });
    }
    if (initialUserEmail) {
      initialUserEmail
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean)
        .forEach((value) => {
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

  const urlRange = searchParams.get("range");
  const urlFrom = searchParams.get("from");
  const urlTo = searchParams.get("to");

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
    (from: Date, to: Date) => {
      updateSearchParams({
        range: null,
        from: from.toISOString(),
        to: to.toISOString(),
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
    isLogsDisabled,
  } = useLogsEnabledErrorCheck(
    useInfiniteQuery({
      queryKey: [
        "hook-traces-content",
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
    const distanceFromBottom =
      container.scrollHeight - (container.scrollTop + container.clientHeight);
    if (isFetchingNextPage || isFetching) return;
    if (!hasNextPage) return;
    if (distanceFromBottom < 200) {
      fetchNextPage();
    }
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

  const isLoading = isFetching && groupedTraces.length === 0;

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs fullWidth />
        </Page.Header>
        {isLogsDisabled ? (
          <Page.Body fullWidth className="space-y-6">
            <div className="flex min-w-0 flex-col gap-1">
              <h1 className="text-xl font-semibold">Hook Traces</h1>
              <p className="text-muted-foreground text-sm">
                Raw hook events and tool executions across all servers
              </p>
            </div>
            <div className="relative flex-1">
              <div
                className="pointer-events-none h-full select-none"
                aria-hidden="true"
              >
                <ObservabilitySkeleton />
              </div>
              <EnableLoggingOverlay onEnabled={() => refetchLogs()} />
            </div>
          </Page.Body>
        ) : (
          <Page.Body fullWidth noPadding overflowHidden className="flex-1">
            <EnterpriseGate
              icon="workflow"
              description="Hooks are available on the Enterprise plan. Book a time to get started."
            >
              <div className="flex min-h-0 w-full flex-1 flex-col">
                <div className="flex min-h-0 flex-1 flex-col gap-6 px-8 pt-8 pb-4">
                  <div className="flex shrink-0 items-start justify-between gap-4">
                    <div className="flex min-w-0 flex-col gap-1">
                      <h1 className="text-xl font-semibold">Hook Traces</h1>
                      <p className="text-muted-foreground text-sm">
                        Raw hook events and tool executions across all servers
                      </p>
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
                      onRemoveChip={(display) =>
                        removeFilter("user.email", display)
                      }
                    />
                    <HookTypeFilter
                      selectedHookTypes={selectedHookTypes}
                      onHookTypesChange={handleHookTypesChange}
                    />
                    <div className="ml-auto">
                      <TimeRangePicker
                        preset={customRange ? null : dateRange}
                        customRange={customRange}
                        onPresetChange={setDateRangeParam}
                        onCustomRangeChange={setCustomRangeParam}
                        onClearCustomRange={clearCustomRange}
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
                          <HooksTraceContent
                            error={error}
                            isLoading={isLoading}
                            groupedTraces={groupedTraces as HookTrace[]}
                            activeFilters={activeFilters}
                            expandedTraceId={expandedTraceId}
                            isFetchingNextPage={isFetchingNextPage}
                            onToggleExpand={(traceId) =>
                              setExpandedTraceId((prev) =>
                                prev === traceId ? null : traceId,
                              )
                            }
                            onLogClick={(log) => setSelectedLog(log)}
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
            </EnterpriseGate>
          </Page.Body>
        )}
      </Page>

      <LogDetailSheet
        log={selectedLog}
        open={!!selectedLog}
        onOpenChange={(open) => !open && setSelectedLog(null)}
      />
    </div>
  );
}
