import { ChartCard } from "@/components/chart/ChartCard";
import { Page } from "@/components/page-layout";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";
import {
  TokensUnderManagement,
  TUMPeriod,
} from "@gram/client/models/components";
import {
  invalidateAllGetTokensUnderManagement,
  useGetTokensUnderManagement,
  useSetBillingMetadataMutation,
} from "@gram/client/react-query";
import { Button, Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import {
  BarElement,
  CategoryScale,
  Chart as ChartJS,
  type ChartDataset,
  type ChartOptions,
  Legend,
  LinearScale,
  LineElement,
  PointElement,
  Tooltip as ChartTooltip,
} from "chart.js";
import { Info } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Chart } from "react-chartjs-2";
import { UsageProgress } from "./usage-controls";

ChartJS.register(
  CategoryScale,
  LinearScale,
  BarElement,
  LineElement,
  PointElement,
  ChartTooltip,
  Legend,
);

const cycleDateFormat = new Intl.DateTimeFormat("en-US", {
  month: "short",
  day: "numeric",
  year: "numeric",
  timeZone: "UTC",
});

const cycleLabelFormat = new Intl.DateTimeFormat("en-US", {
  month: "short",
  day: "numeric",
  timeZone: "UTC",
});

const dayLabelFormat = new Intl.DateTimeFormat("en-US", {
  month: "short",
  day: "numeric",
  timeZone: "UTC",
});

const compactTokens = new Intl.NumberFormat("en-US", {
  notation: "compact",
  maximumFractionDigits: 1,
});

function formatCycleRange(tum: TokensUnderManagement): string {
  return `${cycleDateFormat.format(tum.periodStart)} – ${cycleDateFormat.format(tum.periodEnd)}`;
}

const MS_PER_DAY = 24 * 60 * 60 * 1000;

// Builds a contiguous daily series across all cycles, filling days the API
// omitted (zero usage) so gaps render honestly. Stops at the current UTC day.
function buildDailySeries(history: TUMPeriod[]): {
  labels: string[];
  data: number[];
} {
  const first = history[0];
  const last = history[history.length - 1];
  if (!first || !last) return { labels: [], data: [] };

  const tokensByDate = new Map<string, number>();
  for (const period of history) {
    for (const day of period.days) {
      const key = day.date.toString();
      tokensByDate.set(key, (tokensByDate.get(key) ?? 0) + day.tokens);
    }
  }

  const startMs = first.periodStart.getTime();
  const endMs = Math.min(last.periodEnd.getTime(), Date.now() + MS_PER_DAY);

  const labels: string[] = [];
  const data: number[] = [];
  for (let ms = startMs; ms < endMs; ms += MS_PER_DAY) {
    const date = new Date(ms);
    const key = date.toISOString().slice(0, 10);
    labels.push(dayLabelFormat.format(date));
    data.push(tokensByDate.get(key) ?? 0);
  }

  return { labels, data };
}

type TumGranularity = "day" | "cycle";

