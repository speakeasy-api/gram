import type { Meta, StoryObj } from "@storybook/react-vite";
import { useState } from "react";

import { SegmentedControl } from ".";

const meta: Meta<typeof SegmentedControl> = {
  title: "Design System/SegmentedControl",
  component: SegmentedControl,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof SegmentedControl>;

export const Default: Story = {
  render: function Render() {
    const [value, setValue] = useState("tokens");

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
  },
};

/** A choice not yet made: no segment is active until the reader picks one. */
export const NothingSelected: Story = {
  render: function NothingSelectedStory() {
    const [value, setValue] = useState<"approved" | "blocked" | null>(null);
    return (
      <SegmentedControl
        ariaLabel="Access decision"
        value={value}
        onChange={setValue}
        options={[
          { value: "approved", label: "Approve" },
          { value: "blocked", label: "Disallow" },
        ]}
      />
    );
  },
};
