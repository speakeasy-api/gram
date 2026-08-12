import type { Meta, StoryObj } from "@storybook/react-vite";

import { Text } from ".";

const meta: Meta<typeof Text> = {
  title: "Design System/Text",
  component: Text,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof Text>;

export const Variants: Story = {
  render: () => (
    <div className="flex flex-col gap-2">
      <Text variant="body">Body copy for most of the product.</Text>
      <Text variant="small">Small print</Text>
      <Text variant="subheading">Subheading</Text>
      <Text muted>Muted body copy</Text>
      <Text mono>mono / code-adjacent</Text>
    </div>
  ),
};