function TumHistoryChart({
  history,
  monthlyTokenLimit,
}: {
  history: TUMPeriod[];
  monthlyTokenLimit: number | undefined;
}): JSX.Element {
  const [granularity, setGranularity] = useState<TumGranularity>("day");

  const hasData = history.some((p) => p.tokens > 0);

  const chartData = useMemo<{
    labels: string[];
    datasets: Array<
      ChartDataset<"bar", number[]> | ChartDataset<"line", number[]>
    >;
  }>(() => {
    if (granularity === "day") {
      const { labels, data } = buildDailySeries(history);
      return {
        labels,
        datasets: [
          {
            type: "bar" as const,
            label: "Tokens Under Management",
            data,
            backgroundColor: "rgba(96, 165, 250, 0.5)",
          },
        ],
      };
    }

    const labels = history.map((p) => cycleLabelFormat.format(p.periodStart));
    const datasets: Array<
      ChartDataset<"bar", number[]> | ChartDataset<"line", number[]>
    > = [
      {
        type: "bar" as const,
        label: "Tokens Under Management",
        data: history.map((p) => p.tokens),
        backgroundColor: "rgba(96, 165, 250, 0.5)",
      },
    ];

    if (monthlyTokenLimit != null && monthlyTokenLimit > 0) {
      datasets.push({
        type: "line" as const,
        label: "Contracted Limit",
        data: history.map(() => monthlyTokenLimit),
        borderColor: "#f59e0b",
        borderDash: [6, 4],
        borderWidth: 1.5,
        pointRadius: 0,
      });
    }

    return { labels, datasets };
  }, [granularity, history, monthlyTokenLimit]);

  const chartOptions = useMemo<ChartOptions<"bar">>(
    () => ({
      responsive: true,
      maintainAspectRatio: false,
      plugins: {
        legend: {
          display: granularity === "cycle" && monthlyTokenLimit != null,
        },
        tooltip: {
          callbacks: {
            label: (item) =>
              `${item.dataset.label}: ${Number(item.raw).toLocaleString()} tokens`,
          },
        },
      },
      scales: {
        x: {
          grid: { display: false },
          ticks: { maxTicksLimit: granularity === "day" ? 14 : 12 },
        },
        y: {
          beginAtZero: true,
          ticks: {
            callback: (value) => compactTokens.format(Number(value)),
          },
        },
      },
    }),
    [granularity, monthlyTokenLimit],
  );

  const granularityButton = (value: TumGranularity, label: string) => (
    <button
      type="button"
      onClick={() => setGranularity(value)}
      className={cn(
        "rounded px-2 py-0.5 text-xs transition-colors",
        granularity === value
          ? "bg-muted text-foreground font-medium"
          : "text-muted-foreground hover:text-foreground",
      )}
    >
      {label}
    </button>
  );

  return (
    <ChartCard
      title="Usage History"
      chartId="tum-history"
      expandedChart={null}
      onExpand={() => {}}
      hasData={hasData}
      expandable={false}
    >
      <div className="mb-2 flex items-center gap-1">
        {granularityButton("day", "By day")}
        {granularityButton("cycle", "By billing cycle")}
      </div>
      {hasData ? (
        <div style={{ height: 260 }}>
          {/* `<Chart>` with an explicit `"bar" | "line"` generic accepts the
              bar series plus the line limit overlay (see InsightsAgents). */}
          <Chart<"bar" | "line", number[], string>
            type="bar"
            data={chartData}
            options={chartOptions}
          />
        </div>
      ) : (
        <Type muted small>
          No tokens under management recorded yet.
        </Type>
      )}
    </ChartCard>
  );
}

export const TumUsageSection = (): JSX.Element => {
  const { data: tum } = useGetTokensUnderManagement();

  return (
    <Page.Section>
      <Page.Section.Title>Tokens Under Management</Page.Section.Title>
      <Page.Section.Description>
        The volume of agent traffic Gram has processed, stored, and run security
        analysis on this billing cycle, measured in tokens.
      </Page.Section.Description>
      <Page.Section.Body>
        {tum ? (
          <Stack gap={3} className="mb-6">
            <Stack direction="horizontal" align="center" gap={1}>
              <Type variant="body" className="font-medium">
                Tokens Under Management
              </Type>
              <SimpleTooltip tooltip="Counts tokens from agent sessions Gram has stored chats or tool calls for. Compared against your contracted monthly allowance.">
                <Info className="text-muted-foreground h-4 w-4" />
              </SimpleTooltip>
              <Type muted small className="ml-auto">
                Billing cycle: {formatCycleRange(tum)}
              </Type>
            </Stack>
            <UsageProgress
              value={tum.tokens}
              included={tum.monthlyTokenLimit ?? 0}
              overageIncrement={tum.monthlyTokenLimit ?? 1}
              noMax={tum.monthlyTokenLimit == null}
            />
            <div className="mt-8">
              <TumHistoryChart
                history={tum.history}
                monthlyTokenLimit={tum.monthlyTokenLimit}
              />
            </div>
          </Stack>
        ) : (
          <div className="space-y-4">
            <Skeleton className="h-4 w-1/3" />
            <Skeleton className="h-4 w-full" />
            <Skeleton className="h-40 w-full" />
          </div>
        )}
      </Page.Section.Body>
    </Page.Section>
  );
};

