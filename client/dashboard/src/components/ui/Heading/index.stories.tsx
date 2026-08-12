import type { Meta, StoryObj } from "@storybook/react-vite";

import { Heading } from ".";

const meta: Meta<typeof Heading> = {
  title: "Design System/Heading",
  component: Heading,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof Heading>;

export const Scale: Story = {
  render: () => (
    <div className="flex flex-col gap-3">
      <Heading variant="h1">Heading one</Heading>
      <Heading variant="h2">Heading two</Heading>
      <Heading variant="h3">Heading three</Heading>
      <Heading variant="h4">Heading four</Heading>
      <Heading variant="h5">Heading five</Heading>
      <Heading variant="h6">Heading six</Heading>
    </div>
  ),
};
