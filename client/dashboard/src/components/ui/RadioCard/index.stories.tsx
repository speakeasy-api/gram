import type { Meta, StoryObj } from "@storybook/react-vite";
import { useState } from "react";

import { RadioCard, RadioCardGroup } from ".";

const meta: Meta<typeof RadioCardGroup> = {
  title: "Design System/RadioCard",
  component: RadioCardGroup,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof RadioCardGroup>;

export const Default: Story = {
  render: function Render() {
    const [value, setValue] = useState<string | null>(null);

    return (
      <RadioCardGroup
        aria-label="Delivery speed"
        value={value}
        onValueChange={setValue}
      >
        <RadioCard value="standard" title="Standard delivery" />
        <RadioCard value="express" title="Express delivery">
          Arrives sooner with priority handling.
        </RadioCard>
        <RadioCard value="pickup" title="Collect from a nearby location." />
      </RadioCardGroup>
    );
  },
};

export const WithoutIndicators: Story = {
  render: function Render() {
    const [value, setValue] = useState("comfortable");

    return (
      <RadioCardGroup
        aria-label="View density"
        value={value}
        onValueChange={setValue}
        showIndicator={false}
      >
        <RadioCard value="comfortable" title="Comfortable">
          More space between items.
        </RadioCard>
        <RadioCard value="compact" title="Compact" />
      </RadioCardGroup>
    );
  },
};

export const Horizontal: Story = {
  render: function Render() {
    const [value, setValue] = useState("grid");

    return (
      <RadioCardGroup
        aria-label="View mode"
        orientation="horizontal"
        value={value}
        onValueChange={setValue}
      >
        <RadioCard value="grid" title="Grid" />
        <RadioCard value="list" title="Detailed list" />
        <RadioCard value="board" title="Board">
          Grouped into columns.
        </RadioCard>
      </RadioCardGroup>
    );
  },
};

export const DisabledChoice: Story = {
  render: function Render() {
    const [value, setValue] = useState("weekly");

    return (
      <RadioCardGroup
        aria-label="Update frequency"
        value={value}
        onValueChange={setValue}
      >
        <RadioCard value="daily" title="Daily updates" disabled>
          Not available for this source.
        </RadioCard>
        <RadioCard value="weekly" title="Weekly updates">
          A summary once each week.
        </RadioCard>
        <RadioCard value="monthly" title="Monthly updates">
          A summary once each month.
        </RadioCard>
      </RadioCardGroup>
    );
  },
};

export const CustomContent: Story = {
  render: function Render() {
    const [value, setValue] = useState("");

    return (
      <RadioCardGroup
        aria-label="Update frequency"
        orientation="horizontal"
        value={value}
        onValueChange={setValue}
        showIndicator={false}
      >
        <RadioCard value="daily">
          <div className="flex flex-col items-center gap-1">
            <span className="font-medium text-base">⚡ Daily Sync</span>
            <span className="text-sm text-orange-700">Every morning</span>
          </div>
        </RadioCard>
        <RadioCard value="weekly">
          <div className="flex flex-col items-center gap-1">
            <span className="font-medium text-base">📅 Weekly Digest</span>
            <span className="text-sm text-orange-700">
              Sent out Monday morning
            </span>
          </div>
        </RadioCard>
        <RadioCard value="monthly">
          <div className="flex flex-col items-center gap-1">
            <span className="font-medium text-base">🗓️ Monthly Report</span>
            <span className="text-sm text-orange-700">
              A full breakdown at month's end
            </span>
          </div>
        </RadioCard>
      </RadioCardGroup>
    );
  },
};
