import type { Meta, StoryObj } from "@storybook/react-vite";
import { faker } from "@faker-js/faker";
import { useState } from "react";
import {
  Timeseries,
  type TimeseriesPoint,
  type TimeseriesSeries,
} from "./Timeseries";

faker.seed(41);

const DAY_MS = 24 * 60 * 60 * 1000;

function buildPoints(
  days: number,
  min: number,
  max: number,
): TimeseriesPoint[] {
  const now = Date.now();
  return Array.from({ length: days }, (_, i) => ({
    x: now - (days - i) * DAY_MS,
    y: faker.number.int({ min, max }),
  }));
}

// Series labels double as a demo of the design language: the language
// palette IS the series palette, so naming series after languages makes the
// color assignment legible at a glance.
const SERIES_LABELS = ["TypeScript", "Python", "Go"];

function buildSeries(
  days: number,
  min: number,
  max: number,
): TimeseriesSeries[] {
  return SERIES_LABELS.map((label) => ({
    label,
    data: buildPoints(days, min, max),
  }));
}

const meta: Meta<typeof Timeseries> = {
  title: "Charts/Timeseries",
  component: Timeseries,
  parameters: { layout: "padded" },
};

export default meta;

type Story = StoryObj<typeof Timeseries>;

export const Line: Story = {
  render: () => (
    <div className="max-w-2xl">
      <Timeseries series={buildSeries(30, 10, 200)} mode="line" />
    </div>
  ),
};

export const SingleSeries: Story = {
  render: () => (
    <div className="max-w-2xl">
      <Timeseries series={[buildSeries(30, 10, 200)[0]!]} mode="line" />
    </div>
  ),
};

export const Area: Story = {
  render: () => (
    <div className="max-w-2xl">
      <Timeseries series={buildSeries(30, 10, 200)} mode="area" />
    </div>
  ),
};

export const StackedBar: Story = {
  render: () => (
    <div className="max-w-2xl">
      <Timeseries series={buildSeries(14, 5, 80)} mode="stacked-bar" />
    </div>
  ),
};

export const BarWithTrend: Story = {
  render: () => (
    <div className="max-w-2xl">
      <Timeseries series={buildSeries(30, 5, 80)} mode="bar-with-trend" />
    </div>
  ),
};

export const CustomValueFormat: Story = {
  render: () => (
    <div className="max-w-2xl">
      <Timeseries
        series={buildSeries(30, 1, 40)}
        mode="area"
        valueFormatter={(v) => `$${v.toFixed(2)}`}
      />
    </div>
  ),
};

function ZoomableDemo() {
  const [range, setRange] = useState<string | null>(null);
  return (
    <div className="max-w-2xl">
      <Timeseries
        series={buildSeries(30, 10, 200)}
        mode="line"
        enableZoom
        onZoomRange={(from, to) =>
          setRange(`${from.toLocaleString()} – ${to.toLocaleString()}`)
        }
      />
      {range && (
        <p className="text-muted-foreground mt-2 text-xs">Selected: {range}</p>
      )}
    </div>
  );
}

export const Zoomable: Story = {
  render: () => <ZoomableDemo />,
};

export const Loading: Story = {
  render: () => (
    <div className="max-w-2xl">
      <Timeseries series={[]} loading />
    </div>
  ),
};

export const Empty: Story = {
  render: () => (
    <div className="max-w-2xl">
      <Timeseries
        series={[]}
        emptyMessage="No tool calls for the selected time range"
      />
    </div>
  ),
};
