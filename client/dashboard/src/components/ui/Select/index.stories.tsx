import type { Meta, StoryObj } from "@storybook/react-vite";

import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from ".";

const meta: Meta<typeof Select> = {
  title: "Design System/Select",
  component: Select,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof Select>;

export const Default: Story = {
  render: () => (
    <Select defaultValue="prod">
      <SelectTrigger className="w-64">
        <SelectValue placeholder="Environment" />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="prod">Production</SelectItem>
        <SelectItem value="staging">Staging</SelectItem>
        <SelectItem value="dev">Development</SelectItem>
      </SelectContent>
    </Select>
  ),
};
