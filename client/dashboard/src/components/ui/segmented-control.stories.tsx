import type { Meta, StoryObj } from "@storybook/react-vite";
import { useState } from "react";

import { SegmentedControl } from "@/components/ui/segmented-control";

const meta: Meta<typeof SegmentedControl> = {
  title: "UI/SegmentedControl",
  component: SegmentedControl,
  tags: ["autodocs"],
};

export default meta;

type Story = StoryObj<typeof SegmentedControl>;

function TokensCostControl() {
  const [value, setValue] = useState<"tokens" | "cost">("tokens");

  return (
    <SegmentedControl
      value={value}
      onChange={setValue}
      options={[
        { value: "tokens", label: "Tokens" },
        { value: "cost", label: "Cost" },
      ]}
    />
  );
}

export const Default: Story = {
  render: () => <TokensCostControl />,
};

function ThreeOptionControl() {
  const [value, setValue] = useState<"day" | "week" | "month">("week");

  return (
    <SegmentedControl
      value={value}
      onChange={setValue}
      options={[
        { value: "day", label: "Day" },
        { value: "week", label: "Week" },
        { value: "month", label: "Month" },
      ]}
    />
  );
}

export const ThreeOptions: Story = {
  render: () => <ThreeOptionControl />,
};

function WithTooltipControl() {
  const [value, setValue] = useState<"employees" | "unknown">("employees");

  return (
    <SegmentedControl
      value={value}
      onChange={setValue}
      options={[
        { value: "employees", label: "Employees" },
        {
          value: "unknown",
          label: "Unknown users",
          tooltip: "Users not matched to a known employee",
        },
      ]}
    />
  );
}

export const WithTooltip: Story = {
  render: () => <WithTooltipControl />,
};

export const Disabled: Story = {
  render: () => (
    <SegmentedControl
      value="tokens"
      onChange={() => {}}
      disabled
      options={[
        { value: "tokens", label: "Tokens" },
        { value: "cost", label: "Cost" },
      ]}
    />
  ),
};
