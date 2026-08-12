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
