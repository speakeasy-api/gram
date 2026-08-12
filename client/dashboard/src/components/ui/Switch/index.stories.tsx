import type { Meta, StoryObj } from "@storybook/react-vite";
import { useState } from "react";

import { Switch } from ".";
import { Label } from "@/components/ui/Label";

const meta: Meta<typeof Switch> = {
  title: "Design System/Switch",
  component: Switch,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof Switch>;

export const Default: Story = {
  render: function Render() {
    const [checked, setChecked] = useState(true);

    return (
      <div className="flex items-center gap-2">
        <Switch
          checked={checked}
          onCheckedChange={setChecked}
          aria-labelledby="logging-label"
        />
        <Label id="logging-label">Enable logging</Label>
      </div>
    );
  },
};

export const Disabled: Story = {
  render: () => (
    <Switch
      checked={false}
      onCheckedChange={() => {}}
      disabled
      aria-label="Locked"
    />
  ),
};
