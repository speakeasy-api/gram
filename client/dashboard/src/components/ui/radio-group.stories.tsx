import type { Meta, StoryObj } from "@storybook/react-vite";
import { useState } from "react";

import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Label } from "@/components/ui/label";

const meta: Meta<typeof RadioGroup> = {
  title: "UI/RadioGroup",
  component: RadioGroup,
  tags: ["autodocs"],
};

export default meta;

type Story = StoryObj<typeof RadioGroup>;

export const Default: Story = {
  render: () => (
    <RadioGroup defaultValue="monthly">
      <div className="flex items-center gap-2">
        <RadioGroupItem value="monthly" id="monthly" />
        <Label htmlFor="monthly">Monthly billing</Label>
      </div>
      <div className="flex items-center gap-2">
        <RadioGroupItem value="annual" id="annual" />
        <Label htmlFor="annual">Annual billing</Label>
      </div>
    </RadioGroup>
  ),
};

export const DisabledItem: Story = {
  render: () => (
    <RadioGroup defaultValue="free">
      <div className="flex items-center gap-2">
        <RadioGroupItem value="free" id="free" />
        <Label htmlFor="free">Free</Label>
      </div>
      <div className="flex items-center gap-2">
        <RadioGroupItem value="pro" id="pro" />
        <Label htmlFor="pro">Pro</Label>
      </div>
      <div className="flex items-center gap-2">
        <RadioGroupItem value="enterprise" id="enterprise" disabled />
        <Label htmlFor="enterprise">Enterprise (contact sales)</Label>
      </div>
    </RadioGroup>
  ),
};

export const DisabledGroup: Story = {
  render: () => (
    <RadioGroup defaultValue="monthly" disabled>
      <div className="flex items-center gap-2">
        <RadioGroupItem value="monthly" id="monthly-disabled" />
        <Label htmlFor="monthly-disabled">Monthly billing</Label>
      </div>
      <div className="flex items-center gap-2">
        <RadioGroupItem value="annual" id="annual-disabled" />
        <Label htmlFor="annual-disabled">Annual billing</Label>
      </div>
    </RadioGroup>
  ),
};

export const Horizontal: Story = {
  render: () => (
    <RadioGroup defaultValue="sm" className="flex gap-4">
      <div className="flex items-center gap-2">
        <RadioGroupItem value="sm" id="size-sm" />
        <Label htmlFor="size-sm">Small</Label>
      </div>
      <div className="flex items-center gap-2">
        <RadioGroupItem value="md" id="size-md" />
        <Label htmlFor="size-md">Medium</Label>
      </div>
      <div className="flex items-center gap-2">
        <RadioGroupItem value="lg" id="size-lg" />
        <Label htmlFor="size-lg">Large</Label>
      </div>
    </RadioGroup>
  ),
};

function ControlledRadioGroup() {
  const [value, setValue] = useState("staging");

  return (
    <div className="flex flex-col gap-2">
      <RadioGroup value={value} onValueChange={setValue}>
        <div className="flex items-center gap-2">
          <RadioGroupItem value="production" id="env-production" />
          <Label htmlFor="env-production">Production</Label>
        </div>
        <div className="flex items-center gap-2">
          <RadioGroupItem value="staging" id="env-staging" />
          <Label htmlFor="env-staging">Staging</Label>
        </div>
        <div className="flex items-center gap-2">
          <RadioGroupItem value="development" id="env-development" />
          <Label htmlFor="env-development">Development</Label>
        </div>
      </RadioGroup>
      <p className="text-muted-foreground text-xs">Selected: {value}</p>
    </div>
  );
}

export const Controlled: Story = {
  render: () => <ControlledRadioGroup />,
};
