import type { Meta, StoryObj } from "@storybook/react-vite";

import {
  Skeleton,
  SkeletonCode,
  SkeletonParagraph,
  SkeletonTable,
} from "@/components/ui/skeleton";

const meta: Meta<typeof Skeleton> = {
  title: "UI/Skeleton",
  component: Skeleton,
  tags: ["autodocs"],
};

export default meta;

type Story = StoryObj<typeof Skeleton>;

export const Default: Story = {
  render: () => <Skeleton className="h-6 w-48" />,
};

export const Shapes: Story = {
  render: () => (
    <div className="flex items-center gap-4">
      <Skeleton className="h-10 w-10 rounded-full" />
      <div className="flex flex-col gap-2">
        <Skeleton className="h-4 w-[200px]" />
        <Skeleton className="h-4 w-[150px]" />
      </div>
    </div>
  ),
};

export const Table: Story = {
  render: () => <SkeletonTable />,
};

export const Paragraph: Story = {
  render: () => <SkeletonParagraph />,
};

export const ParagraphCustomLines: Story = {
  render: () => <SkeletonParagraph lines={6} />,
};

export const Code: Story = {
  render: () => <SkeletonCode />,
};

export const CodeCustomLines: Story = {
  render: () => <SkeletonCode lines={12} />,
};
