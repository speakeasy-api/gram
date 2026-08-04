import type { Meta, StoryObj } from "@storybook/react-vite";

import { TextArea } from ".";

const meta: Meta<typeof TextArea> = {
  title: "Design System/Textarea",
  component: TextArea,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof TextArea>;

export const Default: Story = {
  render: () => (
    <div className="w-96">
      <TextArea placeholder="Describe this toolset…" rows={4} />
    </div>
  ),
};
