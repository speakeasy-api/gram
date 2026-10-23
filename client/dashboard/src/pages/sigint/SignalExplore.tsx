import { useFilterState } from "@/components/filters";
import { InlineEmptyState } from "@/components/inline-empty-state";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { useAnalyticsDescribe } from "@gram/client/react-query/analyticsDescribe.js";
import { useRunQuery } from "@/pages/explore/useRunQuery";
import type { AnalyticsSignalCatalog } from "@gram/client/models/components/analyticssignalcatalog.js";
import { useState } from "react";
import { Link } from "react-router";
import {
  SIGNAL_FILTERS,
  WINDOW_OPTIONS,
  signalPayload,
  type SignalControls,
  type SignalChartMetric,
} from "./explorationModel";
import { SignalResults } from "./SignalResults";
import { SignalAttributes } from "./SignalAttributes";

export function SignalExplore(): JSX.Element {
  return (
    <RequireScope scope="project:read" level="section">
      <SignalCatalog />
    </RequireScope>
  );
}

function SignalCatalog(): JSX.Element {
  const query = useAnalyticsDescribe();
  if (query.isPending) return <Skeleton className="h-[520px] w-full" />;
  if (query.isError)
    return (
      <InlineEmptyState
        icon="triangle-alert"
        heading="Couldn’t load signal exploration"
        description="Try loading the project’s sensors again."
        action={<Button onClick={() => void query.refetch()}>Try again</Button>}
      />
    );
  const catalog = query.data.signalMetrics;
  if (
    !catalog ||
    !catalog.sensors.some((sensor) => sensor.signalIds.length > 0)
  )
    return (
      <InlineEmptyState
        icon="radio"
        heading="Start with a sensor"
        description="Choose the signals you want to find in agent conversations, then enable a sensor to measure them."
        action={
          <Button asChild>
            <Link to="?tab=sensors">View sensors</Link>
          </Button>
        }
      />
    );
  return (
    <SignalWorkbench
      catalog={{
        ...catalog,
        sensors: catalog.sensors.filter(
          (sensor) => sensor.signalIds.length > 0,
        ),
      }}
    />
  );
}

function SignalWorkbench({
  catalog,
}: {
  catalog: AnalyticsSignalCatalog;
}): JSX.Element {
  const filters = useFilterState(SIGNAL_FILTERS);
  const [metric, setMetric] = useState<SignalChartMetric>("matched_messages");
  const sensor =
    catalog.sensors.find((item) => item.id === filters.values.sensor) ??
    catalog.sensors[0]!;
  const signalOptions =
    sensor.mode === "ordered_score"
      ? sensor.signalIds.map((_, index) => String(index))
      : sensor.signalIds;
  const signalId = signalOptions.includes(filters.values.signal ?? "")
    ? filters.values.signal!
    : signalOptions[0]!;
  const values = {
    ...filters.values,
    sensor: sensor.id,
    signal: signalId,
    window: filters.values.window ?? "7d",
  };
  const threshold = values.threshold ?? 0.5;
  const validThreshold =
    Number.isFinite(threshold) && threshold > 0 && threshold <= 1;
  const [submitted, setSubmitted] = useState(() =>
    signalPayload(
      catalog,
      { ...values, threshold: validThreshold ? threshold : 0.5 },
      new Date(),
    ),
  );
  const query = useRunQuery(submitted);
  const names = new Map(
    catalog.signals.map((signal) => [signal.id, signal.name]),
  );
  const options = {
    sensor: catalog.sensors.map((item) => ({
      value: item.id,
      label: item.name,
    })),
    signal: signalOptions.map((id) => ({
      value: id,
      label:
        sensor.mode === "ordered_score"
          ? `Level ${Number(id) + 1} and higher`
          : (names.get(id) ?? "Removed signal"),
    })),
    window: WINDOW_OPTIONS,
    step: catalog.steps
      .filter((step) => step !== "auto")
      .map((step) => ({
        value: step,
        label: `${step[0]!.toUpperCase()}${step.slice(1)}`,
      })),
    comparison: [
      { value: "equals", label: "Equals" },
      { value: "exists", label: "Is present" },
      { value: "missing", label: "Is missing" },
    ],
  };
  const submittedSensor = catalog.sensors.find(
    (item) => item.id === submitted.signalQuery?.selection.sensorId,
  );
  const selection = submitted.signalQuery!.selection;
  const selectedName =
    selection.minimumLevel !== undefined
      ? `Level ${selection.minimumLevel + 1} and higher`
      : (names.get(selection.signalId ?? "") ?? "Selected signal");
  const run = () => setSubmitted(signalPayload(catalog, values, new Date()));
  return (
    <div className="flex min-w-0 flex-col gap-5">
      <Page.Toolbar>
        <Page.Toolbar.Filters
          schema={SIGNAL_FILTERS}
          values={values}
          optionsById={options}
          onChange={(id, value) => filters.setValues({ [id]: value })}
          onClear={(id) => filters.clearValue(id as keyof SignalControls)}
          onClearAll={filters.clearAll}
        />
        <Page.Toolbar.Leading>
          <SignalAttributes
            request={submitted}
            value={values.group}
            onGroup={(key) => filters.setValue("group", key)}
          />
        </Page.Toolbar.Leading>
        <Page.Toolbar.Actions>
          <SegmentedControl
            value={metric}
            onChange={setMetric}
            ariaLabel="Chart measurement"
            options={[
              {
                value: "matched_messages",
                label: "Matches",
                tooltip: "Count of matching original messages",
              },
              {
                value: "match_share",
                label: "Share",
                tooltip:
                  "Matching messages divided by measured answers in each interval",
              },
            ]}
          />
          <Button onClick={run} disabled={query.isFetching || !validThreshold}>
            Run query
          </Button>
        </Page.Toolbar.Actions>
      </Page.Toolbar>
      {!validThreshold && (
        <p role="alert" className="text-sm text-destructive">
          Choose a minimum probability greater than 0 and at most 1 in More
          filters.
        </p>
      )}
      <SignalResults
        metric={metric}
        query={query}
        request={submitted}
        title={selectedName}
        sensorName={submittedSensor?.name ?? "Selected sensor"}
        onRetry={() => void query.refetch()}
      />
    </div>
  );
}
