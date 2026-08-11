import type { Meta, StoryObj } from "@storybook/react-vite";

import { Skeleton, SkeletonCode, SkeletonParagraph, SkeletonTable } from ".";

const meta: Meta<typeof Skeleton> = {
  title: "Design System/Skeleton",
  component: Skeleton,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof Skeleton>;

export const SingleBar: Story = {
  render: () => <Skeleton className="h-4 w-40" />,
};

export const FromChildren: Story = {
  render: () => (
    <Skeleton>
      <div className="h-5 w-48" />
      <div className="h-5 w-96" />
      <div className="h-5 w-64" />
    </Skeleton>
  ),
};

export const Paragraph: Story = {
  render: () => (
    <div className="w-96">
      <SkeletonParagraph lines={4} />
    </div>
  ),
};

export const TablePlaceholder: Story = {
  render: () => <SkeletonTable />,
};

export const CodePlaceholder: Story = {
  render: () => (
    <div className="w-[32rem]">
      <SkeletonCode lines={12} />
    </div>
  ),
};
