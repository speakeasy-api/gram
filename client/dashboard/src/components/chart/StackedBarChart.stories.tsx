import type { Meta, StoryObj } from "@storybook/react-vite";
import { faker } from "@faker-js/faker";
import { useState } from "react";
import { StackedBarChart, type StackedBarSeries } from "./StackedBarChart";

faker.seed(61);

const SERVER_LABELS = Array.from({ length: 8 }, () =>
  faker.company.name(),
).sort();

function buildSeries(): { labels: string[]; series: StackedBarSeries[] } {
  return {
    labels: SERVER_LABELS,
    series: [
      {
        label: "Successful",
        values: SERVER_LABELS.map(() =>
          faker.number.int({ min: 20, max: 200 }),
        ),
      },
      {
        label: "Failed",
        values: SERVER_LABELS.map(() => faker.number.int({ min: 0, max: 30 })),
      },
    ],
  };
}

const meta: Meta<typeof StackedBarChart> = {
  title: "Charts/StackedBarChart",
  component: StackedBarChart,
  parameters: { layout: "padded" },
};

export default meta;

type Story = StoryObj<typeof StackedBarChart>;

export const Default: Story = {
  render: () => {
    const { labels, series } = buildSeries();
    return (
      <div className="max-w-2xl">
        <StackedBarChart labels={labels} series={series} />
      </div>
    );
  },
};

export const WithTotals: Story = {
  render: () => {
    const { labels, series } = buildSeries();
    return (
      <div className="max-w-2xl">
        <StackedBarChart labels={labels} series={series} showTotals />
      </div>
    );
  },
};

function CollapsedDemo() {
  const [expanded, setExpanded] = useState(false);
  const { labels, series } = buildSeries();
  return (
    <div className="max-w-2xl">
      <StackedBarChart
        labels={labels}
        series={series}
        maxRows={4}
        expanded={expanded}
        onShowAll={() => setExpanded(true)}
      />
    </div>
  );
}

export const Collapsed: Story = {
  render: () => <CollapsedDemo />,
};

export const SingleSeries: Story = {
  render: () => {
    const { labels, series } = buildSeries();
    return (
      <div className="max-w-2xl">
        <StackedBarChart labels={labels} series={[series[0]!]} />
      </div>
    );
  },
};

export const Loading: Story = {
  render: () => (
    <div className="max-w-2xl">
      <StackedBarChart labels={[]} series={[]} loading height={220} />
    </div>
  ),
};

export const Empty: Story = {
  render: () => (
    <div className="max-w-2xl">
      <StackedBarChart
        labels={[]}
        series={[]}
        emptyMessage="No matching tool usage"
      />
    </div>
  ),
};
