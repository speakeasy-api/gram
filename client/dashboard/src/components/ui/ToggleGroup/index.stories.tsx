import type { Meta, StoryObj } from "@storybook/react-vite";

import { ToggleGroup, ToggleGroupItem } from ".";

const meta: Meta<typeof ToggleGroup> = {
  title: "Design System/ToggleGroup",
  component: ToggleGroup,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof ToggleGroup>;

export const Default: Story = {
  render: () => (
    <ToggleGroup type="single" defaultValue="list">
      <ToggleGroupItem value="list">List</ToggleGroupItem>
      <ToggleGroupItem value="grid">Grid</ToggleGroupItem>
    </ToggleGroup>
  ),
};
