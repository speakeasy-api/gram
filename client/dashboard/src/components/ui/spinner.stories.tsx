import type { Meta, StoryObj } from "@storybook/react-vite";

import { Spinner } from "@/components/ui/spinner";

const meta: Meta<typeof Spinner> = {
  title: "UI/Spinner",
  component: Spinner,
  tags: ["autodocs"],
};

export default meta;

type Story = StoryObj<typeof Spinner>;

export const Default: Story = {
  render: () => <Spinner />,
};

export const Sizes: Story = {
  render: () => (
    <div className="flex items-center gap-4">
      <Spinner className="h-3 w-3" />
      <Spinner className="h-4 w-4" />
      <Spinner className="h-6 w-6" />
      <Spinner className="h-10 w-10" />
    </div>
  ),
};

export const CustomColor: Story = {
  render: () => <Spinner className="text-destructive h-6 w-6" />,
};

export const InsideButton: Story = {
  render: () => (
    <button
      disabled
      className="bg-primary text-primary-foreground inline-flex items-center rounded-md px-4 py-2 text-sm font-medium opacity-70"
    >
      <Spinner />
      Loading...
    </button>
  ),
};
