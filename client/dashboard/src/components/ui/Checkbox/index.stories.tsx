import type { Meta, StoryObj } from "@storybook/react-vite";

import { Checkbox } from ".";
import { Label } from "@/components/ui/Label";

const meta: Meta<typeof Checkbox> = {
  title: "Design System/Checkbox",
  component: Checkbox,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof Checkbox>;

export const Default: Story = {
  render: () => (
    <div className="flex items-center gap-2">
      <Checkbox id="public" defaultChecked />
      <Label htmlFor="public">Publicly listed</Label>
    </div>
  ),
};

export const Disabled: Story = {
  render: () => (
    <div className="flex items-center gap-2">
      <Checkbox id="locked" disabled />
      <Label htmlFor="locked">Managed by your org</Label>
    </div>
  ),
};
