import type { Meta, StoryObj } from "@storybook/react-vite";

import { Avatar, AvatarFallback, AvatarImage } from ".";

const meta: Meta<typeof Avatar> = {
  title: "Design System/Avatar",
  component: Avatar,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof Avatar>;

export const WithFallback: Story = {
  render: () => (
    <Avatar>
      <AvatarFallback>GR</AvatarFallback>
    </Avatar>
  ),
};

// Inline so the story renders identically offline and in CI.
const SWATCH =
  "data:image/svg+xml;utf8," +
  encodeURIComponent(
    `<svg xmlns="http://www.w3.org/2000/svg" width="64" height="64"><rect width="64" height="64" fill="#4f46e5"/></svg>`,
  );

export const WithImage: Story = {
  render: () => (
    <Avatar>
      <AvatarImage src={SWATCH} alt="" />
      <AvatarFallback>SP</AvatarFallback>
    </Avatar>
  ),
};
