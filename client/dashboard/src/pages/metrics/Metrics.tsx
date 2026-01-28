import { Page } from "@/components/page-layout";
import {
  DateRangeSelect,
  DateRangePreset,
  getDateRange,
} from "./date-range-select";
import { Skeleton } from "@/components/ui/skeleton";
import { useSession } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import { getServerURL } from "@/lib/utils";
import { Chat, ElementsConfig, GramElementsProvider } from "@gram-ai/elements";
import { chatSessionsCreate } from "@gram/client/funcs/chatSessionsCreate";
import { telemetryGetProjectMetricsSummary } from "@gram/client/funcs/telemetryGetProjectMetricsSummary";
import { useGramContext, useListToolsets } from "@gram/client/react-query";
import { unwrapAsync } from "@gram/client/types/fp";
import { Icon, useMoonshineConfig } from "@speakeasy-api/moonshine";
import { useQuery } from "@tanstack/react-query";
import { XIcon } from "lucide-react";
import { useCallback, useMemo, useState } from "react";
import { MetricsCards } from "./MetricsCards";
import { MetricsCharts } from "./MetricsCharts";

export default function MetricsPage() {
  const [dateRange, setDateRange] = useState<DateRangePreset>("7d");
  const { theme } = useMoonshineConfig();
  const gramMcpConfig = useGramMcpConfig();

  const client = useGramContext();

  const { data, isPending, error } = useQuery({
    queryKey: [
      "@gram/client",
      "telemetry",
      "getProjectMetricsSummary",
      dateRange,
    ],
    queryFn: () => {
      const { from, to } = getDateRange(dateRange);
      return unwrapAsync(
        telemetryGetProjectMetricsSummary(client, {
          getProjectMetricsSummaryPayload: { from, to },
        }),
      );
    },
  });

  const metricsElementsConfig = useMemo<ElementsConfig>(
    () => ({
      ...gramMcpConfig,
      variant: "standalone",
      welcome: {
        title: "Explore Metrics",
        subtitle:
          "Ask me about your usage metrics! Powered by Elements + Gram MCP",
        suggestions: [
          {
            title: "Token Usage",
            label: "Summarize token usage",
            prompt: "Summarize my token usage over the selected period",
          },
          {
            title: "Tool Performance",
            label: "Analyze tool calls",
            prompt: "Which tools have the highest failure rate?",
          },
        ],
      },
      theme: {
        colorScheme: theme === "dark" ? "dark" : "light",
      },
    }),
    [gramMcpConfig, theme],
  );

  const metrics = data?.metrics;
  const isEnabled = data?.enabled ?? true;

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs fullWidth />
      </Page.Header>
      <Page.Body fullWidth fullHeight className="!p-0">
        <div className="flex flex-row h-full w-full">
          {/* Metrics Content */}
          <div className="flex flex-col gap-6 w-1/2 min-w-0 p-8 overflow-y-auto">
            {/* Header */}
            <div className="flex flex-col gap-1">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <h1 className="text-xl font-semibold">Project Metrics</h1>
                  <span className="text-[10px] font-semibold uppercase tracking-wider px-1.5 py-0.5 rounded bg-amber-500/15 text-amber-500">
                    Experimental
                  </span>
                </div>
                <DateRangeSelect
                  value={dateRange}
                  onValueChange={setDateRange}
                />
              </div>
              <p className="text-sm text-muted-foreground">
                Project-level observability metrics
              </p>
            </div>

            {isPending ? (
              <MetricsLoadingSkeleton />
            ) : error ? (
              <MetricsError error={error} />
            ) : !isEnabled ? (
              <MetricsDisabledState />
            ) : metrics ? (
              <>
                <MetricsCards metrics={metrics} />
                <MetricsCharts metrics={metrics} />
              </>
            ) : null}
          </div>

          {/* Chat Panel */}
          <div className="w-1/2 border-l border-border p-8">
            <GramElementsProvider config={metricsElementsConfig}>
              <Chat />
            </GramElementsProvider>
          </div>
        </div>
      </Page.Body>
    </Page>
  );
}

