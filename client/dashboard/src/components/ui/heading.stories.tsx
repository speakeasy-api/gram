import type { Meta, StoryObj } from "@storybook/react-vite";

import { Heading } from "@/components/ui/heading";

const meta: Meta<typeof Heading> = {
  title: "UI/Heading",
  component: Heading,
  tags: ["autodocs"],
  args: {
    children: "The quick brown fox",
  },
};

export default meta;

type Story = StoryObj<typeof Heading>;

export const H1: Story = { args: { variant: "h1" } };
export const H2: Story = { args: { variant: "h2" } };
export const H3: Story = { args: { variant: "h3" } };
export const H4: Story = { args: { variant: "h4" } };
export const H5: Story = { args: { variant: "h5" } };
export const H6: Story = { args: { variant: "h6" } };

export const AllVariants: Story = {
  render: () => (
    <div className="flex flex-col gap-2">
      <Heading variant="h1">Heading h1</Heading>
      <Heading variant="h2">Heading h2</Heading>
      <Heading variant="h3">Heading h3</Heading>
      <Heading variant="h4">Heading h4</Heading>
      <Heading variant="h5">Heading h5</Heading>
      <Heading variant="h6">Heading h6</Heading>
    </div>
  ),
};

export const WithTooltip: Story = {
  args: {
    variant: "h3",
    tooltip: "Additional context shown on hover",
    children: "Hover for more info",
  },
};

export const LoadingSkeleton: Story = {
  render: () => (
    <div className="flex flex-col gap-2">
      <Heading variant="h1">{undefined}</Heading>
      <Heading variant="h3">{undefined}</Heading>
      <Heading variant="h6">{undefined}</Heading>
    </div>
  ),
};
