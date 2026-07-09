import type { Meta, StoryObj } from "@storybook/react-vite";

import { Kbd, KbdSequence } from "@/components/ui/kbd";

const meta: Meta<typeof Kbd> = {
  title: "UI/Kbd",
  component: Kbd,
  tags: ["autodocs"],
};

export default meta;

type Story = StoryObj<typeof Kbd>;

export const SingleKey: Story = {
  args: {
    children: "K",
  },
};

export const Modifier: Story = {
  args: {
    children: "⌘",
  },
};

export const Sequence: Story = {
  render: () => <KbdSequence keys={["⌘", "K"]} />,
};

export const LongerSequence: Story = {
  render: () => <KbdSequence keys={["⌘", "⇧", "P"]} />,
};

export const CustomSeparator: Story = {
  render: () => <KbdSequence keys={["G", "G"]} separator="then" />,
};

export const InlineWithCopy: Story = {
  render: () => (
    <div className="flex items-center gap-2 font-sans text-sm">
      <span>Open the command palette</span>
      <KbdSequence keys={["⌘", "K"]} />
    </div>
  ),
};
