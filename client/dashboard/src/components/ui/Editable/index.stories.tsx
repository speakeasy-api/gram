import type { Meta, StoryObj } from "@storybook/react-vite";

import { Editable } from ".";

const meta: Meta<typeof Editable> = {
  title: "Design System/Editable",
  component: Editable,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof Editable>;

export const Default: Story = {
  render: () => (
    <Editable onClick={() => {}}>
      <span className="text-lg font-medium">Petstore</span>
    </Editable>
  ),
};

export const Disabled: Story = {
  render: () => (
    <Editable disabled>
      <span className="text-lg font-medium">Petstore</span>
    </Editable>
  ),
};
