import type { Meta, StoryObj } from "@storybook/react-vite";
import { faker } from "@faker-js/faker";

import { Sparkline } from "@/components/chart/Sparkline";
import { StatCard, StatTile } from "@/components/ui/stat-tile";

faker.seed(7);

const meta: Meta<typeof StatTile> = {
  title: "UI/StatTile",
  component: StatTile,
  tags: ["autodocs"],
  parameters: {
    layout: "padded",
  },
};

export default meta;

type Story = StoryObj<typeof StatTile>;

export const Default: Story = {
  render: () => (
    <div className="max-w-xs">
      <StatTile
        label="Total requests"
        value="128,402"
        delta={{ value: "+12.4%", tone: "positive" }}
        caption="vs. previous 30 days"
      />
    </div>
  ),
};

export const NegativeDelta: Story = {
  render: () => (
    <div className="max-w-xs">
      <StatTile
        label="Success rate"
        value="98.2%"
        delta={{ value: "-0.6%", tone: "negative" }}
        caption="vs. previous 30 days"
      />
    </div>
  ),
};

export const NeutralDelta: Story = {
  render: () => (
    <div className="max-w-xs">
      <StatTile
        label="Active tool servers"
        value="42"
        delta={{ value: "0%", tone: "neutral" }}
        caption="No change this week"
      />
    </div>
  ),
};

export const DestructiveTone: Story = {
  render: () => (
    <div className="max-w-xs">
      <StatTile
        label="Policy violations"
        value="17"
        tone="destructive"
        delta={{ value: "+9", tone: "negative" }}
        caption="Requires review"
      />
    </div>
  ),
};

export const WarningTone: Story = {
  render: () => (
    <div className="max-w-xs">
      <StatTile
        label="Expiring credentials"
        value="3"
        tone="warning"
        caption="Within the next 7 days"
      />
    </div>
  ),
};

export const WithSparkline: Story = {
  render: () => (
    <div className="max-w-xs">
      <StatTile
        label="Agent sessions"
        value="1,204"
        delta={{ value: "+4.2%", tone: "neutral" }}
        sparkline={
          <Sparkline
            data={[12, 18, 14, 22, 19, 27, 24, 31, 28, 35]}
            trendColor
            width={64}
            height={24}
          />
        }
      />
    </div>
  ),
};

export const NoDeltaOrCaption: Story = {
  render: () => (
    <div className="max-w-xs">
      <StatTile label="Toolsets" value="9" />
    </div>
  ),
};

export const Loading: Story = {
  render: () => (
    <div className="max-w-xs">
      <StatTile label="Total requests" value="—" caption="—" isLoading />
    </div>
  ),
};

export const CardWrapper: Story = {
  render: () => (
    <div className="grid max-w-3xl grid-cols-3 gap-4">
      <StatCard
        label="Total cost"
        value="$4,208"
        delta={{ value: "+3.1%", tone: "positive" }}
        caption="Last 30 days"
      />
      <StatCard
        label="Blocked calls"
        value="212"
        tone="destructive"
        delta={{ value: "+41", tone: "negative" }}
        caption="Needs attention"
      />
      <StatCard label="Toolsets" value="9" caption="Across 3 projects" />
    </div>
  ),
};

export const CardGridLoading: Story = {
  render: () => (
    <div className="grid max-w-3xl grid-cols-3 gap-4">
      {Array.from({ length: 3 }, (_, index) => (
        <StatCard
          key={index}
          label={faker.word.noun()}
          value="—"
          caption="—"
          isLoading
        />
      ))}
    </div>
  ),
};
