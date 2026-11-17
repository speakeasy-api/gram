import type { Meta, StoryObj } from "@storybook/react-vite";

import { SkeletonTable } from ".";

const meta: Meta<typeof SkeletonTable> = {
  title: "Design System/SkeletonTable",
  component: SkeletonTable,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof SkeletonTable>;

export const TablePlaceholder: Story = {};