export const TumAdminSection = (): JSX.Element => {
  const queryClient = useQueryClient();
  const { data: tum } = useGetTokensUnderManagement();

  const [tokenLimit, setTokenLimit] = useState("");
  const [alertEmail, setAlertEmail] = useState("");
  const [anchorDay, setAnchorDay] = useState("1");

  // Prefill the form once the current contract terms load.
  useEffect(() => {
    if (!tum) return;
    setTokenLimit(tum.monthlyTokenLimit?.toString() ?? "");
    setAlertEmail(tum.alertEmail ?? "");
    setAnchorDay(tum.billingCycleAnchorDay.toString());
  }, [tum]);

  const mutation = useSetBillingMetadataMutation({
    onSuccess: () => {
      void invalidateAllGetTokensUnderManagement(queryClient);
    },
  });

  const parsedLimit = tokenLimit.trim() === "" ? undefined : Number(tokenLimit);
  const parsedAnchorDay = Number(anchorDay);
  const limitInvalid =
    parsedLimit !== undefined &&
    (!Number.isFinite(parsedLimit) || parsedLimit < 0);
  const anchorDayInvalid =
    !Number.isInteger(parsedAnchorDay) ||
    parsedAnchorDay < 1 ||
    parsedAnchorDay > 31;

  const handleSave = () => {
    mutation.mutate({
      request: {
        setBillingMetadataRequestBody: {
          monthlyTokenLimit: parsedLimit,
          alertEmail: alertEmail.trim() === "" ? undefined : alertEmail.trim(),
          billingCycleAnchorDay: parsedAnchorDay,
        },
      },
    });
  };

  return (
    <Page.Section>
      <Page.Section.Title>
        TUM Contract (PLATFORM ADMIN VIEW ONLY)
      </Page.Section.Title>
      <Page.Section.Description>
        Set this organization's contracted tokens under management terms.
        Customers never see this section or the alert email.
      </Page.Section.Description>
      <Page.Section.Body>
        <Stack gap={4} className="max-w-md">
          <Stack gap={2}>
            <Label htmlFor="tum-monthly-limit">
              Allowed TUM per month (tokens)
            </Label>
            <Input
              id="tum-monthly-limit"
              type="number"
              min={0}
              placeholder="Leave empty for no contracted limit"
              value={tokenLimit}
              onChange={setTokenLimit}
            />
          </Stack>
          <Stack gap={2}>
            <Label htmlFor="tum-alert-email">Alert email</Label>
            <Input
              id="tum-alert-email"
              type="email"
              placeholder="billing-alerts@customer.com"
              value={alertEmail}
              onChange={setAlertEmail}
            />
          </Stack>
          <Stack gap={2}>
            <Label htmlFor="tum-anchor-day">
              Billing cycle anchor day (1–31)
            </Label>
            <Input
              id="tum-anchor-day"
              type="number"
              min={1}
              max={31}
              value={anchorDay}
              onChange={setAnchorDay}
            />
          </Stack>
          <Stack direction="horizontal" align="center" gap={3}>
            <Button
              onClick={handleSave}
              disabled={mutation.isPending || limitInvalid || anchorDayInvalid}
            >
              {mutation.isPending ? "SAVING..." : "SAVE CONTRACT TERMS"}
            </Button>
            {mutation.isSuccess && !mutation.isPending && (
              <Type muted small>
                Saved.
              </Type>
            )}
            {mutation.isError && (
              <Type small className="text-destructive">
                Failed to save contract terms.
              </Type>
            )}
          </Stack>
        </Stack>
      </Page.Section.Body>
    </Page.Section>
  );
};
