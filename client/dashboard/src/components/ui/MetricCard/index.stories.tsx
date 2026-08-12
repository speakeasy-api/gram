import type { Meta, StoryObj } from "@storybook/react-vite";

import { MetricCard } from ".";

const meta: Meta<typeof MetricCard> = {
  title: "Design System/MetricCard",
  component: MetricCard,
  tags: ["autodocs"],
};

export default meta;

type Story = StoryObj<typeof MetricCard>;

export const Default: Story = {
  args: {
    tone: "neutral",
    label: "Findings · Last 24h",
    value: "3,418",
    delta: "+18%",
    description: "across 8 clustered signals",
    className: "max-w-xs border",
  },
};

export const DestructiveTone: Story = {
  args: {
    label: "Org risk score",
    value: "78",
    delta: "+6",
    tone: "destructive",
    description: "High — driven by 2 critical signals",
    className: "max-w-xs border",
  },
};

export const NoDelta: Story = {
  args: {
    tone: "neutral",
    label: "Active seats",
    value: "540",
    description: "across 12 teams",
    className: "max-w-xs border",
  },
};

export const Sizes: Story = {
  render: () => (
    <div className="flex flex-col gap-4">
      <MetricCard.Group>
        <MetricCard
          size="sm"
          tone="neutral"
          label="Small"
          value="128"
          delta="+4"
        />
        <MetricCard
          size="md"
          tone="neutral"
          label="Medium"
          value="128"
          delta="+4"
        />
        <MetricCard
          size="lg"
          tone="neutral"
          label="Large"
          value="128"
          delta="+4"
        />
      </MetricCard.Group>
    </div>
  ),
};

export const Group: Story = {
  render: () => (
    <MetricCard.Group>
      <MetricCard
        label="Org risk score"
        value="78"
        delta="+6"
        tone="destructive"
        description="High — driven by 2 critical signals"
      />
      <MetricCard
        tone="neutral"
        label="Findings · Last 24h"
        value="3,418"
        delta="+18%"
        description="across 8 clustered signals"
      />
      <MetricCard
        tone="neutral"
        label="Open signals"
        value="8"
        delta="2 crit"
        description="unresolved & ranked by risk"
      />
      <MetricCard
        tone="neutral"
        label="Users exposed"
        value="86"
        delta="+12"
        description="of 540 active seats"
      />
    </MetricCard.Group>
  ),
};

export const SuccessAndWarning: Story = {
  render: () => (
    <MetricCard.Group>
      <MetricCard
        label="Policies passing"
        value="97%"
        delta="+2%"
        tone="success"
        deltaTone="success"
        description="41 of 42 policies green"
      />
      <MetricCard
        label="Pending reviews"
        value="14"
        delta="3 stale"
        tone="warning"
        deltaTone="warning"
        description="oldest waiting 6 days"
      />
    </MetricCard.Group>
  ),
};
