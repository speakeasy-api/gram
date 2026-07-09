import type { Meta, StoryObj } from "@storybook/react-vite";
import { Info } from "lucide-react";

import { Label } from "@/components/ui/label";

const meta: Meta<typeof Label> = {
  title: "UI/Label",
  component: Label,
  tags: ["autodocs"],
};

export default meta;

type Story = StoryObj<typeof Label>;

export const Default: Story = {
  render: () => <Label>Project name</Label>,
};

export const WithInput: Story = {
  render: () => (
    <div className="flex flex-col gap-1.5">
      <Label htmlFor="project-name">Project name</Label>
      <input
        id="project-name"
        type="text"
        placeholder="my-project"
        className="h-9 rounded-md border px-3 text-sm"
      />
    </div>
  ),
};

export const WithIcon: Story = {
  render: () => (
    <Label>
      <Info className="size-4" />
      API key
    </Label>
  ),
};

export const Required: Story = {
  render: () => (
    <Label htmlFor="required-field">
      Environment name
      <span className="text-destructive">*</span>
    </Label>
  ),
};

export const DisabledPeer: Story = {
  name: "Disabled (via peer input)",
  render: () => (
    <div className="flex items-center gap-2">
      <input id="disabled-input" type="checkbox" disabled className="peer" />
      <Label htmlFor="disabled-input">Cannot be changed on this plan</Label>
    </div>
  ),
};