function MetricsLoadingSkeleton() {
  return (
    <div className="flex flex-col gap-6">
      {/* KPI Cards skeleton */}
      <div className="grid grid-cols-4 gap-3">
        {[1, 2, 3, 4].map((i) => (
          <div key={i} className="p-4 rounded-xl border border-border bg-card">
            <Skeleton className="h-9 w-9 rounded-lg mb-3" />
            <Skeleton className="h-3 w-16 mb-2" />
            <Skeleton className="h-7 w-20 mb-1" />
            <Skeleton className="h-3 w-24" />
          </div>
        ))}
      </div>
      {/* Charts skeleton */}
      <div className="flex flex-col gap-4">
        {[1, 2].map((i) => (
          <div
            key={i}
            className="rounded-xl border border-border bg-card overflow-hidden"
          >
            <div className="px-4 py-3 border-b border-border bg-muted/30">
              <Skeleton className="h-4 w-32" />
            </div>
            <div className="p-4">
              <Skeleton className="h-[160px] w-full" />
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

function MetricsError({ error }: { error: Error }) {
  return (
    <div className="flex flex-col items-center gap-2 py-12">
      <XIcon className="size-6 stroke-destructive-default" />
      <span className="text-destructive-default font-medium">
        Error loading metrics
      </span>
      <span className="text-sm text-muted-foreground">{error.message}</span>
    </div>
  );
}

function MetricsDisabledState() {
  return (
    <div className="flex flex-col items-center gap-3 py-12">
      <Icon
        name="chart-no-axes-combined"
        className="size-8 text-muted-foreground"
      />
      <span className="text-muted-foreground">
        Metrics are disabled for your organization.
      </span>
    </div>
  );
}

const useGramMcpConfig = () => {
  const { projectSlug } = useSlugs();
  const client = useGramContext();
  const isLocal = process.env.NODE_ENV === "development";
  const { session } = useSession();

  const { data: toolsets } = useListToolsets(
    {
      gramProject: "kitchen-sink",
    },
    undefined,
    {
      enabled: isLocal,
      headers: {
        "gram-project": "kitchen-sink",
      },
    },
  );

  const getSession = useCallback(async (): Promise<string> => {
    const res = await chatSessionsCreate(
      client,
      {
        createRequestBody: {
          embedOrigin: window.location.origin,
        },
      },
      undefined,
      {
        headers: {
          "Gram-Project": projectSlug ?? "",
        },
      },
    );
    return res.value?.clientToken ?? "";
  }, [client, projectSlug]);

  const gramToolset = useMemo(() => {
    return toolsets?.toolsets.find((toolset) => toolset.slug === "gram-seed");
  }, [toolsets]);

  const toolsToInclude = useCallback(
    ({ toolName }: { toolName: string }) => toolName.includes("metrics"),
    [],
  );

  return useMemo(() => {
    const baseConfig: ElementsConfig = {
      projectSlug: "kitchen-sink",
      tools: {
        toolsToInclude,
      },
      api: {
        sessionFn: getSession,
      },
      environment: {
        GRAM_SERVER_URL: getServerURL(),
        GRAM_SESSION_HEADER_GRAM_SESSION: session,
        GRAM_APIKEY_HEADER_GRAM_KEY: "", // This must be set or else the tool call will fail
        GRAM_PROJECT_SLUG_HEADER_GRAM_PROJECT: projectSlug,
      },
    };

    if (isLocal) {
      if (toolsets && !gramToolset) {
        throw new Error("No gram-seed toolset found--have you run mise seed?");
      }

      return {
        ...baseConfig,
        ...(gramToolset && {
          mcp: `${getServerURL()}/mcp/${gramToolset?.mcpSlug}`,
        }),
      };
    }

    const mcpUrl = getServerURL().includes("app.getgram.ai")
      ? "https://app.getgram.ai/mcp/speakeasy-team-gram"
      : "https://dev.getgram.ai/mcp/speakeasy-team-gram";

    return {
      ...baseConfig,
      mcp: mcpUrl,
    };
  }, [toolsToInclude, getSession, session, projectSlug, isLocal, toolsets, gramToolset]);
};
