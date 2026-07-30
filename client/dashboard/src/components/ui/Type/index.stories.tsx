import type { Meta, StoryObj } from "@storybook/react-vite";

import { Type } from ".";

const meta: Meta<typeof Type> = {
  title: "Design System/Type",
  component: Type,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof Type>;

export const Variants: Story = {
  render: () => (
    <div className="flex flex-col gap-2">
      <Type variant="body">Body copy for most of the product.</Type>
      <Type variant="small">Small print</Type>
      <Type variant="subheading">Subheading</Type>
      <Type muted>Muted body copy</Type>
      <Type mono>mono / code-adjacent</Type>
    </div>
  ),
};
