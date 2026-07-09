import type { Meta, StoryObj } from "@storybook/react-vite";
import { useState } from "react";

import { Switch } from "@/components/ui/switch";
import { Label } from "@/components/ui/label";

const meta: Meta<typeof Switch> = {
  title: "UI/Switch",
  component: Switch,
  tags: ["autodocs"],
};

export default meta;

type Story = StoryObj<typeof Switch>;

export const Unchecked: Story = {
  render: () => <Switch checked={false} onCheckedChange={() => {}} />,
};

export const Checked: Story = {
  render: () => <Switch checked={true} onCheckedChange={() => {}} />,
};

export const Disabled: Story = {
  render: () => (
    <div className="flex items-center gap-4">
      <Switch checked={false} onCheckedChange={() => {}} disabled />
      <Switch checked={true} onCheckedChange={() => {}} disabled />
    </div>
  ),
};

function SwitchWithLabel() {
  const [checked, setChecked] = useState(false);
  return (
    <div className="flex items-center gap-2">
      <Switch
        checked={checked}
        onCheckedChange={setChecked}
        aria-labelledby="notifications-label"
      />
      <Label id="notifications-label">Enable notifications</Label>
    </div>
  );
}

export const WithLabel: Story = {
  render: () => <SwitchWithLabel />,
};

function ControlledSwitch() {
  const [checked, setChecked] = useState(false);

  return (
    <div className="flex items-center gap-2">
      <Switch
        checked={checked}
        onCheckedChange={setChecked}
        aria-label="Toggle setting"
      />
      <span className="text-muted-foreground text-sm">
        {checked ? "On" : "Off"}
      </span>
    </div>
  );
}

export const Controlled: Story = {
  render: () => <ControlledSwitch />,
};
