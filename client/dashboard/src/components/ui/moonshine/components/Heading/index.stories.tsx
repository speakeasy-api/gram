import type { Meta, StoryObj } from "@storybook/react-vite";

import { Heading } from ".";

const meta: Meta<typeof Heading> = {
  title: "Moonshine/Heading",
  component: Heading,
  tags: ["autodocs"],
};

export default meta;

type Story = StoryObj<typeof Heading>;

export const ExtraLarge: Story = {
  args: {
    variant: "xl",
    children: "CLI not connected",
  },
};

export const Large: Story = {
  args: {
    variant: "lg",
    children: "Creating a new target",
  },
};

export const Medium: Story = {
  args: {
    variant: "md",
    children: "Select SDKs",
  },
};

export const Small: Story = {
  args: {
    variant: "sm",
    children: "Run your first GitHub Action",
  },
};

export const ExtraSmall: Story = {
  args: {
    variant: "xs",
    children: "Let's get you setup with a new target!",
  },
};

export const AsH6: Story = {
  args: {
    as: "h6",
    variant: "xl",
    children: "CLI not connected",
  },
};

export const AllVariants: Story = {
  render: () => (
    <div className="space-y-4">
      <Heading variant="xl">CLI not connected</Heading>
      <Heading variant="lg">Creating a new target</Heading>
      <Heading variant="md">Select SDKs</Heading>
      <Heading variant="sm">Run your first GitHub Action</Heading>
      <Heading variant="xs">Let's get you setup with a new target!</Heading>
    </div>
  ),
};
