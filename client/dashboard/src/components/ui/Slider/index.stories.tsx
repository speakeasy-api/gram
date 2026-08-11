import type { Meta, StoryObj } from "@storybook/react-vite";
import { useState } from "react";

import { Slider } from ".";

const meta: Meta<typeof Slider> = {
  title: "Design System/Slider",
  component: Slider,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof Slider>;

export const Default: Story = {
  render: function Render() {
    const [value, setValue] = useState(40);

    return (
      <div className="w-96">
        <Slider value={value} onChange={setValue} />
      </div>
    );
  },
};

export const WithTicks: Story = {
  render: function Render() {
    const [value, setValue] = useState(50);

    return (
      <div className="w-96">
        <Slider
          value={value}
          onChange={setValue}
          ticks={[0, 25, 50, 75, 100]}
        />
      </div>
    );
  },
};
