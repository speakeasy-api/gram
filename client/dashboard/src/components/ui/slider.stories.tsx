import type { Meta, StoryObj } from "@storybook/react-vite";
import { useState } from "react";

import { Slider } from "@/components/ui/slider";

const meta: Meta<typeof Slider> = {
  title: "UI/Slider",
  component: Slider,
  tags: ["autodocs"],
};

export default meta;

type Story = StoryObj<typeof Slider>;

function BasicSlider() {
  const [value, setValue] = useState(40);

  return (
    <div className="flex w-64 flex-col gap-2">
      <Slider value={value} onChange={setValue} />
      <p className="text-muted-foreground text-xs">Value: {value}</p>
    </div>
  );
}

export const Default: Story = {
  render: () => <BasicSlider />,
};

function TicksSlider() {
  const [value, setValue] = useState(50);

  return (
    <div className="flex w-64 flex-col gap-2">
      <Slider
        value={value}
        onChange={setValue}
        min={0}
        max={100}
        step={25}
        ticks={[0, 25, 50, 75, 100]}
      />
      <p className="text-muted-foreground text-xs">Value: {value}</p>
    </div>
  );
}

export const WithTicks: Story = {
  render: () => <TicksSlider />,
};

function CustomRangeSlider() {
  const [value, setValue] = useState(1.5);

  return (
    <div className="flex w-64 flex-col gap-2">
      <Slider value={value} onChange={setValue} min={0} max={2} step={0.1} />
      <p className="text-muted-foreground text-xs">
        Temperature: {value.toFixed(1)}
      </p>
    </div>
  );
}

export const CustomRange: Story = {
  render: () => <CustomRangeSlider />,
};

export const Disabled: Story = {
  render: () => (
    <div className="w-64">
      <Slider value={30} onChange={() => {}} disabled />
    </div>
  ),
};
